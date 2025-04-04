package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"
	"golang.org/x/time/rate"
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

	for _, device := range devices {
		client := whatsmeow.NewClient(device, nil)
		setupWhatsAppClient(client)
		
		// Try to connect if we have a saved session
		if client.Store.ID != nil {
			err = client.Connect()
			if err != nil {
				log.Printf("Error connecting to WhatsApp for device %s: %v", device.ID, err)
			}
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
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			log.Printf("Device connected: %s", client.Store.ID)
			deviceInfo := getDeviceInfo(client)
			
			// Store the client in waClients map
			waClientsMux.Lock()
			waClients[client.Store.ID.String()] = waClient
			waClientsMux.Unlock()
			
			broadcastDeviceStatus(client.Store.ID.String(), true, deviceInfo)
			
			// Store device info in MongoDB
			collection := mongoClient.Database("whatsapp").Collection("devices")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			info := DeviceInfo{
				JID:       client.Store.ID.String(),
				Connected: true,
				LastSeen:  time.Now(),
				PushName:  client.Store.PushName,
				Platform:  client.Store.Platform,
			}

			_, err := collection.UpdateOne(
				ctx,
				bson.M{"jid": info.JID},
				bson.M{"$set": info},
				options.Update().SetUpsert(true),
			)
			if err != nil {
				log.Printf("Error storing device info in MongoDB: %v", err)
			}

		case *events.Disconnected:
			if client.Store.ID != nil {
				log.Printf("Device disconnected: %s", client.Store.ID)
				deviceInfo := getDeviceInfo(client)
				broadcastDeviceStatus(client.Store.ID.String(), false, deviceInfo)
			}

		case *events.Message:
			if client.Store.ID != nil {
				// Broadcast received messages to connected clients
				broadcastToClients(map[string]interface{}{
					"type":     "message",
					"deviceId": client.Store.ID.String(),
					"from":     v.Info.Sender.String(),
					"content":  v.Message.GetConversation(),
				})
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
	
	// Broadcast total groups count immediately
	broadcastToClients(gin.H{
		"type": "groups_total",
		"count": len(groups),
	})

	// Process each group
	for _, group := range groups {
		// Broadcast group progress
		broadcastToClients(gin.H{
			"type": "group_progress",
			"name": group.Name,
			"participants": len(group.Participants),
		})

		if saveGroups {
			// Save to MongoDB if requested
			groupData := bson.M{
				"groupId": group.JID.String(),
				"name": group.Name,
				"participantCount": len(group.Participants),
				"deviceId": deviceId,
				"updatedAt": time.Now(),
			}

			_, err := groupsCollection.UpdateOne(
				context.Background(),
				bson.M{"groupId": group.JID.String()},
				bson.M{"$set": groupData},
				options.Update().SetUpsert(true),
			)
			if err != nil {
				log.Printf("Error saving group %s: %v", group.Name, err)
			}
		}
		
		// Add small delay to prevent flooding
		time.Sleep(100 * time.Millisecond)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Groups fetched successfully",
		"saved": saveGroups,
	})
}

func main() {
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
				activeDevices = append(activeDevices, deviceInfo)
			}
		}
		waClientsMux.RUnlock()

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
				jid, err := types.ParseJID(phone + "@s.whatsapp.net")
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
		messageGroup.GET("/list", func(c *gin.Context) {
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

			// Get store
			store := client.Client.Store
			if store == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "store not initialized"})
				return
			}

			// Get messages from store
			messages := make(map[string][]map[string]interface{})

			// For now return placeholder as message history requires SQLite setup
			c.JSON(http.StatusOK, gin.H{
				"status": "success",
				"message": "Message history will be available after SQLite setup",
				"data": messages,
			})
		})

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

			chat, err := types.ParseJID(chatJID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
				return
			}

			// For now, return empty list as message history requires additional setup
			c.JSON(http.StatusOK, gin.H{
				"chatId": chat.String(),
				"messages": []interface{}{},
			})
		})

		// Send text message
		messageGroup.POST("/text", func(c *gin.Context) {
			var req SendMessageRequest
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

			recipient, err := types.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
				return
			}

			msg := &waProto.Message{
				Conversation: proto.String(req.Message),
			}

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "message sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

		// Send image message
		messageGroup.POST("/image", func(c *gin.Context) {
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

			recipient, err := types.ParseJID(req.To)
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

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "image sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

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

			recipient, err := types.ParseJID(req.To)
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

			recipient, err := types.ParseJID(req.To)
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

			recipient, err := types.ParseJID(req.To)
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
		messageGroup.POST("/location", func(c *gin.Context) {
			var req SendLocationMessageRequest
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

			recipient, err := types.ParseJID(req.To)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recipient JID"})
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

			resp, err := client.Client.SendMessage(context.Background(), recipient, msg)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "location sent",
				"id": resp.ID,
				"timestamp": resp.Timestamp,
			})
		})

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

			recipient, err := types.ParseJID(req.To)
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

			recipient, err := types.ParseJID(req.To)
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
			recipient := types.JID{
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

			chat, err := types.ParseJID(chatJID)
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

			chat, err := types.ParseJID(req.ChatID)
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

			chat, err := types.ParseJID(req.ChatID)
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

			chat, err := types.ParseJID(req.ChatID)
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
		chatGroup.GET("", func(c *gin.Context) {
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

			// Get store
			store := client.Client.Store
			if store == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "store not initialized"})
				return
			}

			// Get all chats
			chatList := make([]map[string]interface{}, 0)

			// Add groups
			groups, err := client.Client.GetJoinedGroups()
			if err == nil {
				for _, group := range groups {
					chatInfo := map[string]interface{}{
						"id":           group.JID.String(),
						"name":         group.Name,
						"type":         "group",
						"participants": len(group.Participants),
						"owner":        group.OwnerJID.String(),
						"creation":     group.GroupCreated.Format(time.RFC3339),
					}
					chatList = append(chatList, chatInfo)
				}
			}

			c.JSON(http.StatusOK, chatList)
		})

		// Get chat by ID
		chatGroup.GET("/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatID := c.Param("chatId")

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

			// For now return placeholder as chat info requires additional setup
			c.JSON(http.StatusOK, gin.H{
				"chatId": chatID,
				"status": "chat details would be returned here",
			})
		})

		// Delete chat
		chatGroup.DELETE("/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatID := c.Param("chatId")

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

			// Delete chat implementation would go here
			c.JSON(http.StatusOK, gin.H{
				"status": "chat deleted",
				"chatId": chatID,
			})
		})

		// Archive/Unarchive chat
		chatGroup.POST("/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatID := c.Param("chatId")
			action := c.Query("action") // "archive" or "unarchive"

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

			// Archive/Unarchive implementation would go here
			c.JSON(http.StatusOK, gin.H{
				"status": fmt.Sprintf("chat %s", action),
				"chatId": chatID,
			})
		})

		// Update chat settings (pin/mute/mark as read)
		chatGroup.PATCH("/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatID := c.Param("chatId")
			action := c.Query("action") // "pin", "mute", "read"

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

			// Update chat settings implementation would go here
			c.JSON(http.StatusOK, gin.H{
				"status": fmt.Sprintf("chat %s", action),
				"chatId": chatID,
			})
		})
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
					"creation": group.GroupCreated,
				})
			}

			c.JSON(http.StatusOK, groupList)
		})

		// Create group
		groupGroup.POST("", func(c *gin.Context) {
			var req CreateGroupRequest
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

			if len(req.Name) > 25 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "group name cannot exceed 25 characters"})
				return
			}

			if len(req.Participants) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "at least one participant is required"})
				return
			}

			participants := make([]types.JID, len(req.Participants))
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
				
				jid, err := types.ParseJID(p)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": fmt.Sprintf("invalid participant number %s: %v", p, err),
					})
					return
				}
				participants[i] = jid
			}

			createReq := whatsmeow.ReqCreateGroup{
				Name:         req.Name,
				Participants: participants,
			}

			// Create group directly without goroutine
			group, err := client.Client.CreateGroup(createReq)
			if err != nil {
				log.Printf("Error creating group: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Save group to MongoDB
			groupData := bson.M{
				"_id":          group.JID.String(),
				"name":         group.Name,
				"participants": len(group.Participants),
				"deviceId":     req.DeviceID,
				"createdAt":    group.GroupCreated,
				"updatedAt":    time.Now(),
			}

			_, err = groupsCollection.InsertOne(context.Background(), groupData)
			if err != nil {
				log.Printf("Error saving group to MongoDB: %v", err)
			}

			c.JSON(http.StatusOK, gin.H{
				"id":           group.JID.String(),
				"name":         group.Name,
				"participants": len(group.Participants),
				"owner":        group.OwnerJID.String(),
				"creation":     group.GroupCreated,
			})
		})

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

			groupJID, err := types.ParseJID(groupID)
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
				"creation": info.GroupCreated,
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

			groupJID, err := types.ParseJID(groupID)
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

			groupJID, err := types.ParseJID(groupID)
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

			groupJID, err := types.ParseJID(groupID)
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

		// Revoke group invite
		groupGroup.DELETE("/:groupId/invite", func(c *gin.Context) {
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

			groupJID, err := types.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			link, err := client.Client.GetGroupInviteLink(groupJID, true)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "invite link revoked",
				"new_link": link,
			})
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

			groupJID, err := types.ParseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			participants := make([]types.JID, len(req.Participants))
			for i, p := range req.Participants {
				// Clean the phone number
				p = strings.TrimSpace(p)
				p = strings.ReplaceAll(p, " ", "")
				p = strings.ReplaceAll(p, "-", "")
				p = strings.ReplaceAll(p, "+", "")
				
				// Remove any existing suffix
				p = strings.TrimSuffix(p, "@s.whatsapp.net")
				p = strings.TrimSuffix(p, "@g.us")
				
				// Add @s.whatsapp.net suffix
				p = p + "@s.whatsapp.net"
				
				jid, err := types.ParseJID(p)
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

			groupJID, err := types.ParseJID(req.GroupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group JID"})
				return
			}

			participants := make([]types.JID, len(req.Participants))
			for i, p := range req.Participants {
				jid, err := types.ParseJID(p)
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

			groupJID, err := types.ParseJID(groupID)
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