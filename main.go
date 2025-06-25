package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// 获取文件元数据文件名
func getMetadataFileName(remotePath string) string {
	return remotePath + ".meta"
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
	
	// 创建各个模块实例
	stateManager := NewStateManager(config)
	fileScanner := NewFileScanner(config)
	
	// 加载本地状态
	localState, err := stateManager.LoadState()
	if err != nil {
		log.Fatalf("Failed to load local state: %v", err)
	}
	
	// 扫描本地文件并检测变化
	log.Println("Scanning for changed files...")
	changedFiles, err := fileScanner.ScanAndCompareFiles(localState)
	if err != nil {
		log.Fatalf("File scan failed: %v", err)
	}
	log.Printf("Found %d changed files", len(changedFiles))
	
	// 如果没有文件变化，直接退出
	if len(changedFiles) == 0 {
		log.Println("No files changed, backup skipped")
		return
	}
	
	// 创建B2存储实例
	b2Storage, err := NewB2Storage(config)
	if err != nil {
		log.Fatalf("B2 storage initialization failed: %v", err)
	}
	defer b2Storage.Close()
	
	// 获取B2文件列表
	log.Println("Fetching B2 file list...")
	b2Files, err := b2Storage.GetFileList()
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
		if err := b2Storage.UploadFile(localPath, fileState.Path, fileState.Checksum); err != nil {
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
		for relPath := range localState.Files {
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
					if err := b2Storage.DeleteFile(remoteFile); err != nil {
						log.Printf("Delete failed for %s: %v", relPath, err)
						stats["failed"]++
					} else {
						stats["deleted"]++
						stateManager.RemoveFile(localState, relPath) // 从状态中移除
					}
				}
			}
		}
	}
	
	// 执行保留策略
	if config.RetentionDays > 0 {
		log.Println("Applying retention policy...")
		if err := b2Storage.ManageRetention(); err != nil {
			log.Printf("Retention policy failed: %v", err)
		}
	}
	
	// 更新最后备份时间
	stateManager.UpdateLastBackupTime(localState)
	
	// 保存本地状态
	if err := stateManager.SaveState(localState); err != nil {
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
	
	// 创建邮件通知实例并发送通知
	emailConfig := EmailConfig{
		Server:   config.SmtpServer,
		Port:     config.SmtpPort,
		User:     config.SmtpUser,
		Password: config.SmtpPassword,
		From:     config.EmailFrom,
		To:       config.EmailTo,
		Enabled:  config.EnableEmailNotification,
	}
	
	emailNotifier := NewEmailNotification(emailConfig)
	success := stats["failed"] == 0
	if err := emailNotifier.SendNotification(success, stats); err != nil {
		log.Printf("Failed to send email notification: %v", err)
	}
	
	if !success {
		log.Fatal("Backup completed with errors")
	} else {
		log.Println("Backup completed successfully")
	}
}