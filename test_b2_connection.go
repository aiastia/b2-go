package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Backblaze/b2-sdk-go/v2"
	"github.com/joho/godotenv"
)

func main() {
	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found, using system environment variables")
	}

	// 获取配置
	accountID := os.Getenv("B2_ACCOUNT_ID")
	applicationKey := os.Getenv("B2_APPLICATION_KEY")
	bucketName := os.Getenv("B2_BUCKET_NAME")

	if accountID == "" || applicationKey == "" || bucketName == "" {
		log.Fatal("Missing required environment variables: B2_ACCOUNT_ID, B2_APPLICATION_KEY, B2_BUCKET_NAME")
	}

	fmt.Println("Testing B2 connection...")

	// 创建B2客户端
	ctx := context.Background()
	client, err := b2.NewClient(ctx, &b2.ClientOptions{
		AccountID:      accountID,
		ApplicationKey: applicationKey,
	})
	if err != nil {
		log.Fatalf("Failed to create B2 client: %v", err)
	}

	fmt.Println("✅ B2 client created successfully")

	// 测试列出文件
	files, err := client.ListFileNames(ctx, &b2.ListFileNamesRequest{
		BucketName:   bucketName,
		MaxFileCount: 10,
	})
	if err != nil {
		log.Fatalf("Failed to list files: %v", err)
	}

	fmt.Printf("✅ Successfully connected to bucket: %s\n", bucketName)
	fmt.Printf("📁 Found %d files in bucket\n", len(files.Files))

	if len(files.Files) > 0 {
		fmt.Println("📋 Sample files:")
		for i, file := range files.Files {
			if i >= 5 { // 只显示前5个文件
				break
			}
			fmt.Printf("  - %s (size: %d bytes)\n", file.Name, file.ContentLength)
		}
	}

	fmt.Println("🎉 B2 connection test completed successfully!")
} 