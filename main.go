package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kothar/go-backblaze"
)

// 配置结构体
type Config struct {
	SourceDir        string
	BucketName       string
	AccountID        string
	ApplicationKey   string
	RetentionDays    int
	SmtpServer       string
	SmtpPort         int
	SmtpUser         string
	SmtpPassword     string
	EmailFrom        string
	EmailTo          string
	ExcludePatterns  []string
	SyncDelete       bool
	BackupPrefix     string
	LocalStatePath   string
}

// 文件状态信息
type FileState struct {
	Path     string
	Size     int64
	ModTime  time.Time
	Checksum string
}

// 加载环境变量
func loadConfig() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found, using system environment variables")
	}

	exclude := strings.Split(os.Getenv("EXCLUDE_PATTERNS"), ",")
	if len(exclude) == 1 && exclude[0] == "" {
		exclude = []string{}
	}

	return Config{
		SourceDir:        os.Getenv("SOURCE_DIR"),
		BucketName:       os.Getenv("B2_BUCKET_NAME"),
		AccountID:        os.Getenv("B2_ACCOUNT_ID"),
		ApplicationKey:   os.Getenv("B2_APPLICATION_KEY"),
		RetentionDays:    parseInt(os.Getenv("RETENTION_DAYS"), 30),
		SmtpServer:       os.Getenv("SMTP_SERVER"),
		SmtpPort:         parseInt(os.Getenv("SMTP_PORT"), 587),
		SmtpUser:         os.Getenv("SMTP_USER"),
		SmtpPassword:     os.Getenv("SMTP_PASSWORD"),
		EmailFrom:        os.Getenv("EMAIL_FROM"),
		EmailTo:          os.Getenv("EMAIL_TO"),
		ExcludePatterns:  exclude,
		SyncDelete:       os.Getenv("SYNC_DELETE") == "true",
		BackupPrefix:     os.Getenv("BACKUP_PREFIX"),
		LocalStatePath:   os.Getenv("LOCAL_STATE_PATH"),
	}
}

func parseInt(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	var result int
	_, err := fmt.Sscanf(value, "%d", &result)
	if err != nil {
		return defaultValue
	}
	return result
}

// 检查文件是否应该排除
func isExcluded(path string, patterns []string) bool {
	relPath := filepath.ToSlash(path)
	for _, pattern := range patterns {
		matched, _ := filepath.Match(pattern, filepath.Base(relPath))
		if matched {
			return true
		}
		
		// 检查目录模式
		if strings.Contains(pattern, "/") {
			matched, _ := filepath.Match(pattern, relPath)
			if matched {
				return true
			}
		}
	}
	return false
}

// 计算文件SHA1校验和
func fileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// 扫描本地文件
func scanLocalFiles(config Config) (map[string]FileState, error) {
	fileStates := make(map[string]FileState)

	err := filepath.Walk(config.SourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(config.SourceDir, path)
		if err != nil {
			return err
		}

		// 应用排除规则
		if isExcluded(relPath, config.ExcludePatterns) {
			return nil
		}

		// 计算校验和
		checksum, err := fileChecksum(path)
		if err != nil {
			log.Printf("Error calculating checksum for %s: %v", path, err)
			return nil
		}

		fileStates[relPath] = FileState{
			Path:     relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Checksum: checksum,
		}

		return nil
	})

	return fileStates, err
}

// 获取B2文件列表
func getB2Files(config Config, b2Client *backblaze.B2) (map[string]backblaze.File, error) {
	ctx := context.Background()
	bucket, err := b2Client.Bucket(ctx, config.BucketName)
	if err != nil {
		return nil, err
	}

	// 获取所有文件
	files, err := bucket.ListFileNames(ctx, config.BackupPrefix, "", 10000)
	if err != nil {
		return nil, err
	}

	fileMap := make(map[string]backblaze.File)
	for _, file := range files.Files {
		// 去除前缀
		relPath := strings.TrimPrefix(file.Name, config.BackupPrefix)
		fileMap[relPath] = file
	}

	return fileMap, nil
}

// 上传文件到B2
func uploadFileToB2(config Config, bucket *backblaze.Bucket, localPath, remotePath string) error {
	ctx := context.Background()
	
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 设置元数据
	info := map[string]string{
		"original_path": localPath,
		"upload_time":   time.Now().Format(time.RFC3339),
	}

	// 上传文件
	_, err = bucket.UploadFile(ctx, config.BackupPrefix+remotePath, info, file)
	return err
}

// 删除B2文件
func deleteB2File(config Config, bucket *backblaze.Bucket, file backblaze.File) error {
	ctx := context.Background()
	_, err := bucket.DeleteFileVersion(ctx, file.Name, file.ID)
	return err
}

// 发送邮件通知
func sendEmailNotification(config Config, success bool, stats map[string]int) {
	if config.SmtpServer == "" || config.EmailFrom == "" || config.EmailTo == "" {
		log.Println("Email notification skipped: SMTP configuration missing")
		return
	}

	subject := "Backup Failed"
	if success {
		subject = "Backup Succeeded"
	}

	// 构建统计信息
	statsMsg := fmt.Sprintf("Files uploaded: %d\nFiles deleted: %d\nFiles skipped: %d",
		stats["uploaded"], stats["deleted"], stats["skipped"])

	body := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\nBackup Summary:\n%s",
		config.EmailFrom, config.EmailTo, subject, statsMsg)

	auth := smtp.PlainAuth("", config.SmtpUser, config.SmtpPassword, config.SmtpServer)
	addr := fmt.Sprintf("%s:%d", config.SmtpServer, config.SmtpPort)

	err := smtp.SendMail(addr, auth, config.EmailFrom, []string{config.EmailTo}, []byte(body))
	if err != nil {
		log.Printf("Failed to send email: %v", err)
	} else {
		log.Println("Email notification sent")
	}
}

// 管理备份保留策略
func manageRetention(config Config, bucket *backblaze.Bucket) error {
	ctx := context.Background()

	// 列出所有备份文件
	files, err := bucket.ListFileNames(ctx, config.BackupPrefix, "", 10000)
	if err != nil {
		return err
	}

	// 计算保留截止时间
	retentionCutoff := time.Now().AddDate(0, 0, -config.RetentionDays)

	for _, file := range files.Files {
		// 检查文件时间
		uploadTime := time.Unix(file.UploadTimestamp/1000, 0)
		if uploadTime.Before(retentionCutoff) {
			log.Printf("Deleting old backup: %s (uploaded: %s)", 
				file.Name, uploadTime)
			
			// 删除文件
			if err := deleteB2File(config, bucket, file); err != nil {
				log.Printf("Error deleting file %s: %v", file.Name, err)
			}
		}
	}

	return nil
}

func main() {
	startTime := time.Now()
	log.Println("Starting file sync backup...")
	
	// 加载配置
	config := loadConfig()
	
	// 验证必要配置
	if config.SourceDir == "" || config.BucketName == "" || 
	   config.AccountID == "" || config.ApplicationKey == "" {
		log.Fatal("Missing required environment variables")
	}
	
	// 设置默认值
	if config.BackupPrefix == "" {
		config.BackupPrefix = "backups/"
	} else if !strings.HasSuffix(config.BackupPrefix, "/") {
		config.BackupPrefix += "/"
	}
	
	if config.LocalStatePath == "" {
		config.LocalStatePath = "/tmp/backup_state.json"
	}
	
	log.Printf("Source directory: %s", config.SourceDir)
	log.Printf("Exclude patterns: %v", config.ExcludePatterns)
	log.Printf("Sync delete: %v", config.SyncDelete)
	
	// 连接到Backblaze B2
	log.Println("Connecting to Backblaze B2...")
	b2, err := backblaze.NewB2(backblaze.Credentials{
		AccountID:      config.AccountID,
		ApplicationKey: config.ApplicationKey,
	})
	if err != nil {
		log.Fatalf("B2 connection failed: %v", err)
	}

	ctx := context.Background()
	bucket, err := b2.Bucket(ctx, config.BucketName)
	if err != nil {
		log.Fatalf("Bucket retrieval failed: %v", err)
	}
	
	// 扫描本地文件
	log.Println("Scanning local files...")
	localFiles, err := scanLocalFiles(config)
	if err != nil {
		log.Fatalf("Local file scan failed: %v", err)
	}
	log.Printf("Found %d local files to backup", len(localFiles))
	
	// 获取B2文件列表
	log.Println("Fetching B2 file list...")
	b2Files, err := getB2Files(config, b2)
	if err != nil {
		log.Fatalf("B2 file list retrieval failed: %v", err)
	}
	log.Printf("Found %d files in B2", len(b2Files))
	
	// 统计信息
	stats := map[string]int{
		"uploaded": 0,
		"deleted":  0,
		"skipped":  0,
		"failed":   0,
	}
	
	// 处理上传和更新
	for relPath, localState := range localFiles {
		remoteFile, exists := b2Files[relPath]
		localPath := filepath.Join(config.SourceDir, relPath)
		
		// 检查文件是否需要上传
		if !exists {
			log.Printf("Uploading new file: %s", relPath)
			if err := uploadFileToB2(config, bucket, localPath, relPath); err != nil {
				log.Printf("Upload failed for %s: %v", relPath, err)
				stats["failed"]++
			} else {
				stats["uploaded"]++
			}
			continue
		}
		
		// 检查文件是否需要更新
		remoteModTime := time.Unix(remoteFile.UploadTimestamp/1000, 0)
		if localState.ModTime.After(remoteModTime) {
			// 获取远程文件信息以比较校验和
			fileInfo, err := bucket.GetFileInfo(ctx, remoteFile.ID)
			if err != nil {
				log.Printf("Failed to get file info for %s: %v", relPath, err)
				continue
			}
			
			// 检查校验和是否匹配
			if localState.Checksum != fileInfo.ContentSha1 {
				log.Printf("Updating changed file: %s", relPath)
				if err := uploadFileToB2(config, bucket, localPath, relPath); err != nil {
					log.Printf("Upload failed for %s: %v", relPath, err)
					stats["failed"]++
				} else {
					stats["uploaded"]++
				}
			} else {
				stats["skipped"]++
			}
		} else {
			stats["skipped"]++
		}
	}
	
	// 处理删除（如果启用）
	if config.SyncDelete {
		for relPath, remoteFile := range b2Files {
			if _, exists := localFiles[relPath]; !exists {
				// 检查是否在排除列表中
				if isExcluded(relPath, config.ExcludePatterns) {
					stats["skipped"]++
					continue
				}
				
				log.Printf("Deleting removed file: %s", relPath)
				if err := deleteB2File(config, bucket, remoteFile); err != nil {
					log.Printf("Delete failed for %s: %v", relPath, err)
					stats["failed"]++
				} else {
					stats["deleted"]++
				}
			}
		}
	}
	
	// 执行保留策略
	if config.RetentionDays > 0 {
		log.Println("Applying retention policy...")
		if err := manageRetention(config, bucket); err != nil {
			log.Printf("Retention policy failed: %v", err)
		}
	}
	
	// 计算执行时间
	duration := time.Since(startTime)
	
	// 准备统计信息
	statsMsg := fmt.Sprintf("Backup completed in %v\n", duration.Round(time.Second))
	statsMsg += fmt.Sprintf("Uploaded: %d, Deleted: %d, Skipped: %d, Failed: %d",
		stats["uploaded"], stats["deleted"], stats["skipped"], stats["failed"])
	
	log.Println(statsMsg)
	
	// 发送通知
	success := stats["failed"] == 0
	sendEmailNotification(config, success, stats)
	
	if !success {
		log.Fatal("Backup completed with errors")
	} else {
		log.Println("Backup completed successfully")
	}
}