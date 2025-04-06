package db

import (
    "context"
    "errors"
    "fmt"
    "log"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
    "waani/config"
)

var (
    DevicesTable  = "waani_devices"
    GroupsTable   = "waani_groups"
    MessagesTable = "waani_messages"
    ChatsTable    = "waani_chats"
)

// Device represents a WhatsApp device in DynamoDB
type Device struct {
    JID       string `dynamodbav:"jid"`
    Connected bool   `dynamodbav:"connected"`
    LastSeen  string `dynamodbav:"last_seen"`
    PushName  string `dynamodbav:"push_name"`
    Platform  string `dynamodbav:"platform"`
}

// Group represents a WhatsApp group in DynamoDB
type Group struct {
    ID           string `dynamodbav:"id"`
    Name         string `dynamodbav:"name"`
    Participants int    `dynamodbav:"participants"`
    DeviceID     string `dynamodbav:"device_id"`
    CreatedAt    string `dynamodbav:"created_at"`
    UpdatedAt    string `dynamodbav:"updated_at"`
}

// Message represents a WhatsApp message in DynamoDB
type Message struct {
    ID        string `json:"id" dynamodbav:"id"`
    DeviceID  string `json:"device_id" dynamodbav:"device_id"`
    ChatID    string `json:"chat_id" dynamodbav:"chat_id"`
    Content   string `json:"content" dynamodbav:"content"`
    Type      string `json:"type" dynamodbav:"type"`
    FromMe    bool   `json:"from_me" dynamodbav:"from_me"`
    Timestamp string `json:"timestamp" dynamodbav:"timestamp"`
    SenderID  string `json:"sender_id" dynamodbav:"sender_id"`
    PushName  string `json:"push_name" dynamodbav:"push_name"`
}

// Chat represents a WhatsApp chat in DynamoDB
type Chat struct {
    ID          string `json:"id" dynamodbav:"id"`
    DeviceID    string `json:"device_id" dynamodbav:"device_id"`
    Name        string `json:"name" dynamodbav:"name"`
    Type        string `json:"type" dynamodbav:"type"`
    LastMessage string `json:"last_message" dynamodbav:"last_message"`
    UpdatedAt   string `json:"updated_at" dynamodbav:"updated_at"`
}

func InitDynamoDB() error {
    // Initialize AWS configuration
    if err := config.InitAWS(); err != nil {
        return fmt.Errorf("failed to initialize AWS config: %v", err)
    }

    // Create tables if they don't exist
    tables := []struct {
        name       string
        attributes []types.AttributeDefinition
        keySchema  []types.KeySchemaElement
    }{
        {
            name: DevicesTable,
            attributes: []types.AttributeDefinition{
                {
                    AttributeName: aws.String("jid"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
            },
            keySchema: []types.KeySchemaElement{
                {
                    AttributeName: aws.String("jid"),
                    KeyType:      types.KeyTypeHash,
                },
            },
        },
        {
            name: GroupsTable,
            attributes: []types.AttributeDefinition{
                {
                    AttributeName: aws.String("id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
                {
                    AttributeName: aws.String("device_id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
            },
            keySchema: []types.KeySchemaElement{
                {
                    AttributeName: aws.String("id"),
                    KeyType:      types.KeyTypeHash,
                },
                {
                    AttributeName: aws.String("device_id"),
                    KeyType:      types.KeyTypeRange,
                },
            },
        },
        {
            name: MessagesTable,
            attributes: []types.AttributeDefinition{
                {
                    AttributeName: aws.String("device_id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
                {
                    AttributeName: aws.String("chat_id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
            },
            keySchema: []types.KeySchemaElement{
                {
                    AttributeName: aws.String("device_id"),
                    KeyType:      types.KeyTypeHash,
                },
                {
                    AttributeName: aws.String("chat_id"),
                    KeyType:      types.KeyTypeRange,
                },
            },
        },
        {
            name: ChatsTable,
            attributes: []types.AttributeDefinition{
                {
                    AttributeName: aws.String("device_id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
                {
                    AttributeName: aws.String("id"),
                    AttributeType: types.ScalarAttributeTypeS,
                },
            },
            keySchema: []types.KeySchemaElement{
                {
                    AttributeName: aws.String("device_id"),
                    KeyType:      types.KeyTypeHash,
                },
                {
                    AttributeName: aws.String("id"),
                    KeyType:      types.KeyTypeRange,
                },
            },
        },
    }

    log.Printf("Creating DynamoDB tables...")
    for _, table := range tables {
        input := &dynamodb.CreateTableInput{
            TableName:            aws.String(table.name),
            AttributeDefinitions: table.attributes,
            KeySchema:           table.keySchema,
            BillingMode:         types.BillingModePayPerRequest,
        }

        _, err := config.DynamoDBClient.CreateTable(context.TODO(), input)
        if err != nil {
            var resourceInUseErr *types.ResourceInUseException
            if errors.As(err, &resourceInUseErr) {
                log.Printf("Table %s already exists", table.name)
                continue
            }
            log.Printf("Error creating table %s: %v", table.name, err)
            // Continue with other tables even if one fails
            continue
        }
        log.Printf("Created table %s", table.name)
    }

    return nil
}

// GetDevices retrieves all devices for a given JID
func GetDevices(jid string) ([]Device, error) {
    var input *dynamodb.QueryInput
    
    if jid != "" {
        // Query for specific device
        input = &dynamodb.QueryInput{
            TableName: aws.String(DevicesTable),
            KeyConditionExpression: aws.String("jid = :jid"),
            ExpressionAttributeValues: map[string]types.AttributeValue{
                ":jid": &types.AttributeValueMemberS{Value: jid},
            },
        }
    } else {
        // Scan for all devices
        scanInput := &dynamodb.ScanInput{
            TableName: aws.String(DevicesTable),
        }
        
        result, err := config.DynamoDBClient.Scan(context.TODO(), scanInput)
        if err != nil {
            return nil, err
        }

        var devices []Device
        err = attributevalue.UnmarshalListOfMaps(result.Items, &devices)
        return devices, err
    }

    result, err := config.DynamoDBClient.Query(context.TODO(), input)
    if err != nil {
        return nil, err
    }

    var devices []Device
    err = attributevalue.UnmarshalListOfMaps(result.Items, &devices)
    return devices, err
}

// GetAllDevices retrieves all devices from DynamoDB
func GetAllDevices() ([]Device, error) {
    return GetDevices("")  // Empty JID means get all devices
}

// SaveDevice saves or updates a device in DynamoDB
func SaveDevice(device Device) error {
    // Add timestamp if not set
    if device.LastSeen == "" {
        device.LastSeen = time.Now().Format(time.RFC3339)
    }

    av, err := attributevalue.MarshalMap(device)
    if err != nil {
        return fmt.Errorf("failed to marshal device: %v", err)
    }

    _, err = config.DynamoDBClient.PutItem(context.TODO(), &dynamodb.PutItemInput{
        TableName: aws.String(DevicesTable),
        Item:      av,
    })

    if err != nil {
        return fmt.Errorf("failed to save device: %v", err)
    }

    log.Printf("Successfully saved/updated device %s in DynamoDB", device.JID)
    return nil
}

// DeleteDevice deletes a device from DynamoDB
func DeleteDevice(jid string) error {
    input := &dynamodb.DeleteItemInput{
        TableName: aws.String(DevicesTable),
        Key: map[string]types.AttributeValue{
            "jid": &types.AttributeValueMemberS{Value: jid},
        },
    }

    _, err := config.DynamoDBClient.DeleteItem(context.TODO(), input)
    if err != nil {
        return fmt.Errorf("failed to delete device: %v", err)
    }

    log.Printf("Successfully deleted device %s from DynamoDB", jid)
    return nil
}

// SaveGroup saves a group to DynamoDB
func SaveGroup(group Group) error {
    av, err := attributevalue.MarshalMap(group)
    if err != nil {
        return fmt.Errorf("failed to marshal group: %v", err)
    }

    _, err = config.DynamoDBClient.PutItem(context.TODO(), &dynamodb.PutItemInput{
        TableName: aws.String(GroupsTable),
        Item:      av,
    })
    return err
}

// SaveMessage saves a message to DynamoDB
func SaveMessage(message Message) error {
    av, err := attributevalue.MarshalMap(message)
    if err != nil {
        return fmt.Errorf("failed to marshal message: %v", err)
    }

    _, err = config.DynamoDBClient.PutItem(context.TODO(), &dynamodb.PutItemInput{
        TableName: aws.String(MessagesTable),
        Item:      av,
    })
    return err
}

// SaveChat saves a chat to DynamoDB
func SaveChat(chat Chat) error {
    av, err := attributevalue.MarshalMap(chat)
    if err != nil {
        return fmt.Errorf("failed to marshal chat: %v", err)
    }

    _, err = config.DynamoDBClient.PutItem(context.TODO(), &dynamodb.PutItemInput{
        TableName: aws.String(ChatsTable),
        Item:      av,
    })
    return err
}

// GetGroups retrieves all groups for a given device ID
func GetGroups(deviceID string) ([]Group, error) {
    input := &dynamodb.QueryInput{
        TableName: aws.String(GroupsTable),
        KeyConditionExpression: aws.String("device_id = :device_id"),
        ExpressionAttributeValues: map[string]types.AttributeValue{
            ":device_id": &types.AttributeValueMemberS{Value: deviceID},
        },
    }

    result, err := config.DynamoDBClient.Query(context.TODO(), input)
    if err != nil {
        return nil, err
    }

    var groups []Group
    err = attributevalue.UnmarshalListOfMaps(result.Items, &groups)
    return groups, err
}

// GetMessages retrieves messages from DynamoDB for a given device ID and optionally a chat ID
func GetMessages(deviceID string, chatID string, limit int) ([]Message, error) {
    var input *dynamodb.QueryInput

    if chatID != "" {
        // Query for specific chat
        input = &dynamodb.QueryInput{
            TableName: aws.String(MessagesTable),
            KeyConditionExpression: aws.String("device_id = :device_id AND chat_id = :chat_id"),
            ExpressionAttributeValues: map[string]types.AttributeValue{
                ":device_id": &types.AttributeValueMemberS{Value: deviceID},
                ":chat_id":   &types.AttributeValueMemberS{Value: chatID},
            },
            ScanIndexForward: aws.Bool(false), // Sort in descending order (newest first)
            Limit:           aws.Int32(int32(limit)),
        }
    } else {
        // Query for all chats of the device using the timestamp index
        input = &dynamodb.QueryInput{
            TableName: aws.String(MessagesTable),
            IndexName: aws.String("timestamp-index"),
            KeyConditionExpression: aws.String("device_id = :device_id"),
            ExpressionAttributeValues: map[string]types.AttributeValue{
                ":device_id": &types.AttributeValueMemberS{Value: deviceID},
            },
            ScanIndexForward: aws.Bool(false), // Sort in descending order (newest first)
            Limit:           aws.Int32(int32(limit)),
        }
    }

    result, err := config.DynamoDBClient.Query(context.TODO(), input)
    if err != nil {
        return nil, fmt.Errorf("failed to query messages: %v", err)
    }

    var messages []Message
    err = attributevalue.UnmarshalListOfMaps(result.Items, &messages)
    if err != nil {
        return nil, fmt.Errorf("failed to unmarshal messages: %v", err)
    }

    return messages, nil
}

func CreateTables() error {
    // Create Messages table
    _, err := config.DynamoDBClient.CreateTable(context.Background(), &dynamodb.CreateTableInput{
        TableName: aws.String(MessagesTable),
        AttributeDefinitions: []types.AttributeDefinition{
            {
                AttributeName: aws.String("device_id"),
                AttributeType: types.ScalarAttributeTypeS,
            },
            {
                AttributeName: aws.String("chat_id"),
                AttributeType: types.ScalarAttributeTypeS,
            },
            {
                AttributeName: aws.String("timestamp"),
                AttributeType: types.ScalarAttributeTypeS,
            },
        },
        KeySchema: []types.KeySchemaElement{
            {
                AttributeName: aws.String("device_id"),
                KeyType:      types.KeyTypeHash,
            },
            {
                AttributeName: aws.String("chat_id"),
                KeyType:      types.KeyTypeRange,
            },
        },
        GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
            {
                IndexName: aws.String("timestamp-index"),
                KeySchema: []types.KeySchemaElement{
                    {
                        AttributeName: aws.String("device_id"),
                        KeyType:      types.KeyTypeHash,
                    },
                    {
                        AttributeName: aws.String("timestamp"),
                        KeyType:      types.KeyTypeRange,
                    },
                },
                Projection: &types.Projection{
                    ProjectionType: types.ProjectionTypeAll,
                },
            },
        },
        BillingMode: types.BillingModePayPerRequest,
    })
    if err != nil {
        var resourceInUseErr *types.ResourceInUseException
        if !errors.As(err, &resourceInUseErr) {
            return fmt.Errorf("failed to create messages table: %v", err)
        }
        log.Printf("Table %s already exists", MessagesTable)
    }

    // Create Chats table
    _, err = config.DynamoDBClient.CreateTable(context.Background(), &dynamodb.CreateTableInput{
        TableName: aws.String(ChatsTable),
        AttributeDefinitions: []types.AttributeDefinition{
            {
                AttributeName: aws.String("device_id"),
                AttributeType: types.ScalarAttributeTypeS,
            },
            {
                AttributeName: aws.String("id"),
                AttributeType: types.ScalarAttributeTypeS,
            },
        },
        KeySchema: []types.KeySchemaElement{
            {
                AttributeName: aws.String("device_id"),
                KeyType:      types.KeyTypeHash,
            },
            {
                AttributeName: aws.String("id"),
                KeyType:      types.KeyTypeRange,
            },
        },
        BillingMode: types.BillingModePayPerRequest,
    })
    if err != nil {
        var resourceInUseErr *types.ResourceInUseException
        if !errors.As(err, &resourceInUseErr) {
            return fmt.Errorf("failed to create chats table: %v", err)
        }
        log.Printf("Table %s already exists", ChatsTable)
    }

    return nil
} 