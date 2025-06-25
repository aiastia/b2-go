package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/joho/godotenv"
)

// 配置结构体
type Config struct {
	SourceDir                string
	BucketName               string
	AccountID                string
	ApplicationKey           string
	RetentionDays            int
	SmtpServer               string
	SmtpPort                 int
	SmtpUser                 string
	SmtpPassword             string
	EmailFrom                string
	EmailTo                  string
	ExcludePatterns          []string
	SyncDelete               bool
	BackupPrefix             string
	LocalStatePath           string // 本地状态文件路径
	EnableEmailNotification  bool   // 是否启用邮件通知
	EnableMetadataCheck      bool   // 是否启用元数据检查（防止重复上传）
	MetadataStrategy         string // 元数据策略：none, basic, full
}

// 文件状态信息
type FileState struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Checksum string    `json:"checksum"`
	BackedUp bool      `json:"backed_up"` // 是否已备份
}

// 本地状态结构
type LocalState struct {
	LastBackup time.Time             `json:"last_backup"`
	Files      map[string]*FileState `json:"files"`
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

	// 设置默认元数据策略
	metadataStrategy := os.Getenv("METADATA_STRATEGY")
	if metadataStrategy == "" {
		metadataStrategy = "basic" // 默认使用基本策略
	}

	return Config{
		SourceDir:                os.Getenv("SOURCE_DIR"),
		BucketName:               os.Getenv("B2_BUCKET_NAME"),
		AccountID:                os.Getenv("B2_ACCOUNT_ID"),
		ApplicationKey:           os.Getenv("B2_APPLICATION_KEY"),
		RetentionDays:            parseInt(os.Getenv("RETENTION_DAYS"), 30),
		SmtpServer:               os.Getenv("SMTP_SERVER"),
		SmtpPort:                 parseInt(os.Getenv("SMTP_PORT"), 587),
		SmtpUser:                 os.Getenv("SMTP_USER"),
		SmtpPassword:             os.Getenv("SMTP_PASSWORD"),
		EmailFrom:                os.Getenv("EMAIL_FROM"),
		EmailTo:                  os.Getenv("EMAIL_TO"),
		ExcludePatterns:          exclude,
		SyncDelete:               os.Getenv("SYNC_DELETE") == "true",
		BackupPrefix:             os.Getenv("BACKUP_PREFIX"),
		LocalStatePath:           os.Getenv("LOCAL_STATE_PATH"),
		EnableEmailNotification:  os.Getenv("ENABLE_EMAIL_NOTIFICATION") == "true",
		EnableMetadataCheck:      os.Getenv("ENABLE_METADATA_CHECK") == "true",
		MetadataStrategy:         metadataStrategy,
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
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		
		// 处理目录模式 (以 / 结尾)
		if strings.HasSuffix(pattern, "/") {
			dirPattern := strings.TrimSuffix(pattern, "/")
			// 检查路径是否以该目录开头
			if strings.HasPrefix(relPath, dirPattern+"/") || relPath == dirPattern {
				return true
			}
			// 也检查通配符匹配
			matched, _ := filepath.Match(pattern, relPath)
			if matched {
				return true
			}
			continue
		}
		
		// 处理包含路径分隔符的模式 (如 temp/**)
		if strings.Contains(pattern, "/") {
			matched, _ := filepath.Match(pattern, relPath)
			if matched {
				return true
			}
			continue
		}
		
		// 处理纯文件名模式 (如 *.tmp, db.sqlite3)
		fileName := filepath.Base(relPath)
		matched, _ := filepath.Match(pattern, fileName)
		if matched {
			return true
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

// 加载本地状态
func loadLocalState(config Config) (*LocalState, error) {
	state := &LocalState{
		Files: make(map[string]*FileState),
	}

	if config.LocalStatePath == "" {
		return state, nil
	}

	file, err := os.Open(config.LocalStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil // 文件不存在时返回空状态
		}
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(state); err != nil {
		return nil, err
	}

	return state, nil
}

// 保存本地状态
func saveLocalState(config Config, state *LocalState) error {
	if config.LocalStatePath == "" {
		return nil
	}

	file, err := os.Create(config.LocalStatePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(state)
}

// 扫描本地文件并与状态比较
func scanAndCompareFiles(config Config, state *LocalState) ([]*FileState, error) {
	var changedFiles []*FileState

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

		// 检查文件是否在状态中
		existing, exists := state.Files[relPath]
		
		// 计算新文件的校验和
		checksum, err := fileChecksum(path)
		if err != nil {
			log.Printf("Error calculating checksum for %s: %v", path, err)
			return nil
		}
		
		// 检查文件是否修改
		modified := !exists || 
			info.ModTime().After(existing.ModTime) || 
			info.Size() != existing.Size ||
			checksum != existing.Checksum
		
		if !modified {
			// 文件未修改，标记为已备份
			existing.BackedUp = true
			log.Printf("File %s unchanged, skipping", relPath)
			return nil
		}

		// 如果文件存在但校验和相同，说明只是元数据变化
		if exists && checksum == existing.Checksum {
			// 文件内容未改变，只是元数据变化（如修改时间）
			existing.ModTime = info.ModTime()
			existing.Size = info.Size()
			existing.BackedUp = true
			log.Printf("File %s content unchanged, only metadata updated", relPath)
			return nil
		}

		// 创建新的文件状态
		fileState := &FileState{
			Path:     relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Checksum: checksum,
			BackedUp: false, // 需要备份
		}

		// 添加到状态和变更列表
		state.Files[relPath] = fileState
		changedFiles = append(changedFiles, fileState)
		
		log.Printf("File %s changed (size: %d, checksum: %s), will upload", relPath, info.Size(), checksum[:8])

		return nil
	})

	return changedFiles, err
}

// 获取B2文件列表
func getB2Files(config Config, b2Client *b2.Client) (map[string]*b2.Object, error) {
	ctx := context.Background()
	
	// 获取bucket
	bucket, err := b2Client.Bucket(ctx, config.BucketName)
	if err != nil {
		return nil, err
	}
	
	// 列出文件
	iterator := bucket.List(ctx)
	
	fileMap := make(map[string]*b2.Object)
	for iterator.Next() {
		obj := iterator.Object()
		// 去除前缀
		relPath := strings.TrimPrefix(obj.Name(), config.BackupPrefix)
		fileMap[relPath] = obj
	}
	
	if err := iterator.Err(); err != nil {
		return nil, err
	}
	
	return fileMap, nil
}

// 上传文件到B2
func uploadFileToB2(config Config, bucket *b2.Bucket, localPath, remotePath string, checksum string) error {
	ctx := context.Background()
	
	// 检查云端是否已存在相同文件
	remoteObj := bucket.Object(config.BackupPrefix + remotePath)
	
	// 尝试获取远程文件信息
	if attrs, err := remoteObj.Attrs(ctx); err == nil {
		// 如果远程文件存在，检查是否需要上传
		log.Printf("File %s already exists in B2, checking if update is needed", remotePath)
		
		// 根据元数据策略进行不同的检查
		shouldSkip := false
		
		switch config.MetadataStrategy {
		case "full":
			// 完整策略：使用元数据文件进行详细检查
			if config.EnableMetadataCheck {
				if metadata, err := getFileMetadata(config, bucket, remotePath); err == nil {
					if storedChecksum, ok := metadata["checksum"].(string); ok && storedChecksum == checksum {
						log.Printf("File %s has same checksum (full check), skipping upload", remotePath)
						shouldSkip = true
					}
				}
			}
		case "basic":
			// 基本策略：只进行大小比较，不创建元数据文件
			if localInfo, err := os.Stat(localPath); err == nil {
				if localInfo.Size() == attrs.Size {
					log.Printf("File %s has same size (basic check), skipping upload", remotePath)
					shouldSkip = true
				}
			}
		case "none":
			// 无策略：总是上传
			log.Printf("File %s will be uploaded (no duplicate check)", remotePath)
		default:
			// 默认使用基本策略
			if localInfo, err := os.Stat(localPath); err == nil {
				if localInfo.Size() == attrs.Size {
					log.Printf("File %s has same size (default check), skipping upload", remotePath)
					shouldSkip = true
				}
			}
		}
		
		if shouldSkip {
			return nil
		}
	}
	
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 创建对象
	obj := bucket.Object(config.BackupPrefix + remotePath)
	
	// 创建writer
	w := obj.NewWriter(ctx)
	
	// 复制文件内容
	if _, err := io.Copy(w, file); err != nil {
		w.Close()
		return err
	}
	
	if err := w.Close(); err != nil {
		return err
	}
	
	// 根据策略决定是否存储元数据
	if config.EnableMetadataCheck && config.MetadataStrategy == "full" {
		// 获取文件信息用于存储元数据
		fileInfo, err := os.Stat(localPath)
		if err != nil {
			log.Printf("Warning: Could not get file info for metadata: %v", err)
		} else {
			// 存储文件元数据
			if err := storeFileMetadata(config, bucket, remotePath, checksum, fileInfo.Size(), fileInfo.ModTime()); err != nil {
				log.Printf("Warning: Could not store file metadata: %v", err)
				// 不返回错误，因为文件上传成功了
			}
		}
	}
	
	return nil
}

// 删除B2文件
func deleteB2File(config Config, obj *b2.Object) error {
	ctx := context.Background()
	
	// 删除主文件
	if err := obj.Delete(ctx); err != nil {
		return err
	}
	
	// 只有在完整策略下才删除元数据文件
	if config.EnableMetadataCheck && config.MetadataStrategy == "full" {
		fileName := obj.Name()
		// 从完整路径中提取相对路径
		relPath := strings.TrimPrefix(fileName, config.BackupPrefix)
		metadataFileName := getMetadataFileName(relPath)
		
		// 创建元数据文件对象并删除
		metadataObj := obj.Bucket().Object(config.BackupPrefix + metadataFileName)
		if err := metadataObj.Delete(ctx); err != nil {
			// 元数据文件可能不存在，忽略错误
			log.Printf("Note: Could not delete metadata file for %s: %v", fileName, err)
		}
	}
	
	return nil
}

// 发送邮件通知
func sendEmailNotification(config Config, success bool, stats map[string]int) {
	// 首先检查是否启用邮件通知
	if !config.EnableEmailNotification {
		log.Println("Email notification disabled")
		return
	}
	
	// 检查SMTP配置是否完整
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
func manageRetention(config Config, bucket *b2.Bucket) error {
	ctx := context.Background()

	// 列出所有备份文件
	iterator := bucket.List(ctx)
	
	// 计算保留截止时间
	retentionCutoff := time.Now().AddDate(0, 0, -config.RetentionDays)

	for iterator.Next() {
		obj := iterator.Object()
		
		// 只处理指定前缀的文件
		if !strings.HasPrefix(obj.Name(), config.BackupPrefix) {
			continue
		}
		
		// 获取文件属性
		attrs, err := obj.Attrs(ctx)
		if err != nil {
			log.Printf("Error getting attrs for %s: %v", obj.Name(), err)
			continue
		}
		
		// 检查文件时间
		if attrs.UploadTimestamp.Before(retentionCutoff) {
			log.Printf("Deleting old backup: %s (uploaded: %s)", 
				obj.Name(), attrs.UploadTimestamp)
			
			// 删除文件
			if err := obj.Delete(ctx); err != nil {
				log.Printf("Error deleting file %s: %v", obj.Name(), err)
			}
		}
	}
	
	return iterator.Err()
}

// 获取文件元数据文件名
func getMetadataFileName(remotePath string) string {
	return remotePath + ".meta"
}

// 存储文件元数据到B2
func storeFileMetadata(config Config, bucket *b2.Bucket, remotePath, checksum string, size int64, modTime time.Time) error {
	ctx := context.Background()
	
	// 简化元数据，只存储最核心的信息用于重复检测
	metadata := map[string]interface{}{
		"checksum": checksum,  // 核心：用于检测文件内容是否相同
		"size":     size,      // 辅助：快速预检查
		"version":  "1.0",
	}
	
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	
	metadataObj := bucket.Object(config.BackupPrefix + getMetadataFileName(remotePath))
	w := metadataObj.NewWriter(ctx)
	
	if _, err := w.Write(metadataJSON); err != nil {
		w.Close()
		return err
	}
	
	return w.Close()
}

// 从B2获取文件元数据
func getFileMetadata(config Config, bucket *b2.Bucket, remotePath string) (map[string]interface{}, error) {
	ctx := context.Background()
	
	metadataObj := bucket.Object(config.BackupPrefix + getMetadataFileName(remotePath))
	
	// 尝试获取元数据文件
	reader := metadataObj.NewReader(ctx)
	defer reader.Close()
	
	var metadata map[string]interface{}
	if err := json.NewDecoder(reader).Decode(&metadata); err != nil {
		return nil, err
	}
	
	return metadata, nil
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
		config.LocalStatePath = "/var/backup/state.json"
	}
	
	log.Printf("Source directory: %s", config.SourceDir)
	log.Printf("Exclude patterns: %v", config.ExcludePatterns)
	log.Printf("Sync delete: %v", config.SyncDelete)
	log.Printf("Local state path: %s", config.LocalStatePath)
	log.Printf("Email notification: %v", config.EnableEmailNotification)
	log.Printf("Enable metadata check: %v", config.EnableMetadataCheck)
	log.Printf("Metadata strategy: %s", config.MetadataStrategy)
	
	// 加载本地状态
	localState, err := loadLocalState(config)
	if err != nil {
		log.Fatalf("Failed to load local state: %v", err)
	}
	
	// 扫描本地文件并检测变化
	log.Println("Scanning for changed files...")
	changedFiles, err := scanAndCompareFiles(config, localState)
	if err != nil {
		log.Fatalf("File scan failed: %v", err)
	}
	log.Printf("Found %d changed files", len(changedFiles))
	
	// 如果没有文件变化，直接退出
	if len(changedFiles) == 0 {
		log.Println("No files changed, backup skipped")
		return
	}
	
	// 连接到Backblaze B2
	log.Println("Connecting to Backblaze B2...")
	b2Client, err := b2.NewClient(context.Background(), config.AccountID, config.ApplicationKey)
	if err != nil {
		log.Fatalf("B2 connection failed: %v", err)
	}
	
	// 获取bucket
	bucket, err := b2Client.Bucket(context.Background(), config.BucketName)
	if err != nil {
		log.Fatalf("Bucket retrieval failed: %v", err)
	}
	
	// 获取B2文件列表
	log.Println("Fetching B2 file list...")
	b2Files, err := getB2Files(config, b2Client)
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
	
	// 上传变化的文件
	for _, fileState := range changedFiles {
		localPath := filepath.Join(config.SourceDir, fileState.Path)
		
		log.Printf("Uploading changed file: %s", fileState.Path)
		if err := uploadFileToB2(config, bucket, localPath, fileState.Path, fileState.Checksum); err != nil {
			log.Printf("Upload failed for %s: %v", fileState.Path, err)
			stats["failed"]++
		} else {
			stats["uploaded"]++
			fileState.BackedUp = true // 标记为已备份
		}
	}
	
	// 处理删除（如果启用）
	if config.SyncDelete {
		// 查找本地状态中有但实际不存在的文件
		for relPath, _ := range localState.Files {
			localPath := filepath.Join(config.SourceDir, relPath)
			
			// 检查文件是否仍然存在
			if _, err := os.Stat(localPath); os.IsNotExist(err) {
				// 检查是否在排除列表中
				if isExcluded(relPath, config.ExcludePatterns) {
					stats["skipped"]++
					continue
				}
				
				// 检查云端是否有对应文件
				if remoteFile, exists := b2Files[relPath]; exists {
					log.Printf("Deleting removed file: %s", relPath)
					if err := deleteB2File(config, remoteFile); err != nil {
						log.Printf("Delete failed for %s: %v", relPath, err)
						stats["failed"]++
					} else {
						stats["deleted"]++
						delete(localState.Files, relPath) // 从状态中移除
					}
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
	
	// 更新最后备份时间
	localState.LastBackup = time.Now()
	
	// 保存本地状态
	if err := saveLocalState(config, localState); err != nil {
		log.Printf("Failed to save local state: %v", err)
	} else {
		log.Printf("Local state saved to %s", config.LocalStatePath)
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