package main

import (
	"fmt"
	"log"
	"net/smtp"
)

// EmailConfig 邮件配置结构体
type EmailConfig struct {
	Server   string
	Port     int
	User     string
	Password string
	From     string
	To       string
	Enabled  bool
}

// EmailNotification 邮件通知结构体
type EmailNotification struct {
	config EmailConfig
}

// NewEmailNotification 创建新的邮件通知实例
func NewEmailNotification(config EmailConfig) *EmailNotification {
	return &EmailNotification{
		config: config,
	}
}

// SendNotification 发送邮件通知
func (e *EmailNotification) SendNotification(success bool, stats map[string]int) error {
	// 检查是否启用邮件通知
	if !e.config.Enabled {
		log.Println("Email notification disabled")
		return nil
	}
	
	// 检查SMTP配置是否完整
	if e.config.Server == "" || e.config.From == "" || e.config.To == "" {
		log.Println("Email notification skipped: SMTP configuration missing")
		return nil
	}

	subject := "Backup Failed"
	if success {
		subject = "Backup Succeeded"
	}

	// 构建统计信息
	statsMsg := fmt.Sprintf("Files uploaded: %d\nFiles deleted: %d\nFiles skipped: %d",
		stats["uploaded"], stats["deleted"], stats["skipped"])

	body := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\nBackup Summary:\n%s",
		e.config.From, e.config.To, subject, statsMsg)

	auth := smtp.PlainAuth("", e.config.User, e.config.Password, e.config.Server)
	addr := fmt.Sprintf("%s:%d", e.config.Server, e.config.Port)

	err := smtp.SendMail(addr, auth, e.config.From, []string{e.config.To}, []byte(body))
	if err != nil {
		log.Printf("Failed to send email: %v", err)
		return err
	}
	
	log.Println("Email notification sent")
	return nil
}

// SendCustomNotification 发送自定义邮件通知
func (e *EmailNotification) SendCustomNotification(subject, message string) error {
	// 检查是否启用邮件通知
	if !e.config.Enabled {
		log.Println("Email notification disabled")
		return nil
	}
	
	// 检查SMTP配置是否完整
	if e.config.Server == "" || e.config.From == "" || e.config.To == "" {
		log.Println("Email notification skipped: SMTP configuration missing")
		return nil
	}

	body := fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\n\n%s",
		e.config.From, e.config.To, subject, message)

	auth := smtp.PlainAuth("", e.config.User, e.config.Password, e.config.Server)
	addr := fmt.Sprintf("%s:%d", e.config.Server, e.config.Port)

	err := smtp.SendMail(addr, auth, e.config.From, []string{e.config.To}, []byte(body))
	if err != nil {
		log.Printf("Failed to send email: %v", err)
		return err
	}
	
	log.Println("Custom email notification sent")
	return nil
}

// IsEnabled 检查邮件通知是否启用
func (e *EmailNotification) IsEnabled() bool {
	return e.config.Enabled
}

// GetConfig 获取邮件配置
func (e *EmailNotification) GetConfig() EmailConfig {
	return e.config
} 