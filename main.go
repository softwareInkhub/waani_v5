package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	watypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"
	"golang.org/x/time/rate"
	"waani/db"
	"waani/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	clients    = make(map[*websocket.Conn]bool)
	clientsMux sync.Mutex
	groupsCollection *mongo.Collection

	waClients   = make(map[string]*WhatsAppClient)
	waClientsMux sync.RWMutex
)

type WhatsAppClient struct {
	Client    *whatsmeow.Client
	QRChannel chan string
	EventConn *websocket.Conn
	Messages  map[string][]MessageInfo
	MessagesMux sync.RWMutex
}

type DeviceInfo struct {
	JID       string    `bson:"jid"`
	Connected bool      `bson:"connected"`
	LastSeen  time.Time `bson:"lastSeen"`
	PushName  string    `bson:"pushName"`
	Platform  string    `bson:"platform"`
}

// Contact represents a WhatsApp contact
type Contact struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phoneNumber"`
	About       string `json:"about,omitempty"`
	Status      string `json:"status"`
	Picture     string `json:"picture,omitempty"`
}

// Chat represents a WhatsApp chat
type Chat struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	LastMessage string     `json:"lastMessage,omitempty"`
	Timestamp   *time.Time `json:"timestamp,omitempty"`
	UnreadCount int        `json:"unread"`
	IsGroup     bool       `json:"isGroup"`
	IsArchived  bool       `json:"isArchived"`
	IsMuted     bool       `json:"isMuted"`
	IsPinned    bool       `json:"isPinned"`
}

type ChatResponse struct {
	Chats []Chat `json:"chats"`
}

type ChatActionRequest struct {
	Archive  *bool `json:"archive,omitempty"`
	Pin      *bool `json:"pin,omitempty"`
	Mute     *bool `json:"mute,omitempty"`
	MarkRead *bool `json:"markRead,omitempty"`
}

var (
	mongoClient *mongo.Client
	container    *sqlstore.Container
)

// Message request structures
type SendMessageRequest struct {
	DeviceID string `json:"deviceId"`
	To       string `json:"to"`
	Message  string `json:"message"`
}

type SendMediaMessageRequest struct {
	DeviceID string `json:"deviceId"`
	To       string `json:"to"`
	Caption  string `json:"caption,omitempty"`
	File     []byte `json:"file"`
}

type SendLocationMessageRequest struct {
	DeviceID   string  `json:"deviceId"`
	To         string  `json:"to"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Name       string  `json:"name,omitempty"`
	Address    string  `json:"address,omitempty"`
}

type SendLinkPreviewRequest struct {
	DeviceID    string `json:"deviceId"`
	To          string `json:"to"`
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// Additional message request structures
type SendStickerRequest struct {
	DeviceID string `json:"deviceId"`
	To       string `json:"to"`
	File     []byte `json:"file"`
}

type SendStoryRequest struct {
	DeviceID string `json:"deviceId"`
	Caption  string `json:"caption,omitempty"`
	File     []byte `json:"file,omitempty"`
	Text     string `json:"text,omitempty"`
}

type MessageActionRequest struct {
	DeviceID  string `json:"deviceId"`
	MessageID string `json:"messageId"`
	ChatID    string `json:"chatId"`
	Reaction  string `json:"reaction,omitempty"`
}

// Group represents a WhatsApp group
type Group struct {
	ID           string    `bson:"_id" json:"id"`
	Name         string    `bson:"name" json:"name"`
	Participants int         `bson:"participants" json:"participants"`
	DeviceID     string    `bson:"deviceId" json:"deviceId"`
	CreatedAt    time.Time   `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time   `bson:"updatedAt" json:"updatedAt"`
}

// Add rate limiter
var groupInfoLimiter = rate.NewLimiter(rate.Every(1*time.Second), 1)

// Group management request structures
type CreateGroupRequest struct {
	DeviceID     string   `json:"deviceId"`
	Name         string   `json:"name"`
	Participants []string `json:"participants"`
}

type GroupParticipantRequest struct {
	DeviceID    string   `json:"deviceId"`
	GroupID     string   `json:"groupId"`
	Participants []string `json:"participants"`
}

// MessageInfo struct
type MessageInfo struct {
	ID        string    `json:"id"`
	FromMe    bool      `json:"fromMe"`
	Timestamp time.Time `json:"timestamp"`
	PushName  string    `json:"pushName"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	ChatID    string    `json:"chatId"`
	SenderID  string    `json:"senderId"`
}

// Helper function to parse JID strings
func parseJID(jidStr string) (watypes.JID, error) {
	return watypes.ParseJID(jidStr)
}

func initMongoDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	mongoClient = client
	// Initialize the groups collection
	groupsCollection = mongoClient.Database("whatsapp").Collection("groups")
}

func broadcastToClients(message map[string]interface{}) {
	clientsMux.Lock()
	defer clientsMux.Unlock()

	for client := range clients {
		err := client.WriteJSON(message)
		if err != nil {
			log.Printf("Error broadcasting to client: %v", err)
			client.Close()
			delete(clients, client)
		}
	}
}

func initWhatsAppContainer() {
	var err error
	dbPath := "whatsapp.db"
	
	// Create SQLite container
	container, err = sqlstore.New("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)", nil)
	if err != nil {
		log.Fatal(err)
	}

	// Load existing devices from the database
	devices, err := container.GetAllDevices()
	if err != nil {
		log.Printf("Error loading devices: %v", err)
		return
	}

	log.Printf("Found %d devices in database", len(devices))

	for _, device := range devices {
		client := whatsmeow.NewClient(device, nil)
		waClient := setupWhatsAppClient(client)
		
		// Store in global map before connecting
		waClientsMux.Lock()
		waClients[device.ID.String()] = waClient
		waClientsMux.Unlock()
		
		// Try to connect if we have a saved session
		if client.Store.ID != nil {
			log.Printf("Attempting to connect device: %s", device.ID)
			err = client.Connect()
			if err != nil {
				log.Printf("Error connecting to WhatsApp for device %s: %v", device.ID, err)
				continue
			}
			log.Printf("Successfully connected device: %s", device.ID)
		} else {
			log.Printf("Device %s has no stored session", device.ID)
		}
	}
}

func handleWebSocket(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Error upgrading to websocket: %v", err)
		return
	}
	defer ws.Close()

	log.Println("New WebSocket connection established")

	clientsMux.Lock()
	clients[ws] = true
	clientsMux.Unlock()

	// Send current status of all devices
	waClientsMux.RLock()
	for deviceID, client := range waClients {
		if client.Client != nil {
			deviceInfo := getDeviceInfo(client.Client)
			log.Printf("Sending initial device status for %s: %+v", deviceID, deviceInfo)
			
			err := ws.WriteJSON(map[string]interface{}{
				"type":       "status",
				"deviceId":   deviceID,
				"connected":  client.Client.IsConnected(),
				"deviceInfo": deviceInfo,
			})
			if err != nil {
				log.Printf("Error sending device status: %v", err)
			}
		}
	}
	waClientsMux.RUnlock()

	// Keep connection alive and handle incoming messages
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			log.Printf("WebSocket connection closed: %v", err)
			break
		}
	}

	clientsMux.Lock()
	delete(clients, ws)
	clientsMux.Unlock()
	log.Println("WebSocket connection removed from clients")
}

func broadcastDeviceStatus(deviceID string, connected bool, deviceInfo map[string]interface{}) {
	log.Printf("Broadcasting device status for %s: connected=%v", deviceID, connected)
	
	// Convert to consistent casing
	status := map[string]interface{}{
		"type":      "status",
		"deviceId":  deviceID,
		"connected": connected,
		"deviceInfo": map[string]interface{}{
			"JID":       deviceID,
			"Connected": connected,
			"PushName":  deviceInfo["pushName"],
			"Platform":  deviceInfo["platform"],
		},
	}

	broadcastToClients(status)
}

func setupWhatsAppClient(client *whatsmeow.Client) *WhatsAppClient {
	waClient := &WhatsAppClient{
		Client:    client,
		QRChannel: make(chan string),
		Messages:  make(map[string][]MessageInfo),
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			log.Printf("Device connected: %s", client.Store.ID)
			deviceInfo := getDeviceInfo(client)
			
			// Update device info in DynamoDB
			if err := db.SaveDevice(db.Device{
				JID:       client.Store.ID.String(),
				Connected: true,
				LastSeen:  time.Now().Format(time.RFC3339),
				PushName:  client.Store.PushName,
				Platform:  client.Store.Platform,
			}); err != nil {
				log.Printf("Error saving device to DynamoDB: %v", err)
			}
			
			// Get all contacts and store them as chats
			contacts, err := client.Store.Contacts.GetAllContacts()
			if err != nil {
				log.Printf("Error getting contacts: %v", err)
			} else {
				for jid, contact := range contacts {
					chat := db.Chat{
						ID:          jid.String(),
						DeviceID:    client.Store.ID.String(),
						Name:        contact.FullName,
						Type:        "private",
						LastMessage: "",
						UpdatedAt:   time.Now().Format(time.RFC3339),
					}

					if err := db.SaveChat(chat); err != nil {
						log.Printf("Error saving chat to DynamoDB: %v", err)
					}
				}
			}
			
			broadcastDeviceStatus(client.Store.ID.String(), true, deviceInfo)

		case *events.Disconnected:
			if client.Store.ID != nil {
				log.Printf("Device disconnected: %s", client.Store.ID)
				
				// Update device status in DynamoDB
				if err := db.SaveDevice(db.Device{
					JID:       client.Store.ID.String(),
					Connected: false,
					LastSeen:  time.Now().Format(time.RFC3339),
					PushName:  client.Store.PushName,
					Platform:  client.Store.Platform,
				}); err != nil {
					log.Printf("Error updating device status in DynamoDB: %v", err)
				}
				
				deviceInfo := getDeviceInfo(client)
				broadcastDeviceStatus(client.Store.ID.String(), false, deviceInfo)
			}

		case *events.Message:
			if client.Store.ID != nil {
				// Store message
				waClient.storeMessage(v)

				// Broadcast received messages to connected clients
				broadcastToClients(map[string]interface{}{
					"type":     "message",
					"deviceId": client.Store.ID.String(),
					"from":     v.Info.Sender.String(),
					"content":  v.Message.GetConversation(),
				})
			}

		case *events.HistorySync:
			log.Printf("Received history sync with %d conversations", len(v.Data.Conversations))
			for _, conv := range v.Data.Conversations {
				chatJID, err := watypes.ParseJID(conv.GetId())
				if err != nil {
					log.Printf("Error parsing chat JID from history sync: %v", err)
					continue
				}

				for _, msg := range conv.GetMessages() {
					evt, err := client.ParseWebMessage(chatJID, msg.GetMessage())
					if err != nil {
						log.Printf("Error parsing message from history sync: %v", err)
						continue
					}
					waClient.storeMessage(evt)
				}
			}
		}
	})

	return waClient
}

func getDeviceInfo(client *whatsmeow.Client) map[string]interface{} {
	if client == nil || client.Store == nil {
		log.Println("Warning: Attempted to get device info for nil client or store")
		return nil
	}

	info := map[string]interface{}{
		"Connected": client.IsConnected(),
	}

	if client.Store.ID != nil {
		info["JID"] = client.Store.ID.String()
		info["PhoneNumber"] = client.Store.ID.User
		info["PushName"] = client.Store.PushName
		info["Platform"] = client.Store.Platform
		
		log.Printf("Device info for %s: connected=%v, pushName=%s, platform=%s", 
			client.Store.ID.String(),
			client.IsConnected(),
			client.Store.PushName,
			client.Store.Platform,
		)
	} else {
		log.Println("Warning: Client store ID is nil")
	}

	return info
}

func updateDeviceInfo(deviceID string, connected bool, deviceInfo map[string]interface{}) {
	waClientsMux.Lock()
	if client, ok := waClients[deviceID]; ok && client.Client != nil {
		// Update any relevant device info in the waClients map
		waClients[deviceID] = client
	}
	waClientsMux.Unlock()
}

func getAllDevices() []DeviceInfo {
	collection := mongoClient.Database("whatsapp").Collection("devices")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var devices []DeviceInfo
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Printf("Error fetching devices: %v", err)
		return devices
	}
	defer cursor.Close(ctx)

	err = cursor.All(ctx, &devices)
	if err != nil {
		log.Printf("Error decoding devices: %v", err)
	}
	return devices
}

func cleanupDeviceData(jid string) {
	// Remove from MongoDB
	collection := mongoClient.Database("whatsapp").Collection("devices")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := collection.DeleteOne(ctx, bson.M{"jid": jid})
	if err != nil {
		log.Printf("Error deleting device info from MongoDB: %v", err)
	}

	// Remove from waClients map
	waClientsMux.Lock()
	delete(waClients, jid)
	waClientsMux.Unlock()
}

func getClientByDeviceId(deviceId string) *whatsmeow.Client {
	waClientsMux.RLock()
	defer waClientsMux.RUnlock()
	
	if client, exists := waClients[deviceId]; exists {
		return client.Client
	}
	return nil
}

func handleGetGroups(c *gin.Context) {
	deviceId := c.Param("deviceId")
	saveGroups := c.Query("save") == "true"

	log.Printf("Fetching groups for device %s", deviceId)
	
	client := getClientByDeviceId(deviceId)
	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	groups, err := client.GetJoinedGroups()
	if err != nil {
		log.Printf("Error getting groups: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Found %d groups", len(groups))

	// Save groups to DynamoDB if requested
	if saveGroups {
		for _, group := range groups {
			dbGroup := db.Group{
				ID:           group.JID.String(),
				Name:         group.Name,
				Participants: len(group.Participants),
				DeviceID:     deviceId,
				CreatedAt:    time.Now().Format(time.RFC3339),
				UpdatedAt:    time.Now().Format(time.RFC3339),
			}

			if err := db.SaveGroup(dbGroup); err != nil {
				log.Printf("Error saving group to DynamoDB: %v", err)
			}
		}
	}

	// Return groups to client
	var groupList []map[string]interface{}
	for _, group := range groups {
		groupList = append(groupList, map[string]interface{}{
			"id": group.JID.String(),
			"name": group.Name,
			"participants": len(group.Participants),
			"owner": group.OwnerJID.String(),
			"creation": group.GroupCreated.Unix(),
		})
	}

	c.JSON(http.StatusOK, groupList)
}

func handleSendMessage(c *gin.Context) {
	var req SendMessageRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := getClientByDeviceId(req.DeviceID)
	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	recipient, err := parseJID(req.To)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipient JID"})
		return
	}

	msg := &waProto.Message{
		Conversation: proto.String(req.Message),
	}

	resp, err := client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save message to DynamoDB
	message := db.Message{
		ID:        resp.ID,
		DeviceID:  req.DeviceID,
		ChatID:    req.To,
		Type:      "text",
		Content:   req.Message,
		Timestamp: time.Now().Format(time.RFC3339),
		FromMe:    true,
		SenderID:  req.DeviceID,
		PushName:  client.Store.PushName,
	}

	if err := db.SaveMessage(message); err != nil {
		log.Printf("Error saving message to DynamoDB: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Message sent successfully",
		"id":      resp.ID,
	})
}

func handleSendMediaMessage(c *gin.Context) {
	var req SendMediaMessageRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	waClientsMux.RLock()
	client := waClients[req.DeviceID]
	waClientsMux.RUnlock()

	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	recipient, err := parseJID(req.To)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipient JID"})
		return
	}

	uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaImage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	msg := &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			Caption:       proto.String(req.Caption),
			URL:          proto.String(uploaded.URL),
			DirectPath:   proto.String(uploaded.DirectPath),
			MediaKey:     uploaded.MediaKey,
			Mimetype:     proto.String(http.DetectContentType(req.File)),
			FileLength:   proto.Uint64(uint64(len(req.File))),
			FileSHA256:   uploaded.FileSHA256,
			FileEncSHA256: uploaded.FileEncSHA256,
		},
	}

	resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Store message in DynamoDB
	message := db.Message{
		ID:        resp.ID,
		DeviceID:  req.DeviceID,
		ChatID:    req.To,
		Type:      "media",
		Content:   uploaded.URL,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := db.SaveMessage(message); err != nil {
		log.Printf("Error saving media message to DynamoDB: %v", err)
	}

	// Also store chat if it doesn't exist
	chat := db.Chat{
		ID:          req.To,
		DeviceID:    req.DeviceID,
		Name:        req.To, // We'll update this later when we get contact info
		Type:        "private",
		LastMessage: "Media message",
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}

	if err := db.SaveChat(chat); err != nil {
		log.Printf("Error saving chat to DynamoDB: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Media message sent successfully",
		"id":      resp.ID,
	})
}

func handleSendLocationMessage(c *gin.Context) {
	var req SendLocationMessageRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := getClientByDeviceId(req.DeviceID)
	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	recipient, err := parseJID(req.To)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid recipient JID"})
		return
	}

	msg := &waProto.Message{
		LocationMessage: &waProto.LocationMessage{
			DegreesLatitude:  proto.Float64(req.Latitude),
			DegreesLongitude: proto.Float64(req.Longitude),
			Name:            proto.String(req.Name),
			Address:         proto.String(req.Address),
		},
	}

	resp, err := client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save location message to DynamoDB
	message := db.Message{
		ID:        resp.ID,
		DeviceID:  req.DeviceID,
		ChatID:    req.To,
		Type:      "location",
		Content:   fmt.Sprintf("Location: %f, %f - %s", req.Latitude, req.Longitude, req.Name),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := db.SaveMessage(message); err != nil {
		log.Printf("Error saving location message to DynamoDB: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Location message sent successfully",
		"id":      resp.ID,
	})
}

func handleCreateGroup(c *gin.Context) {
	var req CreateGroupRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := getClientByDeviceId(req.DeviceID)
	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	participants := make([]watypes.JID, len(req.Participants))
	for i, p := range req.Participants {
		jid, err := parseJID(p)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid participant JID: %s", p)})
			return
		}
		participants[i] = jid
	}

	createReq := whatsmeow.ReqCreateGroup{
		Name:         req.Name,
		Participants: participants,
	}

	group, err := client.CreateGroup(createReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save group to DynamoDB
	dbGroup := db.Group{
		ID:           group.JID.String(),
		Name:         req.Name,
		Participants: len(participants),
		DeviceID:     req.DeviceID,
		CreatedAt:    time.Now().Format(time.RFC3339),
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}

	if err := db.SaveGroup(dbGroup); err != nil {
		log.Printf("Error saving group to DynamoDB: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Group created successfully",
		"groupId": group.JID.String(),
	})
}

// GET /chats - Get all chats
func handleGetChats(c *gin.Context) {
    deviceId := c.Query("deviceId")
    if deviceId == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
        return
    }

    waClientsMux.RLock()
    client := waClients[deviceId]
    waClientsMux.RUnlock()

    if client == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
        return
    }

    // Get all chats from the store
    store := client.Client.Store
    if store == nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "store not initialized"})
        return
    }

    // Get all contacts
    contacts, err := store.Contacts.GetAllContacts()
    if err != nil {
        log.Printf("Error getting contacts: %v", err)
        contacts = make(map[watypes.JID]watypes.ContactInfo)
    }
    log.Printf("Found %d contacts", len(contacts))

    // Create a map to track which JIDs we've already added
    addedJIDs := make(map[string]bool)
    chats := make([]Chat, 0)

    // First add all active chats from memory
    client.MessagesMux.RLock()
    for chatJID, messages := range client.Messages {
        if len(messages) == 0 {
            continue
        }

        var name string
        var lastMessage string
        var unreadCount int
        isGroup := strings.HasSuffix(chatJID, "@g.us")

        if isGroup {
            // Get group info
            jid, _ := parseJID(chatJID)
            groupInfo, err := client.Client.GetGroupInfo(jid)
            if err == nil && groupInfo != nil {
                name = groupInfo.Name
            } else {
                name = chatJID
                log.Printf("Error getting group info for %s: %v", chatJID, err)
            }
        } else {
            // Get contact info
            jid, _ := parseJID(chatJID)
            contact, ok := contacts[jid]
            if ok {
                name = contact.FullName
                if name == "" {
                    name = contact.PushName
                }
            }
            if name == "" {
                name = chatJID
            }
        }

        // Get last message
        lastMsg := messages[len(messages)-1]
        lastMessage = lastMsg.Message
        unreadCount = 0 // We don't track unread count yet

        chat := Chat{
            ID:          chatJID,
            Name:        name,
            LastMessage: lastMessage,
            Timestamp:   &lastMsg.Timestamp,
            UnreadCount: unreadCount,
            IsGroup:     isGroup,
            IsArchived:  false,
            IsMuted:     false,
            IsPinned:    false,
        }

        chats = append(chats, chat)
        addedJIDs[chatJID] = true
    }
    client.MessagesMux.RUnlock()

    // Then add contacts that don't have active chats
    for jid, contact := range contacts {
        if !addedJIDs[jid.String()] && jid.Server == "s.whatsapp.net" {
            name := contact.FullName
            if name == "" {
                name = contact.PushName
            }
            if name == "" {
                name = jid.User
            }

            chat := Chat{
                ID:          jid.String(),
                Name:        name,
                LastMessage: "",
                Timestamp:   nil,
                UnreadCount: 0,
                IsGroup:     false,
                IsArchived:  false,
                IsMuted:     false,
                IsPinned:    false,
            }

            chats = append(chats, chat)
        }
    }

    // Sort chats by timestamp (most recent first)
    sort.Slice(chats, func(i, j int) bool {
        // If both timestamps are nil, sort by name
        if chats[i].Timestamp == nil && chats[j].Timestamp == nil {
            return chats[i].Name < chats[j].Name
        }
        // If one timestamp is nil, put it after the non-nil one
        if chats[i].Timestamp == nil {
            return false
        }
        if chats[j].Timestamp == nil {
            return true
        }
        // Otherwise sort by timestamp
        return chats[i].Timestamp.After(*chats[j].Timestamp)
    })

    log.Printf("Returning %d total chats", len(chats))
    c.JSON(http.StatusOK, gin.H{
        "status": "success",
        "data": gin.H{
            "chats": chats,
        },
        "message": fmt.Sprintf("Retrieved %d chats", len(chats)),
    })
}

// GET /chats/{ChatID} - Get specific chat
func handleGetChat(c *gin.Context) {
    deviceID := c.Query("deviceId")
    chatID := c.Param("ChatID")

    if deviceID == "" || chatID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and chatId are required"})
        return
    }

    waClientsMux.RLock()
    client := waClients[deviceID]
    waClientsMux.RUnlock()

    if client == nil || client.Client == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
        return
    }

    // Parse JID from chat ID
    jid, err := parseJID(chatID)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
        return
    }

    var chat Chat
    if jid.Server == "g.us" {
        // Get group info
        group, err := client.Client.GetGroupInfo(jid)
        if err != nil {
            c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
            return
        }
        timestamp := time.Unix(group.GroupCreated.Unix(), 0)
        chat = Chat{
            ID:         group.JID.String(),
            Name:       group.Name,
            Timestamp:  &timestamp,
            IsGroup:    true,
            IsArchived: false,
            IsMuted:    false,
            IsPinned:   false,
        }
    } else {
        // Get contact info
        contact, err := client.Client.Store.Contacts.GetContact(jid)
        if err != nil {
            c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
            return
        }
        timestamp := time.Now()
        chat = Chat{
            ID:         jid.String(),
            Name:       contact.FullName,
            Timestamp:  &timestamp,
            IsGroup:    false,
            IsArchived: false,
            IsMuted:    false,
            IsPinned:   false,
        }
    }

    c.JSON(http.StatusOK, chat)
}

// DELETE /chats/{ChatID} - Delete chat
func handleDeleteChat(c *gin.Context) {
	deviceID := c.Query("deviceId")
	chatID := c.Param("ChatID")

	if deviceID == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and chatId are required"})
		return
	}

	waClientsMux.RLock()
	client := waClients[deviceID]
	waClientsMux.RUnlock()

	if client == nil || client.Client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	// Parse JID from chat ID
	jid, err := parseJID(chatID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Since ClearChat is not available, we'll mark all messages as read
	err = client.Client.MarkRead([]watypes.MessageID{}, time.Now(), jid, jid, watypes.ReceiptTypeRead)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear chat: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// POST /chats/{ChatID}/archive - Archive or unarchive chat
func handleArchiveChat(c *gin.Context) {
	// Since SetArchived is not available in the current version,
	// we'll return a not implemented error
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "Archive/unarchive functionality is not available in the current version",
	})
}

// POST /chats/{ChatID}/settings - Update chat settings (pin/mute/mark as read)
func handleUpdateChatSettings(c *gin.Context) {
	deviceID := c.Query("deviceId")
	chatID := c.Param("ChatID")
	var req ChatActionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if deviceID == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and chatId are required"})
		return
	}

	waClientsMux.RLock()
	client := waClients[deviceID]
	waClientsMux.RUnlock()

	if client == nil || client.Client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	// Parse JID from chat ID
	jid, err := parseJID(chatID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	response := make(map[string]interface{})
	response["success"] = true

	// Mark chat as read if specified
	if req.MarkRead != nil && *req.MarkRead {
		err = client.Client.MarkRead([]watypes.MessageID{}, time.Now(), jid, jid, watypes.ReceiptTypeRead)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark chat as read: " + err.Error()})
			return
		}
		response["marked_as_read"] = true
	}

	// Note: Pin and Mute functionality is not available in the current version
	if req.Pin != nil {
		response["pin_status"] = "Pin functionality is not available"
	}
	if req.Mute != nil {
		response["mute_status"] = "Mute functionality is not available"
	}

	c.JSON(http.StatusOK, response)
}

func (wa *WhatsAppClient) storeMessage(evt *events.Message) {
	// Extract message content based on message type
	var messageContent string
	var messageType string

	switch {
	case evt.Message.GetConversation() != "":
		messageContent = evt.Message.GetConversation()
		messageType = "text"
	case evt.Message.GetImageMessage() != nil:
		messageContent = evt.Message.GetImageMessage().GetCaption()
		messageType = "image"
	case evt.Message.GetVideoMessage() != nil:
		messageContent = evt.Message.GetVideoMessage().GetCaption()
		messageType = "video"
	case evt.Message.GetDocumentMessage() != nil:
		messageContent = evt.Message.GetDocumentMessage().GetFileName()
		messageType = "document"
	case evt.Message.GetAudioMessage() != nil:
		messageContent = "Audio message"
		messageType = "audio"
	case evt.Message.GetStickerMessage() != nil:
		messageContent = "Sticker"
		messageType = "sticker"
	case evt.Message.GetLocationMessage() != nil:
		loc := evt.Message.GetLocationMessage()
		messageContent = fmt.Sprintf("Location: %f, %f", loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		messageType = "location"
	default:
		messageContent = "Unknown message type"
		messageType = "unknown"
	}

	messageInfo := MessageInfo{
		ID:        evt.Info.ID,
		FromMe:    evt.Info.IsFromMe,
		Timestamp: evt.Info.Timestamp,
		PushName:  evt.Info.PushName,
		Message:   messageContent,
		Type:      messageType,
		ChatID:    evt.Info.Chat.String(),
		SenderID:  evt.Info.Sender.String(),
	}

	// Store message in DynamoDB if available
	if config.DynamoDBClient != nil {
		message := db.Message{
			ID:        evt.Info.ID,
			DeviceID:  wa.Client.Store.ID.String(),
			ChatID:    evt.Info.Chat.String(),
			Type:      messageType,
			Content:   messageContent,
			Timestamp: evt.Info.Timestamp.Format(time.RFC3339),
			FromMe:    evt.Info.IsFromMe,
			SenderID:  evt.Info.Sender.String(),
			PushName:  evt.Info.PushName,
		}

		if err := db.SaveMessage(message); err != nil {
			log.Printf("Warning: Failed to save message to DynamoDB: %v", err)
		}

		// Also store/update chat
		chat := db.Chat{
			ID:          evt.Info.Chat.String(),
			DeviceID:    wa.Client.Store.ID.String(),
			Name:        evt.Info.PushName,
			Type:        "private",
			LastMessage: messageContent,
			UpdatedAt:   time.Now().Format(time.RFC3339),
		}

		if err := db.SaveChat(chat); err != nil {
			log.Printf("Warning: Failed to save chat to DynamoDB: %v", err)
		}
	}

	// Store in memory for immediate access
	wa.MessagesMux.Lock()
	defer wa.MessagesMux.Unlock()
	
	if wa.Messages == nil {
		wa.Messages = make(map[string][]MessageInfo)
	}
	wa.Messages[evt.Info.Chat.String()] = append(wa.Messages[evt.Info.Chat.String()], messageInfo)
}

func handleGetMessages(c *gin.Context) {
    deviceId := c.Query("deviceId")
    chatId := c.Query("chatId")
    beforeId := c.Query("before") // Message ID to get messages before this
    limit := 50 // Default limit
    
    if limitStr := c.Query("limit"); limitStr != "" {
        if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
            limit = l
        }
    }

    if deviceId == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
        return
    }

    waClientsMux.RLock()
    client := waClients[deviceId]
    waClientsMux.RUnlock()

    if client == nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
        return
    }

    // If chatId is provided, get messages for that specific chat
    if chatId != "" {
        // Validate chat JID
        if _, err := parseJID(chatId); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
            return
        }

        // Get messages from memory first
        client.MessagesMux.RLock()
        messages := client.Messages[chatId]
        client.MessagesMux.RUnlock()

        // Sort messages by timestamp in descending order (newest first)
        sort.Slice(messages, func(i, j int) bool {
            return messages[i].Timestamp.After(messages[j].Timestamp)
        })

        // Filter messages before the given ID if specified
        if beforeId != "" {
            var beforeTime time.Time
            for _, msg := range messages {
                if msg.ID == beforeId {
                    beforeTime = msg.Timestamp
                    break
                }
            }
            if !beforeTime.IsZero() {
                filteredMessages := []MessageInfo{}
                for _, msg := range messages {
                    if msg.Timestamp.Before(beforeTime) {
                        filteredMessages = append(filteredMessages, msg)
                    }
                }
                messages = filteredMessages
            }
        }

        // Apply limit
        if len(messages) > limit {
            messages = messages[:limit]
        }

        // Get the ID of the oldest message for pagination
        var oldestMessageId string
        var oldestTimestamp *time.Time
        if len(messages) > 0 {
            oldestMessage := messages[len(messages)-1]
            oldestMessageId = oldestMessage.ID
            ts := oldestMessage.Timestamp
            oldestTimestamp = &ts
        }

        c.JSON(http.StatusOK, gin.H{
            "status":          "success",
            "data":           messages,
            "message":        fmt.Sprintf("Retrieved %d messages for chat %s", len(messages), chatId),
            "oldestMessageId": oldestMessageId,
            "oldestTimestamp": oldestTimestamp,
            "hasMore":        len(messages) == limit,
        })
        return
    }

    // Get all messages across all chats
    allMessages := []MessageInfo{}
    client.MessagesMux.RLock()
    if client.Messages != nil {
        for _, chatMessages := range client.Messages {
            allMessages = append(allMessages, chatMessages...)
        }
    }
    client.MessagesMux.RUnlock()

    // Sort all messages by timestamp in descending order
    sort.Slice(allMessages, func(i, j int) bool {
        return allMessages[i].Timestamp.After(allMessages[j].Timestamp)
    })

    // Apply limit
    if len(allMessages) > limit {
        allMessages = allMessages[:limit]
    }

    c.JSON(http.StatusOK, gin.H{
        "status":  "success",
        "data":    allMessages,
        "message": fmt.Sprintf("Retrieved %d messages across all chats", len(allMessages)),
    })
}

func main() {
	// Initialize DynamoDB but don't fail if it's not available
	err := db.InitDynamoDB()
	if err != nil {
		log.Printf("Warning: DynamoDB initialization failed: %v. Continuing with in-memory storage only.", err)
	}

	initMongoDB()
	initWhatsAppContainer()

	r := gin.Default()
	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Set content type based on request path
	r.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasSuffix(path, ".html") {
			c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else if !strings.HasPrefix(path, "/static") {
			c.Writer.Header().Set("Content-Type", "application/json")
		}
		c.Next()
	})
	
	// Increase server timeout
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
		ReadTimeout:  3 * time.Minute,
		WriteTimeout: 3 * time.Minute,
	}
	
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("static/index.html")
	})

	r.GET("/api-test", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.File("static/api-test.html")
	})

	r.GET("/ws", handleWebSocket)

	// Add devices endpoints
	r.GET("/devices", func(c *gin.Context) {
		log.Println("Devices endpoint called")
		
		// Get active devices from waClients map
		waClientsMux.RLock()
		activeDevices := make([]DeviceInfo, 0)
		for deviceID, waClient := range waClients {
			if waClient.Client != nil && waClient.Client.Store != nil && waClient.Client.Store.ID != nil {
				deviceInfo := DeviceInfo{
					JID:       deviceID,
					Connected: waClient.Client.IsConnected(),
					LastSeen:  time.Now(),
					PushName:  waClient.Client.Store.PushName,
					Platform:  waClient.Client.Store.Platform,
				}
				log.Printf("Found active device: %+v", deviceInfo)
				
				// Save device info to DynamoDB
				err := db.SaveDevice(db.Device{
					JID:       deviceID,
					Connected: waClient.Client.IsConnected(),
					LastSeen:  time.Now().Format(time.RFC3339),
					PushName:  waClient.Client.Store.PushName,
					Platform:  waClient.Client.Store.Platform,
				})
				if err != nil {
					log.Printf("Error saving device to DynamoDB: %v", err)
				}
				
				activeDevices = append(activeDevices, deviceInfo)
			}
		}
		waClientsMux.RUnlock()

		// If no active devices found in memory, try to get them from DynamoDB
		if len(activeDevices) == 0 {
			devices, err := db.GetDevices("")
			if err != nil {
				log.Printf("Error getting devices from DynamoDB: %v", err)
			} else {
				for _, device := range devices {
					activeDevices = append(activeDevices, DeviceInfo{
						JID:       device.JID,
						Connected: device.Connected,
						LastSeen:  time.Now(),
						PushName:  device.PushName,
						Platform:  device.Platform,
					})
				}
			}
		}

		log.Printf("Returning %d active devices", len(activeDevices))
		c.JSON(http.StatusOK, activeDevices)
	})

	r.DELETE("/devices", func(c *gin.Context) {
		deviceId := c.Query("deviceId")
		
		waClientsMux.RLock()
		waClient, exists := waClients[deviceId]
		waClientsMux.RUnlock()

		if !exists || !waClient.Client.IsConnected() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Device not found or not connected"})
			return
		}

		err := waClient.Client.Logout()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Clean up device data from DynamoDB
		input := &dynamodb.DeleteItemInput{
			TableName: aws.String(db.DevicesTable),
			Key: map[string]dbtypes.AttributeValue{
				"jid": &dbtypes.AttributeValueMemberS{Value: deviceId},
			},
		}

		_, err = config.DynamoDBClient.DeleteItem(context.TODO(), input)
		if err != nil {
			log.Printf("Error deleting device from DynamoDB: %v", err)
		}

		cleanupDeviceData(deviceId)
		
		c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
	})

	// Contact routes
	contactGroup := r.Group("/contacts")
	{
		contactGroup.GET("", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			contacts := waClient.Client.Store.Contacts
			contactMap, err := contacts.GetAllContacts()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var contactList []Contact
			for jid, contact := range contactMap {
				contactList = append(contactList, Contact{
					ID:          jid.String(),
					Name:        contact.FullName,
					PhoneNumber: jid.User,
					About:       "",
					Status:      "",
					Picture:     "",
				})
			}

			c.JSON(http.StatusOK, contactList)
		})

		contactGroup.POST("", func(c *gin.Context) {
			var req struct {
				DeviceID string   `json:"deviceId"`
				Phones   []string `json:"phones"`
			}
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			waClient := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			phoneResults := make(map[string]bool)
			for _, phone := range req.Phones {
				jid, err := watypes.ParseJID(phone + "@s.whatsapp.net")
				if err != nil {
					phoneResults[phone] = false
					continue
				}
				_, err = waClient.Client.Store.Contacts.GetContact(jid)
				phoneResults[phone] = err == nil
			}

			c.JSON(http.StatusOK, phoneResults)
		})
	}

	// Message endpoints
	messageGroup := r.Group("/messages")
	{
		// Get messages
		messageGroup.GET("/list", handleGetMessages)

		// Get messages by chat
		messageGroup.GET("/list/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatJID := c.Param("chatId")

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chat, err := watypes.ParseJID(chatJID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			// For now return empty list as message history requires additional setup
			c.JSON(http.StatusOK, gin.H{
				"chatId": chat.String(),
				"messages": []interface{}{},
			})
		})

		// Send text message
		messageGroup.POST("/text", handleSendMessage)

		// Send image message
		messageGroup.POST("/image", handleSendMediaMessage)

		// Send video message
		messageGroup.POST("/video", func(c *gin.Context) {
			var req SendMediaMessageRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			recipient, err := watypes.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaVideo)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			msg := &waProto.Message{
				VideoMessage: &waProto.VideoMessage{
					Caption:       proto.String(req.Caption),
					URL:          proto.String(uploaded.URL),
					DirectPath:   proto.String(uploaded.DirectPath),
					MediaKey:     uploaded.MediaKey,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Mimetype:     proto.String(http.DetectContentType(req.File)),
				},
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "video sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send audio message
		messageGroup.POST("/audio", func(c *gin.Context) {
			var req SendMediaMessageRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			recipient, err := watypes.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaAudio)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			msg := &waProto.Message{
				AudioMessage: &waProto.AudioMessage{
					URL:          proto.String(uploaded.URL),
					DirectPath:   proto.String(uploaded.DirectPath),
					MediaKey:     uploaded.MediaKey,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Mimetype:     proto.String(http.DetectContentType(req.File)),
				},
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "audio sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send document message
		messageGroup.POST("/document", func(c *gin.Context) {
			var req SendMediaMessageRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			recipient, err := watypes.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaDocument)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			msg := &waProto.Message{
				DocumentMessage: &waProto.DocumentMessage{
					Title:        proto.String(req.Caption),
					URL:          proto.String(uploaded.URL),
					DirectPath:   proto.String(uploaded.DirectPath),
					MediaKey:     uploaded.MediaKey,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Mimetype:     proto.String(http.DetectContentType(req.File)),
				},
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "document sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send location message
		messageGroup.POST("/location", handleSendLocationMessage)

		// Send link preview message
		messageGroup.POST("/link_preview", func(c *gin.Context) {
			var req SendLinkPreviewRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			recipient, err := watypes.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			msg := &waProto.Message{
				ExtendedTextMessage: &waProto.ExtendedTextMessage{
					Text:        proto.String(req.URL),
					MatchedText: proto.String(req.URL),
					Title:      proto.String(req.Title),
					Description: proto.String(req.Description),
				},
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "link preview sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send sticker message
		messageGroup.POST("/sticker", func(c *gin.Context) {
			var req SendStickerRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			recipient, err := watypes.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaImage)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			msg := &waProto.Message{
				StickerMessage: &waProto.StickerMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Mimetype:      proto.String("image/webp"),
				},
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "sticker sent",
				"id":     resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send story
		messageGroup.POST("/story", func(c *gin.Context) {
			var req SendStoryRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			var msg *waProto.Message
			if len(req.File) > 0 {
				uploaded, err := client.Client.Upload(context.Background(), req.File, whatsmeow.MediaImage)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}

				msg = &waProto.Message{
					ImageMessage: &waProto.ImageMessage{
						Caption:       proto.String(req.Caption),
						URL:          proto.String(uploaded.URL),
						DirectPath:   proto.String(uploaded.DirectPath),
						MediaKey:     uploaded.MediaKey,
						FileEncSHA256: uploaded.FileEncSHA256,
						FileSHA256:    uploaded.FileSHA256,
						FileLength:    proto.Uint64(uploaded.FileLength),
						Mimetype:     proto.String(http.DetectContentType(req.File)),
						ViewOnce:     proto.Bool(true),
					},
				}
			} else {
				msg = &waProto.Message{
					Conversation: proto.String(req.Text),
				}
			}

			// Send to "status@broadcast"
			recipient := watypes.JID{
				User:   "status",
				Server: "broadcast",
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "story sent",
				"id":     resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Get message by ID
		messageGroup.GET("/:messageId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			messageID := c.Param("messageId")
			chatJID := c.Query("chatId")

			if deviceID == "" || chatJID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and chatId are required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chat, err := watypes.ParseJID(chatJID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			// For now return a placeholder as message retrieval requires additional setup
			c.JSON(http.StatusOK, gin.H{
				"messageId": messageID,
				"chatId": chat.String(),
				"status": "message details would be returned here",
			})
		})

		// Forward message
		messageGroup.POST("/:messageId", func(c *gin.Context) {
			var req MessageActionRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chat, err := watypes.ParseJID(req.ChatID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			// Forward message implementation would go here
			// For now, just return success
			c.JSON(http.StatusOK, gin.H{
				"status": "message forwarded",
				"messageId": req.MessageID,
				"chatId": chat.String(),
			})
		})

		// React to message
		messageGroup.PUT("/:messageId/reaction", func(c *gin.Context) {
			var req MessageActionRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chat, err := watypes.ParseJID(req.ChatID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			msg := client.Client.BuildReaction(chat, chat, req.MessageID, req.Reaction)
			resp, err := client.Client.SendMessage(context.Background(), chat, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "reaction sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Star message
		messageGroup.PUT("/:messageId/star", func(c *gin.Context) {
			var req MessageActionRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Star message implementation would go here
			c.JSON(http.StatusOK, gin.H{
				"status": "message starred",
				"messageId": req.MessageID,
			})
		})

		// Delete message
		messageGroup.DELETE("/:messageId", func(c *gin.Context) {
			var req MessageActionRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chat, err := watypes.ParseJID(req.ChatID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			msg := client.Client.BuildRevoke(chat, chat, req.MessageID)
			resp, err := client.Client.SendMessage(context.Background(), chat, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "message deleted",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})
	}

	// Chat management endpoints
	chatGroup := r.Group("/chats")
	{
		// Get all chats
		chatGroup.GET("", handleGetChats)

		// Get chat by ID
		chatGroup.GET("/:chatId", handleGetChat)

		// Delete chat
		chatGroup.DELETE("/:chatId", handleDeleteChat)

		// Archive/Unarchive chat
		chatGroup.POST("/:chatId/archive", handleArchiveChat)

		// Update chat settings (pin/mute/mark as read)
		chatGroup.POST("/:chatId/settings", handleUpdateChatSettings)
	}

	// Add get-groups endpoint
	r.GET("/get-groups/:deviceId", handleGetGroups)

	// Group management endpoints
	groupGroup := r.Group("/groups")
	{
		// Get all groups
		groupGroup.GET("", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groups, err := client.Client.GetJoinedGroups()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var groupList []map[string]interface{}
			for _, group := range groups {
				groupList = append(groupList, map[string]interface{}{
					"id": group.JID.String(),
					"name": group.Name,
					"participants": len(group.Participants),
					"owner": group.OwnerJID.String(),
					"creation": group.GroupCreated.Unix(),
				})
			}

			c.JSON(http.StatusOK, groupList)
		})

		// Create group
		groupGroup.POST("", handleCreateGroup)

		// Accept group invite
		groupGroup.PUT("", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			inviteCode := c.Query("code")

			if deviceID == "" || inviteCode == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId and invite code are required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			_, err := client.Client.JoinGroupWithLink(inviteCode)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "joined group successfully"})
		})

		// Group-specific endpoints
		groupGroup.GET("/:groupId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			info, err := client.Client.GetGroupInfo(groupJID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, map[string]interface{}{
				"id": info.JID.String(),
				"name": info.Name,
				"topic": info.Topic,
				"creation": info.GroupCreated.Unix(),
				"owner": info.OwnerJID.String(),
				"participants": len(info.Participants),
				"ephemeralTimer": info.GroupEphemeral,
				"isAnnounce": info.IsAnnounce,
				"isLocked": info.IsLocked,
			})
		})

		// Update group info
		groupGroup.PUT("/:groupId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")
			var updateData struct {
				Name          string `json:"name,omitempty"`
				Topic         string `json:"topic,omitempty"`
				Announce      *bool  `json:"announce,omitempty"`
				Locked       *bool  `json:"locked,omitempty"`
				EphemeralTimer *uint32 `json:"ephemeralTimer,omitempty"`
			}

			if err := c.BindJSON(&updateData); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil || client.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			if updateData.Name != "" {
				err = client.Client.SetGroupName(groupJID, updateData.Name)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update name: %v", err)})
					return
				}
			}

			if updateData.Topic != "" {
				err = client.Client.SetGroupTopic(groupJID, updateData.Topic, "", "")
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update topic: %v", err)})
					return
				}
			}

			if updateData.Announce != nil {
				err = client.Client.SetGroupAnnounce(groupJID, *updateData.Announce)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update announce setting: %v", err)})
					return
				}
			}

			if updateData.Locked != nil {
				err = client.Client.SetGroupLocked(groupJID, *updateData.Locked)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update locked setting: %v", err)})
					return
				}
			}

			c.JSON(http.StatusOK, gin.H{"status": "group updated successfully"})
		})

		// Leave group
		groupGroup.DELETE("/:groupId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			err = client.Client.LeaveGroup(groupJID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "left group successfully"})
		})

		// Get group invite
		groupGroup.GET("/:groupId/invite", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			link, err := client.Client.GetGroupInviteLink(groupJID, false)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"invite_link": link})
		})

		// Add participants
		groupGroup.POST("/:groupId/participants", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")
			var req struct {
				Participants []string `json:"participants"`
			}
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil || client.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			participants := make([]watypes.JID, len(req.Participants))
			for i, p := range req.Participants {
				// Clean the phone number
				p = strings.TrimSpace(p)
				p = strings.ReplaceAll(p, " ", "")
				p = strings.ReplaceAll(p, "-", "")
				p = strings.ReplaceAll(p, "+", "")
				
				// Remove any existing suffix
				p = strings.TrimSuffix(p, "@s.whatsapp.net")
				p = strings.TrimSuffix(p, "@g.us")
				
				// Add @s.whatsapp.net
				p = p + "@s.whatsapp.net"
				
				jid, err := watypes.ParseJID(p)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": fmt.Sprintf("invalid participant number %s: %v", p, err),
					})
					return
				}
				participants[i] = jid
			}

			result, err := client.Client.UpdateGroupParticipants(groupJID, participants, whatsmeow.ParticipantChangeAdd)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "participants added",
				"result": result,
			})
		})

		// Remove participants
		groupGroup.DELETE("/:groupId/participants", func(c *gin.Context) {
			var req GroupParticipantRequest
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			waClientsMux.RLock()
			client := waClients[req.DeviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(req.GroupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			participants := make([]watypes.JID, len(req.Participants))
			for i, p := range req.Participants {
				jid, err := watypes.ParseJID(p)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "invalid participant JID"})
					return
				}
				participants[i] = jid
			}

			result, err := client.Client.UpdateGroupParticipants(groupJID, participants, whatsmeow.ParticipantChangeRemove)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "participants removed",
				"result": result,
			})
		})

		// Get group icon
		groupGroup.GET("/:groupId/icon", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			if deviceID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
				return
			}

			waClientsMux.RLock()
			client := waClients[deviceID]
			waClientsMux.RUnlock()

			if client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := watypes.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			
			pic, err := client.Client.GetProfilePictureInfo(groupJID, &whatsmeow.GetProfilePictureParams{
				Preview: false,
				IsCommunity: false,
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			if pic == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "no group icon found"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"url": pic.URL,
				"id": pic.ID,
				"type": pic.Type,
				"directPath": pic.DirectPath,
			})
		})
	}

	log.Fatal(srv.ListenAndServe())
} 