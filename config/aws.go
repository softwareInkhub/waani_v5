package config

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/joho/godotenv"
)

var (
    DynamoDBClient *dynamodb.Client
)

func InitAWS() error {
    // Load .env file
    if err := godotenv.Load(); err != nil {
        log.Printf("Warning: .env file not found: %v", err)
    }

    // Load AWS credentials from environment variables
    accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
    secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
    region := os.Getenv("AWS_REGION")

    // Debug logging
    log.Printf("AWS Configuration:")
    log.Printf("Region: %s", region)
    log.Printf("Access Key ID: %s", accessKey)
    if secretKey != "" {
        log.Printf("Secret Access Key: [REDACTED]")
    } else {
        log.Printf("Secret Access Key: not set")
    }

    if region == "" {
        region = "us-east-1" // Default region
    }

    // Validate credentials
    if accessKey == "" || secretKey == "" {
        return fmt.Errorf("AWS credentials not found in environment variables")
    }

    // Create AWS config with explicit credentials
    cfg, err := config.LoadDefaultConfig(context.TODO(),
        config.WithRegion(region),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
            accessKey,
            secretKey,
            "",
        )),
    )
    if err != nil {
        return fmt.Errorf("failed to load AWS config: %v", err)
    }

    // Create DynamoDB client
    DynamoDBClient = dynamodb.NewFromConfig(cfg)

    // Test connection
    _, err = DynamoDBClient.ListTables(context.TODO(), &dynamodb.ListTablesInput{
        Limit: aws.Int32(1),
    })
    if err != nil {
        return fmt.Errorf("failed to connect to DynamoDB: %v", err)
    }

    log.Printf("Successfully connected to DynamoDB")
    return nil
} 