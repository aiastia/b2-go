# B2-Go Backup Tool

一个用Go语言编写的Backblaze B2备份工具，支持增量备份、文件变化检测和邮件通知。

## 功能特性

- 🔄 **增量备份**: 只备份发生变化的文件
- ⏰ **智能频率控制**: 可配置备份间隔，避免过于频繁的备份
- 📧 **邮件通知**: 备份完成后发送邮件通知
- 🗑️ **保留策略**: 自动删除过期的备份文件
- 🔍 **文件过滤**: 支持排除特定文件模式
- 📊 **状态跟踪**: 本地保存备份状态，提高效率

## 安装

1. 确保已安装Go 1.21或更高版本
2. 克隆项目并安装依赖：

```bash
git clone <repository-url>
cd b2-go
go mod tidy
```

## 配置

创建 `.env` 文件并配置以下环境变量：

```env
# 源目录路径
SOURCE_DIR=/path/to/your/source/directory

# Backblaze B2 配置
B2_BUCKET_NAME=your-bucket-name
B2_ACCOUNT_ID=your-account-id
B2_APPLICATION_KEY=your-application-key

# 备份配置
BACKUP_PREFIX=backups/
RETENTION_DAYS=30           # 文件保留天数

# 同步配置
SYNC_DELETE=true            # 是否同步删除本地已删除的文件
EXCLUDE_PATTERNS=*.tmp,*.log,.git/*  # 排除的文件模式，用逗号分隔

# 本地状态文件路径
LOCAL_STATE_PATH=/var/backup/state.json

# 邮件通知配置（可选）
ENABLE_EMAIL_NOTIFICATION=false  # 是否启用邮件通知，默认关闭
SMTP_SERVER=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-app-password
EMAIL_FROM=your-email@gmail.com
EMAIL_TO=admin@example.com
```

## 使用方法

### 编译

```bash
go build -o b2-backup main.go
```

### 运行

```bash
./b2-backup
```

### 定时运行

**重要**: 本程序设计为单次执行，建议使用系统定时任务来控制运行频率：

### Linux/macOS (cron)

```bash
# 每小时运行一次
0 * * * * /path/to/b2-backup

# 每天凌晨2点运行
0 2 * * * /path/to/b2-backup

# 每30分钟运行一次
*/30 * * * * /path/to/b2-backup
```

### Windows (任务计划程序)

1. 打开任务计划程序
2. 创建基本任务
3. 设置触发器为每天或每小时
4. 设置操作为启动程序：`C:\path\to\b2-backup.exe`

## 配置说明

### ENABLE_EMAIL_NOTIFICATION

- **默认值**: false (关闭)
- **说明**: 控制是否启用邮件通知功能
- **示例**: 
  - `true`: 启用邮件通知
  - `false`: 关闭邮件通知

### 文件排除模式

支持以下排除模式：
- `*.tmp`: 排除所有.tmp文件
- `*.log`: 排除所有.log文件
- `.git/*`: 排除.git目录下的所有文件
- `temp/`: 排除temp目录下的所有文件

## 工作原理

1. **文件扫描**: 扫描源目录，与本地状态比较
2. **变化检测**: 通过文件大小、修改时间和校验和检测变化
3. **增量上传**: 只上传发生变化的文件
4. **状态更新**: 更新本地状态文件
5. **保留清理**: 删除过期的备份文件
6. **邮件通知**: 发送备份结果通知（如果启用）

## 日志输出

程序会输出详细的日志信息，包括：
- 备份开始和结束时间
- 文件变化检测结果
- 上传/删除的文件数量
- 邮件通知状态
- 错误信息（如果有）

## 注意事项

1. 确保有足够的磁盘空间存储本地状态文件
2. 定期检查日志文件，确保备份正常运行
3. 建议在首次运行前测试配置是否正确
4. 邮件通知需要配置正确的SMTP服务器信息

## 故障排除

### 常见问题

1. **认证失败**: 检查B2_ACCOUNT_ID和B2_APPLICATION_KEY是否正确
2. **权限错误**: 确保对源目录有读取权限
3. **网络问题**: 检查网络连接和防火墙设置
4. **邮件发送失败**: 检查SMTP配置和邮箱设置

### 调试模式

可以通过设置环境变量来启用更详细的日志：

```bash
export DEBUG=true
./b2-backup
```