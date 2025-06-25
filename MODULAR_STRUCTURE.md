# 模块化代码结构

## 概述

代码已经被重构为模块化结构，每个功能模块都有独立的文件，提高了代码的可维护性、可测试性和可扩展性。

## 文件结构

```
b2-go/
├── main.go              # 主程序入口
├── email.go             # 邮件通知模块
├── b2_storage.go        # B2存储模块
├── file_scanner.go      # 文件扫描模块
├── state_manager.go     # 状态管理模块
├── go.mod               # Go模块文件
├── .env                 # 环境配置文件
├── README.md            # 项目说明
├── DUPLICATE_PREVENTION.md  # 重复预防机制说明
└── MODULAR_STRUCTURE.md     # 本文档
```

## 模块说明

### 1. 主程序模块 (`main.go`)

**职责**：
- 程序入口点
- 配置加载
- 模块协调
- 主流程控制

**主要功能**：
- 加载环境配置
- 创建各个模块实例
- 协调模块间的交互
- 执行备份流程

### 2. 邮件通知模块 (`email.go`)

**职责**：
- 邮件通知功能
- SMTP配置管理
- 邮件发送逻辑

**主要类**：
- `EmailConfig`：邮件配置结构体
- `EmailNotification`：邮件通知结构体

**主要方法**：
- `NewEmailNotification()`：创建邮件通知实例
- `SendNotification()`：发送备份结果通知
- `SendCustomNotification()`：发送自定义通知
- `IsEnabled()`：检查是否启用邮件通知

### 3. B2存储模块 (`b2_storage.go`)

**职责**：
- B2云存储操作
- 文件上传/下载
- 元数据管理
- 保留策略

**主要类**：
- `B2Storage`：B2存储结构体

**主要方法**：
- `NewB2Storage()`：创建B2存储实例
- `UploadFile()`：上传文件到B2
- `DeleteFile()`：删除B2文件
- `GetFileList()`：获取B2文件列表
- `ManageRetention()`：管理备份保留策略
- `Close()`：关闭B2连接

### 4. 文件扫描模块 (`file_scanner.go`)

**职责**：
- 本地文件扫描
- 文件变化检测
- 校验和计算
- 排除规则应用

**主要类**：
- `FileScanner`：文件扫描器结构体

**主要方法**：
- `NewFileScanner()`：创建文件扫描器实例
- `ScanAndCompareFiles()`：扫描并比较文件
- `FindDeletedFiles()`：查找已删除的文件
- `CalculateChecksum()`：计算文件校验和
- `GetFileInfo()`：获取文件信息
- `IsFileExcluded()`：检查文件是否被排除

### 5. 状态管理模块 (`state_manager.go`)

**职责**：
- 本地状态文件管理
- 状态持久化
- 状态查询和更新

**主要类**：
- `StateManager`：状态管理器结构体

**主要方法**：
- `NewStateManager()`：创建状态管理器实例
- `LoadState()`：加载本地状态
- `SaveState()`：保存本地状态
- `UpdateLastBackupTime()`：更新最后备份时间
- `AddFile()`：添加文件到状态
- `RemoveFile()`：从状态中移除文件
- `BackupState()`：备份状态文件
- `RestoreState()`：恢复状态文件

## 模块间交互

```
main.go
├── 创建 StateManager
├── 创建 FileScanner
├── 创建 B2Storage
└── 创建 EmailNotification

StateManager
├── 加载/保存状态文件
└── 管理文件状态

FileScanner
├── 扫描本地文件
├── 计算校验和
└── 检测文件变化

B2Storage
├── 上传文件到B2
├── 删除B2文件
├── 管理元数据
└── 执行保留策略

EmailNotification
└── 发送备份结果通知
```

## 优势

### 1. **可维护性**
- 每个模块职责单一，易于理解和修改
- 代码结构清晰，便于定位问题
- 模块间耦合度低，修改一个模块不影响其他模块

### 2. **可测试性**
- 每个模块可以独立测试
- 可以轻松创建模拟对象进行单元测试
- 测试覆盖率高，代码质量更好

### 3. **可扩展性**
- 可以轻松添加新功能模块
- 可以替换现有模块的实现
- 支持插件化架构

### 4. **可重用性**
- 模块可以在其他项目中重用
- 可以独立发布和维护模块
- 支持微服务架构

## 使用示例

### 基本使用
```go
// 创建各个模块实例
stateManager := NewStateManager(config)
fileScanner := NewFileScanner(config)
b2Storage, _ := NewB2Storage(config)
emailNotifier := NewEmailNotification(emailConfig)

// 使用模块功能
localState, _ := stateManager.LoadState()
changedFiles, _ := fileScanner.ScanAndCompareFiles(localState)
b2Storage.UploadFile(localPath, remotePath, checksum)
emailNotifier.SendNotification(success, stats)
```

### 自定义邮件通知
```go
emailNotifier := NewEmailNotification(emailConfig)
err := emailNotifier.SendCustomNotification("自定义主题", "自定义消息内容")
```

### 状态管理
```go
stateManager := NewStateManager(config)
stateManager.BackupState()  // 备份状态文件
stateManager.RestoreState() // 恢复状态文件
```

## 配置说明

所有模块都使用统一的 `Config` 结构体进行配置，确保配置的一致性和可维护性。

## 错误处理

每个模块都有完善的错误处理机制：
- 返回详细的错误信息
- 记录详细的日志
- 支持错误恢复
- 提供错误统计

## 性能优化

模块化结构支持性能优化：
- 可以独立优化每个模块
- 支持并发处理
- 支持缓存机制
- 支持批量操作

## 未来扩展

基于模块化结构，可以轻松添加新功能：
- 支持多种存储后端
- 支持多种通知方式
- 支持多种文件格式
- 支持分布式部署 