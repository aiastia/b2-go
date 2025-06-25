package main

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// StateManager 状态管理器结构体
type StateManager struct {
	config Config
}

// NewStateManager 创建新的状态管理器实例
func NewStateManager(config Config) *StateManager {
	return &StateManager{
		config: config,
	}
}

// LoadState 加载本地状态
func (sm *StateManager) LoadState() (*LocalState, error) {
	state := &LocalState{
		Files: make(map[string]*FileState),
	}

	if sm.config.LocalStatePath == "" {
		return state, nil
	}

	file, err := os.Open(sm.config.LocalStatePath)
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

// SaveState 保存本地状态
func (sm *StateManager) SaveState(state *LocalState) error {
	if sm.config.LocalStatePath == "" {
		return nil
	}

	// 确保目录存在
	dir := sm.getStateDirectory()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(sm.config.LocalStatePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(state)
}

// UpdateLastBackupTime 更新最后备份时间
func (sm *StateManager) UpdateLastBackupTime(state *LocalState) {
	state.LastBackup = time.Now()
}

// GetLastBackupTime 获取最后备份时间
func (sm *StateManager) GetLastBackupTime(state *LocalState) time.Time {
	return state.LastBackup
}

// AddFile 添加文件到状态
func (sm *StateManager) AddFile(state *LocalState, fileState *FileState) {
	state.Files[fileState.Path] = fileState
}

// RemoveFile 从状态中移除文件
func (sm *StateManager) RemoveFile(state *LocalState, filePath string) {
	delete(state.Files, filePath)
}

// GetFile 获取文件状态
func (sm *StateManager) GetFile(state *LocalState, filePath string) (*FileState, bool) {
	fileState, exists := state.Files[filePath]
	return fileState, exists
}

// UpdateFile 更新文件状态
func (sm *StateManager) UpdateFile(state *LocalState, fileState *FileState) {
	state.Files[fileState.Path] = fileState
}

// GetAllFiles 获取所有文件状态
func (sm *StateManager) GetAllFiles(state *LocalState) map[string]*FileState {
	return state.Files
}

// GetFileCount 获取文件数量
func (sm *StateManager) GetFileCount(state *LocalState) int {
	return len(state.Files)
}

// ClearState 清空状态
func (sm *StateManager) ClearState(state *LocalState) {
	state.Files = make(map[string]*FileState)
	state.LastBackup = time.Time{}
}

// BackupState 备份状态文件
func (sm *StateManager) BackupState() error {
	if sm.config.LocalStatePath == "" {
		return nil
	}

	// 检查状态文件是否存在
	if _, err := os.Stat(sm.config.LocalStatePath); os.IsNotExist(err) {
		return nil // 文件不存在，无需备份
	}

	backupPath := sm.config.LocalStatePath + ".backup"
	
	// 读取原文件
	originalData, err := os.ReadFile(sm.config.LocalStatePath)
	if err != nil {
		return err
	}

	// 写入备份文件
	if err := os.WriteFile(backupPath, originalData, 0644); err != nil {
		return err
	}

	log.Printf("State file backed up to %s", backupPath)
	return nil
}

// RestoreState 恢复状态文件
func (sm *StateManager) RestoreState() error {
	if sm.config.LocalStatePath == "" {
		return nil
	}

	backupPath := sm.config.LocalStatePath + ".backup"
	
	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return nil // 备份文件不存在
	}

	// 读取备份文件
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}

	// 写入原文件
	if err := os.WriteFile(sm.config.LocalStatePath, backupData, 0644); err != nil {
		return err
	}

	log.Printf("State file restored from %s", backupPath)
	return nil
}

// GetStatePath 获取状态文件路径
func (sm *StateManager) GetStatePath() string {
	return sm.config.LocalStatePath
}

// 获取状态文件目录
func (sm *StateManager) getStateDirectory() string {
	// 从完整路径中提取目录部分
	dir := sm.config.LocalStatePath
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return "."
} 