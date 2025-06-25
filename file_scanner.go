package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileScanner 文件扫描器结构体
type FileScanner struct {
	config Config
}

// NewFileScanner 创建新的文件扫描器实例
func NewFileScanner(config Config) *FileScanner {
	return &FileScanner{
		config: config,
	}
}

// ScanAndCompareFiles 扫描本地文件并与状态比较
func (fs *FileScanner) ScanAndCompareFiles(state *LocalState) ([]*FileState, error) {
	var changedFiles []*FileState

	err := filepath.Walk(fs.config.SourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(fs.config.SourceDir, path)
		if err != nil {
			return err
		}

		// 应用排除规则
		if isExcluded(relPath, fs.config.ExcludePatterns) {
			return nil
		}

		// 检查文件是否在状态中
		existing, exists := state.Files[relPath]
		
		// 计算新文件的校验和
		checksum, err := fs.fileChecksum(path)
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

// FindDeletedFiles 查找已删除的文件
func (fs *FileScanner) FindDeletedFiles(state *LocalState) []string {
	var deletedFiles []string

	for relPath := range state.Files {
		localPath := filepath.Join(fs.config.SourceDir, relPath)
		
		// 检查文件是否仍然存在
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			// 检查是否在排除列表中
			if !isExcluded(relPath, fs.config.ExcludePatterns) {
				deletedFiles = append(deletedFiles, relPath)
			}
		}
	}

	return deletedFiles
}

// CalculateChecksum 计算文件校验和
func (fs *FileScanner) CalculateChecksum(filePath string) (string, error) {
	return fs.fileChecksum(filePath)
}

// 计算文件SHA1校验和
func (fs *FileScanner) fileChecksum(path string) (string, error) {
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

// GetFileInfo 获取文件信息
func (fs *FileScanner) GetFileInfo(filePath string) (*FileState, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	relPath, err := filepath.Rel(fs.config.SourceDir, filePath)
	if err != nil {
		return nil, err
	}

	checksum, err := fs.fileChecksum(filePath)
	if err != nil {
		return nil, err
	}

	return &FileState{
		Path:     relPath,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Checksum: checksum,
		BackedUp: false,
	}, nil
}

// IsFileExcluded 检查文件是否被排除
func (fs *FileScanner) IsFileExcluded(filePath string) bool {
	relPath, err := filepath.Rel(fs.config.SourceDir, filePath)
	if err != nil {
		return true // 如果无法获取相对路径，则排除
	}
	return isExcluded(relPath, fs.config.ExcludePatterns)
}

// GetSourceDirectory 获取源目录
func (fs *FileScanner) GetSourceDirectory() string {
	return fs.config.SourceDir
}

// GetExcludePatterns 获取排除模式
func (fs *FileScanner) GetExcludePatterns() []string {
	return fs.config.ExcludePatterns
} 