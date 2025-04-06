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
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	watypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
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

	waClients   = make(map[string]*WhatsAppClient)
	waClientsMux sync.RWMutex

	// WebSocket clients and mutex
	wsClients = make(map[*websocket.Conn]struct{})
	wsClientsMux sync.RWMutex
)

type WhatsAppClient struct {
	Client    *whatsmeow.Client
	QRChannel chan string
	EventConn *websocket.Conn
	Messages  map[string][]MessageInfo
	MessagesMux sync.RWMutex
}

type DeviceInfo struct {
	JID       string    `json:"jid"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"lastSeen"`
	PushName  string    `json:"pushName"`
	Platform  string    `json:"platform"`
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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Participants int       `json:"participants"`
	DeviceID     string    `json:"deviceId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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

// Helper function to parse time from RFC3339 string
func parseTime(timeStr string) time.Time {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Now() // Return current time as fallback
	}
	return t
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
			
			// Set client to be active right after connecting
			if err := client.SendPresence(watypes.PresenceAvailable); err != nil {
				log.Printf("Error setting presence: %v", err)
			}
		} else {
			log.Printf("Device %s has no stored session", device.ID)
		}
	}
}

func broadcastDeviceStatus(deviceID string, connected bool, deviceInfo map[string]interface{}) {
    message := map[string]interface{}{
        "type": "device_status",
        "device": map[string]interface{}{
            "JID":       deviceID,
            "Connected": connected,
            "LastSeen":  time.Now().Format(time.RFC3339),
        },
    }

    // Add additional device info if available
    if deviceInfo != nil {
        for k, v := range deviceInfo {
            message["device"].(map[string]interface{})[k] = v
        }
    }

    // Convert message to JSON
    jsonMessage, err := json.Marshal(message)
    if err != nil {
        log.Printf("Error marshaling device status: %v", err)
        return
    }

    // Broadcast to all connected WebSocket clients
    wsClientsMux.Lock()
    for client := range wsClients {
        err := client.WriteMessage(websocket.TextMessage, jsonMessage)
        if err != nil {
            log.Printf("Error sending device status to client: %v", err)
            client.Close()
            delete(wsClients, client)
        }
    }
    wsClientsMux.Unlock()
}

func handleWebSocket(c *gin.Context) {
    // Upgrade HTTP connection to WebSocket
    upgrader := websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool {
            return true // Allow all origins for testing
        },
    }

    ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        log.Printf("Error upgrading to WebSocket: %v", err)
        return
    }
    defer ws.Close()

    // Add client to connected clients
    wsClientsMux.Lock()
    wsClients[ws] = struct{}{}
    wsClientsMux.Unlock()

    // Send initial device status for all devices
    waClientsMux.RLock()
    for deviceID, client := range waClients {
        if client.Client != nil && client.Client.Store != nil && client.Client.Store.ID != nil {
            deviceInfo := map[string]interface{}{
                "JID":       deviceID,
                "Connected": client.Client.IsConnected(),
                "LastSeen":  time.Now().Format(time.RFC3339),
                "PushName":  client.Client.Store.PushName,
                "Platform":  client.Client.Store.Platform,
            }
            broadcastDeviceStatus(deviceID, client.Client.IsConnected(), deviceInfo)
        }
    }
    waClientsMux.RUnlock()

    // Keep connection alive and handle incoming messages
    for {
        _, _, err := ws.ReadMessage()
        if err != nil {
            log.Printf("Error reading WebSocket message: %v", err)
            break
        }
    }

    // Remove client when connection is closed
    wsClientsMux.Lock()
    delete(wsClients, ws)
    wsClientsMux.Unlock()
}

func setupWhatsAppClient(client *whatsmeow.Client) *WhatsAppClient {
	waClient := &WhatsAppClient{
		Client:    client,
		QRChannel: make(chan string),
		Messages:  make(map[string][]MessageInfo),
	}

	// Enable auto-reconnect
	client.EnableAutoReconnect = true

	// Load existing messages from DynamoDB if client is already connected
	if client.Store.ID != nil {
		// Load messages in background to not block setup
		go func() {
			messages, err := db.GetMessages(client.Store.ID.String(), "", 1000) // Get last 1000 messages
			if err != nil {
				log.Printf("Error loading messages from DynamoDB: %v", err)
				return
			}
			
			// Convert and store messages in memory
			waClient.MessagesMux.Lock()
			for _, msg := range messages {
				messageInfo := MessageInfo{
					ID:        msg.ID,
					FromMe:    msg.FromMe,
					Timestamp: parseTime(msg.Timestamp),
					PushName:  msg.PushName,
					Message:   msg.Content,
					Type:      msg.Type,
					ChatID:    msg.ChatID,
					SenderID:  msg.SenderID,
				}
				waClient.Messages[msg.ChatID] = append(waClient.Messages[msg.ChatID], messageInfo)
			}
			waClient.MessagesMux.Unlock()
			
			log.Printf("Loaded %d messages from DynamoDB for device %s", len(messages), client.Store.ID)
		}()
	}

	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.QR:
			// Handle QR code event
			select {
			case waClient.QRChannel <- string(v.Codes[0]):
			default:
			}
		case *events.Connected:
			log.Printf("Device connected: %s", client.Store.ID)
			
			// Set client to be active
			if err := client.SendPresence(watypes.PresenceAvailable); err != nil {
				log.Printf("Error setting presence: %v", err)
			}
			
			deviceInfo := getDeviceInfo(client)
			
			// Store device info in DynamoDB asynchronously
			go func() {
				if err := db.SaveDevice(db.Device{
					JID:       client.Store.ID.String(),
					Connected: true,
					LastSeen:  time.Now().Format(time.RFC3339),
					PushName:  client.Store.PushName,
					Platform:  client.Store.Platform,
				}); err != nil {
					log.Printf("Error saving device to DynamoDB: %v", err)
				}
			}()
			
			// Update waClients map with the latest client
			waClientsMux.Lock()
			waClients[client.Store.ID.String()] = waClient
			waClientsMux.Unlock()
			
			// Broadcast device status to all connected WebSocket clients
			broadcastDeviceStatus(client.Store.ID.String(), true, deviceInfo)

		case *events.Disconnected:
			if client.Store.ID != nil {
				log.Printf("Device disconnected: %s", client.Store.ID)
				
				deviceInfo := getDeviceInfo(client)
				
				// Update device status in DynamoDB asynchronously
				go func() {
					if err := db.SaveDevice(db.Device{
						JID:       client.Store.ID.String(),
						Connected: false,
						LastSeen:  time.Now().Format(time.RFC3339),
						PushName:  client.Store.PushName,
						Platform:  client.Store.Platform,
					}); err != nil {
						log.Printf("Error updating device status in DynamoDB: %v", err)
					}
				}()
				
				// Broadcast device status to all connected WebSocket clients
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

				log.Printf("Processing history sync for chat %s with %d messages", chatJID.String(), len(conv.GetMessages()))
				for _, msg := range conv.GetMessages() {
					evt, err := client.ParseWebMessage(chatJID, msg.GetMessage())
					if err != nil {
						log.Printf("Error parsing message from history sync: %v", err)
						continue
					}
					
					// Store the message
					waClient.storeMessage(evt)
					
					// Broadcast to connected clients that we received historical messages
					broadcastToClients(map[string]interface{}{
						"type":     "history_sync",
						"deviceId": client.Store.ID.String(),
						"chatId":   chatJID.String(),
						"message":  evt,
					})
				}
			}
		}
	})

	return waClient
}

func getDeviceInfo(client *whatsmeow.Client) map[string]interface{} {
    return map[string]interface{}{
        "PushName":  client.Store.PushName,
        "Platform":  client.Store.Platform,
        "Connected": client.IsConnected(),
    }
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
	var devices []DeviceInfo

	// Get devices from DynamoDB
	dbDevices, err := db.GetAllDevices()
	if err != nil {
		log.Printf("Error fetching devices from DynamoDB: %v", err)
		return devices
	}

	// Convert db.Device to DeviceInfo
	for _, d := range dbDevices {
		devices = append(devices, DeviceInfo{
			JID:       d.JID,
			Connected: d.Connected,
			LastSeen:  parseTime(d.LastSeen),
			PushName:  d.PushName,
			Platform:  d.Platform,
		})
	}

	return devices
}

func cleanupDeviceData(jid string) {
	// Remove from DynamoDB
	err := db.DeleteDevice(jid)
	if err != nil {
		log.Printf("Error deleting device from DynamoDB: %v", err)
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
    deviceId := c.Query("deviceId")
    saveGroups := c.Query("save") == "true"

    if deviceId == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
        return
    }

    waClientsMux.RLock()
    waClient := waClients[deviceId]
    waClientsMux.RUnlock()

    if waClient == nil || waClient.Client == nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "device not connected"})
        return
    }

    // Rate limit check
    if !groupInfoLimiter.Allow() {
        c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
        return
    }

    groups, err := waClient.Client.GetJoinedGroups()
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
            "id":           group.JID.String(),
            "name":         group.Name,
            "participants": len(group.Participants),
            "owner":        group.OwnerJID.String(),
            "creation":     group.GroupCreated.Unix(),
        })
    }

    c.JSON(http.StatusOK, gin.H{
        "status": "success",
        "data": gin.H{
            "groups": groupList,
        },
        "message": fmt.Sprintf("Retrieved %d groups", len(groupList)),
    })
}

func handleSendMessage(c *gin.Context) {
	var req SendMessageRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	waClientsMux.RLock()
	waClient := waClients[req.DeviceID]
	waClientsMux.RUnlock()

	if waClient == nil || waClient.Client == nil {
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

	resp, err := waClient.Client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Create message info
	messageInfo := MessageInfo{
		ID:        resp.ID,
		FromMe:    true,
		Timestamp: time.Now(),
		PushName:  waClient.Client.Store.PushName,
		Message:   req.Message,
		Type:      "text",
		ChatID:    recipient.String(),
		SenderID:  req.DeviceID,
	}

	// Store in memory
	waClient.MessagesMux.Lock()
	if waClient.Messages == nil {
		waClient.Messages = make(map[string][]MessageInfo)
	}
	waClient.Messages[recipient.String()] = append(waClient.Messages[recipient.String()], messageInfo)
	waClient.MessagesMux.Unlock()

	// Save message to DynamoDB
	message := db.Message{
		ID:        resp.ID,
		DeviceID:  req.DeviceID,
		ChatID:    recipient.String(),
		Type:      "text",
		Content:   req.Message,
		Timestamp: time.Now().Format(time.RFC3339),
		FromMe:    true,
		SenderID:  req.DeviceID,
		PushName:  waClient.Client.Store.PushName,
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
    chatID := c.Param("chatId")

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
            // If contact not found, just use the JID as name
            chat = Chat{
                ID:         jid.String(),
                Name:       jid.User,
                IsGroup:    false,
                IsArchived: false,
                IsMuted:    false,
                IsPinned:   false,
            }
        } else {
            chat = Chat{
                ID:         jid.String(),
                Name:       contact.FullName,
                IsGroup:    false,
                IsArchived: false,
                IsMuted:    false,
                IsPinned:   false,
            }
        }
        // Set current time as timestamp
        now := time.Now()
        chat.Timestamp = &now
    }

    c.JSON(http.StatusOK, gin.H{
        "status": "success",
        "data": chat,
        "message": "Chat retrieved successfully",
    })
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

	chatID := evt.Info.Chat.String()
	messageInfo := MessageInfo{
		ID:        evt.Info.ID,
		FromMe:    evt.Info.IsFromMe,
		Timestamp: evt.Info.Timestamp,
		PushName:  evt.Info.PushName,
		Message:   messageContent,
		Type:      messageType,
		ChatID:    chatID,
		SenderID:  evt.Info.Sender.String(),
	}

	// Store in memory
	wa.MessagesMux.Lock()
	if wa.Messages == nil {
		wa.Messages = make(map[string][]MessageInfo)
	}
	wa.Messages[chatID] = append(wa.Messages[chatID], messageInfo)
	log.Printf("Stored message in memory for chat %s (total messages: %d)", chatID, len(wa.Messages[chatID]))
	wa.MessagesMux.Unlock()

	// Store message in DynamoDB if available
	if config.DynamoDBClient != nil {
		message := db.Message{
			ID:        evt.Info.ID,
			DeviceID:  wa.Client.Store.ID.String(),
			ChatID:    chatID,
			Type:      messageType,
			Content:   messageContent,
			Timestamp: evt.Info.Timestamp.Format(time.RFC3339),
			FromMe:    evt.Info.IsFromMe,
			SenderID:  evt.Info.Sender.String(),
			PushName:  evt.Info.PushName,
		}

		if err := db.SaveMessage(message); err != nil {
			log.Printf("Warning: Failed to save message to DynamoDB: %v", err)
		} else {
			log.Printf("Successfully saved message to DynamoDB for chat %s", chatID)
		}

		// Also store/update chat
		chat := db.Chat{
			ID:          chatID,
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

    log.Printf("Getting messages for device: %s, chat: %s, before: %s, limit: %d", deviceId, chatId, beforeId, limit)

    if deviceId == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId is required"})
        return
    }

    waClientsMux.RLock()
    waClient := waClients[deviceId]
    waClientsMux.RUnlock()

    if waClient == nil || waClient.Client == nil {
        log.Printf("Device not found: %s", deviceId)
        c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
        return
    }

    // If chatId is provided, get messages for that specific chat
    if chatId != "" {
        // Validate chat JID
        chatJID, err := parseJID(chatId)
        if err != nil {
            log.Printf("Invalid chat JID: %s, error: %v", chatId, err)
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat JID"})
            return
        }

        // Get messages from memory first
        waClient.MessagesMux.RLock()
        messages, exists := waClient.Messages[chatId]
        if !exists {
            messages = []MessageInfo{}
        }
        waClient.MessagesMux.RUnlock()

        log.Printf("Found %d messages in memory for chat %s", len(messages), chatId)

        // Sort messages by timestamp in descending order (newest first)
        sort.Slice(messages, func(i, j int) bool {
            return messages[i].Timestamp.After(messages[j].Timestamp)
        })

        // Apply limit if specified
        if len(messages) > limit {
            messages = messages[:limit]
        }

        // If we have a beforeId, request history sync
        if beforeId != "" {
            log.Printf("Requesting history sync for messages before %s", beforeId)
            // Request history sync in background
            go func() {
                // Create message info for the last known message
                lastKnownMsg := &watypes.MessageInfo{
                    ID: watypes.MessageID(beforeId),
                    MessageSource: watypes.MessageSource{
                        Chat: chatJID,
                        IsFromMe: false,
                        IsGroup: chatJID.Server == "g.us",
                    },
                }

                // Build and send history sync request
                historyMsg := waClient.Client.BuildHistorySyncRequest(lastKnownMsg, limit)
                _, err := waClient.Client.SendMessage(context.Background(), chatJID, historyMsg, whatsmeow.SendRequestExtra{
                    Peer: true, // Important: This must be true for history sync requests
                })
                if err != nil {
                    log.Printf("Error requesting history sync: %v", err)
                } else {
                    log.Printf("Successfully requested history sync")
                }
            }()
        }

        response := gin.H{
            "status": "success",
            "data": gin.H{
                "messages": messages,
            },
            "message": fmt.Sprintf("Retrieved %d messages", len(messages)),
        }

        log.Printf("Returning response with %d messages", len(messages))
        c.JSON(http.StatusOK, response)
        return
    }

    // If no chatId provided, return all messages across all chats
    var allMessages []MessageInfo
    waClient.MessagesMux.RLock()
    for chatID, chatMessages := range waClient.Messages {
        log.Printf("Found %d messages for chat %s", len(chatMessages), chatID)
        allMessages = append(allMessages, chatMessages...)
    }
    waClient.MessagesMux.RUnlock()

    // Sort all messages by timestamp in descending order
    sort.Slice(allMessages, func(i, j int) bool {
        return allMessages[i].Timestamp.After(allMessages[j].Timestamp)
    })

    // Apply limit if specified
    if len(allMessages) > limit {
        allMessages = allMessages[:limit]
    }

    log.Printf("Returning %d total messages across all chats", len(allMessages))
    c.JSON(http.StatusOK, gin.H{
        "status": "success",
        "data": gin.H{
            "messages": allMessages,
        },
        "message": fmt.Sprintf("Retrieved %d messages", len(allMessages)),
    })
}

func main() {
	// Initialize DynamoDB but don't fail if it's not available
	err := db.InitDynamoDB()
	if err != nil {
		log.Printf("Warning: DynamoDB initialization failed: %v. Continuing with in-memory storage only.", err)
	}

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
		Addr:    ":8081",
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
		
		// Get active devices directly from waClients map
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
				
				// Store device info in DynamoDB asynchronously
				go func(info DeviceInfo) {
					err := db.SaveDevice(db.Device{
						JID:       info.JID,
						Connected: info.Connected,
						LastSeen:  time.Now().Format(time.RFC3339),
						PushName:  info.PushName,
						Platform:  info.Platform,
					})
					if err != nil {
						log.Printf("Error saving device to DynamoDB: %v", err)
					}
				}(deviceInfo)
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

	// Message routes
	messageGroup := r.Group("/messages")
	{
		// Get all messages
		messageGroup.GET("", handleGetMessages)

		// Get messages by chat
		messageGroup.GET("/:chatId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			chatID := c.Param("chatId")
			
			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			waClient.MessagesMux.RLock()
			messages := waClient.Messages[chatID]
			waClient.MessagesMux.RUnlock()

			c.JSON(http.StatusOK, messages)
		})

		// Send text message
		messageGroup.POST("/text", handleSendMessage)

		// Send media message
		messageGroup.POST("/media", handleSendMediaMessage)

		// Send location message
		messageGroup.POST("/location", handleSendLocationMessage)

		// Send reaction
		messageGroup.POST("/reaction", func(c *gin.Context) {
			var req MessageActionRequest
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

			chatJID, err := parseJID(req.ChatID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
				return
			}

			msgID := watypes.MessageID(req.MessageID)
			reaction := waClient.Client.BuildReaction(chatJID, chatJID, msgID, req.Reaction)
			_, err = waClient.Client.SendMessage(context.Background(), chatJID, reaction)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "reaction sent"})
		})

		// Delete message
		messageGroup.DELETE("/:messageId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			messageID := c.Param("messageId")
			chatID := c.Query("chatId")

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			chatJID, err := parseJID(chatID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
				return
			}

			msgID := watypes.MessageID(messageID)
			revoke := waClient.Client.BuildRevoke(chatJID, chatJID, msgID)
			_, err = waClient.Client.SendMessage(context.Background(), chatJID, revoke)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "message deleted"})
		})
	}

	// Chat routes
	chatGroup := r.Group("/chats")
	{
		// Get all chats
		chatGroup.GET("", handleGetChats)

		// Get specific chat
		chatGroup.GET("/:chatId", handleGetChat)

		// Delete chat
		chatGroup.DELETE("/:chatId", handleDeleteChat)

		// Archive/unarchive chat
		chatGroup.PUT("/:chatId/archive", handleArchiveChat)

		// Update chat settings
		chatGroup.PUT("/:chatId/settings", handleUpdateChatSettings)
	}

	// Group management endpoints
	groupGroup := r.Group("/groups")
	{
		// Group participants
		groupGroup.GET("/:groupId/participants", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := parseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
				return
			}

			// Get group info first to get participants
			info, err := waClient.Client.GetGroupInfo(groupJID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Return the participants from group info
			c.JSON(http.StatusOK, info.Participants)
		})

		// Add/remove participants
		groupGroup.POST("/:groupId/participants", func(c *gin.Context) {
			var req GroupParticipantRequest
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

			groupJID, err := parseJID(req.GroupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
				return
			}

			var participants []watypes.JID
			for _, p := range req.Participants {
				jid, err := parseJID(p)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid participant JID: %s", p)})
					return
				}
				participants = append(participants, jid)
			}

			action := c.Query("action")
			var participantAction whatsmeow.ParticipantChange
			switch action {
			case "add":
				participantAction = whatsmeow.ParticipantChangeAdd
			case "remove":
				participantAction = whatsmeow.ParticipantChangeRemove
			case "promote":
				participantAction = whatsmeow.ParticipantChangePromote
			case "demote":
				participantAction = whatsmeow.ParticipantChangeDemote
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid action"})
				return
			}

			result, err := waClient.Client.UpdateGroupParticipants(groupJID, participants, participantAction)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, result)
		})

		// Get group invite link
		groupGroup.GET("/:groupId/invite", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")
			reset := c.Query("reset") == "true"

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := parseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
				return
			}

			link, err := waClient.Client.GetGroupInviteLink(groupJID, reset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"link": link})
		})

		// Join group with invite link
		groupGroup.POST("/:groupId/join", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			code := c.Query("code")

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupID, err := waClient.Client.JoinGroupWithLink(code)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"groupId": groupID.String()})
		})

		// Get group info
		groupGroup.GET("/:groupId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := parseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
				return
			}

			info, err := waClient.Client.GetGroupInfo(groupJID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, info)
		})

		// Leave group
		groupGroup.DELETE("/:groupId", func(c *gin.Context) {
			deviceID := c.Query("deviceId")
			groupID := c.Param("groupId")

			waClientsMux.RLock()
			waClient := waClients[deviceID]
			waClientsMux.RUnlock()

			if waClient == nil || waClient.Client == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
				return
			}

			groupJID, err := parseJID(groupID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group ID"})
				return
			}

			err = waClient.Client.LeaveGroup(groupJID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"status": "left group"})
		})

		// Get all groups
		groupGroup.GET("", handleGetGroups)

		// Create group
		groupGroup.POST("/create", handleCreateGroup)
	}

	// Add QR code endpoint
	r.GET("/qr", func(c *gin.Context) {
		log.Println("QR code endpoint called")
		
		// Create a new device
		device := container.NewDevice()
		if device == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create device"})
			return
		}

		// Create WhatsApp client
		client := whatsmeow.NewClient(device, nil)
		if client == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create WhatsApp client"})
			return
		}

		// Setup client with event handlers
		waClient := setupWhatsAppClient(client)

		// Get QR channel before connecting
		qrChan, err := client.GetQRChannel(context.Background())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get QR channel: %v", err)})
			return
		}

		// Connect to WhatsApp
		err = client.Connect()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to connect: %v", err)})
			return
		}

		log.Println("Waiting for QR code...")

		// Wait for either QR code or successful connection
		select {
		case evt := <-qrChan:
			log.Printf("Received QR event: %+v", evt)
			
			// Store client in waClients map only after successful QR generation
			waClientsMux.Lock()
			if device.ID != nil {
				waClients[device.ID.String()] = waClient
				log.Printf("Stored new device with ID: %s", device.ID.String())
			}
			waClientsMux.Unlock()

			c.JSON(http.StatusOK, gin.H{
				"qr": gin.H{
					"code": evt.Code,
					"timeout": 30000, // 30 seconds timeout
				},
			})
			return

		case <-time.After(30 * time.Second):
			// Clean up if timeout occurs
			client.Disconnect()
			c.JSON(http.StatusRequestTimeout, gin.H{"error": "Timeout waiting for QR code"})
			return
		}
	})

	// Start the server
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}