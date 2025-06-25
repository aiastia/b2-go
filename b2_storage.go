package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Backblaze/blazer/b2"
)

// B2Storage B2存储结构体
type B2Storage struct {
	client     *b2.Client
	bucket     *b2.Bucket
	config     Config
}

// NewB2Storage 创建新的B2存储实例
func NewB2Storage(config Config) (*B2Storage, error) {
	ctx := context.Background()
	
	// 连接到Backblaze B2
	client, err := b2.NewClient(ctx, config.AccountID, config.ApplicationKey)
	if err != nil {
		return nil, err
	}
	
	// 获取bucket
	bucket, err := client.Bucket(ctx, config.BucketName)
	if err != nil {
		return nil, err
	}
	
	return &B2Storage{
		client: client,
		bucket: bucket,
		config: config,
	}, nil
}

// UploadFile 上传文件到B2
func (b *B2Storage) UploadFile(localPath, remotePath, checksum string) error {
	ctx := context.Background()
	
	// 检查云端是否已存在相同文件
	remoteObj := b.bucket.Object(b.config.BackupPrefix + remotePath)
	
	// 尝试获取远程文件信息
	if attrs, err := remoteObj.Attrs(ctx); err == nil {
		// 如果远程文件存在，检查是否需要上传
		log.Printf("File %s already exists in B2, checking if update is needed", remotePath)
		
		// 根据元数据策略进行不同的检查
		shouldSkip := false
		
		switch b.config.MetadataStrategy {
		case "full":
			// 完整策略：使用元数据文件进行详细检查
			if b.config.EnableMetadataCheck {
				if metadata, err := b.getFileMetadata(remotePath); err == nil {
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
	obj := b.bucket.Object(b.config.BackupPrefix + remotePath)
	
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
	if b.config.EnableMetadataCheck && b.config.MetadataStrategy == "full" {
		// 获取文件信息用于存储元数据
		fileInfo, err := os.Stat(localPath)
		if err != nil {
			log.Printf("Warning: Could not get file info for metadata: %v", err)
		} else {
			// 存储文件元数据
			if err := b.storeFileMetadata(remotePath, checksum, fileInfo.Size(), fileInfo.ModTime()); err != nil {
				log.Printf("Warning: Could not store file metadata: %v", err)
				// 不返回错误，因为文件上传成功了
			}
		}
	}
	
	return nil
}

// DeleteFile 删除B2文件
func (b *B2Storage) DeleteFile(obj *b2.Object) error {
	ctx := context.Background()
	
	// 删除主文件
	if err := obj.Delete(ctx); err != nil {
		return err
	}
	
	// 只有在完整策略下才删除元数据文件
	if b.config.EnableMetadataCheck && b.config.MetadataStrategy == "full" {
		fileName := obj.Name()
		// 从完整路径中提取相对路径
		relPath := strings.TrimPrefix(fileName, b.config.BackupPrefix)
		metadataFileName := getMetadataFileName(relPath)
		
		// 创建元数据文件对象并删除
		metadataObj := b.bucket.Object(b.config.BackupPrefix + metadataFileName)
		if err := metadataObj.Delete(ctx); err != nil {
			// 元数据文件可能不存在，忽略错误
			log.Printf("Note: Could not delete metadata file for %s: %v", fileName, err)
		}
	}
	
	return nil
}

// GetFileList 获取B2文件列表
func (b *B2Storage) GetFileList() (map[string]*b2.Object, error) {
	ctx := context.Background()
	
	// 列出文件
	iterator := b.bucket.List(ctx)
	
	fileMap := make(map[string]*b2.Object)
	for iterator.Next() {
		obj := iterator.Object()
		// 去除前缀
		relPath := strings.TrimPrefix(obj.Name(), b.config.BackupPrefix)
		fileMap[relPath] = obj
	}
	
	if err := iterator.Err(); err != nil {
		return nil, err
	}
	
	return fileMap, nil
}

// ManageRetention 管理备份保留策略
func (b *B2Storage) ManageRetention() error {
	ctx := context.Background()

	// 列出所有备份文件
	iterator := b.bucket.List(ctx)
	
	// 计算保留截止时间
	retentionCutoff := time.Now().AddDate(0, 0, -b.config.RetentionDays)

	for iterator.Next() {
		obj := iterator.Object()
		
		// 只处理指定前缀的文件
		if !strings.HasPrefix(obj.Name(), b.config.BackupPrefix) {
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

// 存储文件元数据到B2
func (b *B2Storage) storeFileMetadata(remotePath, checksum string, size int64, modTime time.Time) error {
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
	
	metadataObj := b.bucket.Object(b.config.BackupPrefix + getMetadataFileName(remotePath))
	w := metadataObj.NewWriter(ctx)
	
	if _, err := w.Write(metadataJSON); err != nil {
		w.Close()
		return err
	}
	
	return w.Close()
}

// 从B2获取文件元数据
func (b *B2Storage) getFileMetadata(remotePath string) (map[string]interface{}, error) {
	ctx := context.Background()
	
	metadataObj := b.bucket.Object(b.config.BackupPrefix + getMetadataFileName(remotePath))
	
	// 尝试获取元数据文件
	reader := metadataObj.NewReader(ctx)
	defer reader.Close()
	
	var metadata map[string]interface{}
	if err := json.NewDecoder(reader).Decode(&metadata); err != nil {
		return nil, err
	}
	
	return metadata, nil
}

// Close 关闭B2连接
func (b *B2Storage) Close() error {
	// B2客户端通常不需要显式关闭，但这里可以添加清理逻辑
	return nil
} 