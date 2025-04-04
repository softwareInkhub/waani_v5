# whatsmeow - WhatsApp Web Multidevice API Library for Go

[![Go Reference](https://pkg.go.dev/badge/go.mau.fi/whatsmeow.svg)](https://pkg.go.dev/go.mau.fi/whatsmeow)

whatsmeow is a Go library for the WhatsApp web multidevice API. This documentation provides a comprehensive reference for all components of the library.

## Table of Contents

- [Overview](#overview)
- [Installation](#installation)
- [Features](#features)
- [Basic Usage](#basic-usage)
- [Client](#client)
- [Types](#types)
- [Constants](#constants)
- [Variables](#variables)
- [Utilities](#utilities)
- [Events](#events)
- [Media Handling](#media-handling)
- [Group Management](#group-management)
- [Newsletter Functions](#newsletter-functions)
- [App State Management](#app-state-management)
- [Discussion and Support](#discussion-and-support)

## Overview

Package whatsmeow implements a client for interacting with the WhatsApp web multidevice API. It allows you to programmatically interact with WhatsApp, enabling automation of messaging, group management, and more.

## Installation

```bash
go get go.mau.fi/whatsmeow
```

## Features

### Core Features

- **Messaging**: Send messages to private chats and groups (text, media)
- **Receiving Messages**: Receive all types of messages
- **Group Management**: Create, join, and manage groups
- **Invites**: Join via invite messages, use and create invite links
- **Notifications**: Send and receive typing notifications
- **Receipts**: Send and receive delivery and read receipts
- **State Management**: Read and write app state (contact list, chat pin/mute status, etc.)
- **Error Handling**: Send and handle retry receipts if message decryption fails
- **Status Messages**: Send status messages (experimental)

### Limitations

- Sending broadcast list messages (not supported on WhatsApp Web either)
- Calls functionality is limited

## Basic Usage

Here's a simple example of how to use the library:

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "go.mau.fi/whatsmeow"
    "go.mau.fi/whatsmeow/store/sqlstore"
    "go.mau.fi/whatsmeow/types"
    "go.mau.fi/whatsmeow/types/events"
    waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
    // Create a new device store and client
    dbLog := waLog.Stdout("Database", "DEBUG", true)
    container, err := sqlstore.New("sqlite3", "file:examplestore.db?_foreign_keys=on", dbLog)
    if err != nil {
        panic(err)
    }
    deviceStore, err := container.GetFirstDevice()
    if err != nil {
        panic(err)
    }
    clientLog := waLog.Stdout("Client", "DEBUG", true)
    client := whatsmeow.NewClient(deviceStore, clientLog)

    // Add event handler to receive incoming messages
    client.AddEventHandler(func(evt interface{}) {
        switch v := evt.(type) {
        case *events.Message:
            fmt.Println("Received message:", v.Message.GetConversation())
        }
    })

    // Connect to WhatsApp
    if client.Store.ID == nil {
        // No ID stored, new login
        qrChan, _ := client.GetQRChannel(context.Background())
        err = client.Connect()
        if err != nil {
            panic(err)
        }
        for evt := range qrChan {
            if evt.Event == "code" {
                // Display QR code to user
                fmt.Println("QR code:", evt.Code)
            } else {
                fmt.Println("Login event:", evt.Event)
            }
        }
    } else {
        // Already logged in, just connect
        err = client.Connect()
        if err != nil {
            panic(err)
        }
    }

    // Keep the connection alive until terminated
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c
    client.Disconnect()
}
```

## Client

The `Client` struct is the main entry point for interacting with WhatsApp. It provides methods for all operations.

### Creating a New Client

```go
func NewClient(deviceStore *store.Device, log waLog.Logger) *Client
```

Creates a new WhatsApp client with the provided device store and logger.

### Connection Management

- `Connect() error` - Connect to the WhatsApp servers
- `Disconnect()` - Disconnect from the WhatsApp servers
- `IsConnected() bool` - Check if the client is connected
- `IsLoggedIn() bool` - Check if the client is logged in
- `Logout() error` - Log out the current session
- `SetPassive(passive bool) error` - Set the client in passive mode
- `WaitForConnection(timeout time.Duration) bool` - Wait for connection with a timeout

### Event Handling

- `AddEventHandler(handler EventHandler) uint32` - Add an event handler
- `RemoveEventHandler(id uint32) bool` - Remove a specific event handler
- `RemoveEventHandlers()` - Remove all event handlers

### Authentication and Pairing

- `GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error)` - Get a QR code channel for authentication
- `PairPhone(phone string, showPushNotification bool, clientType PairClientType, ...) (string, error)` - Pair with a phone
- `ResolveContactQRLink(code string) (*types.ContactQRLinkTarget, error)` - Resolve a contact QR link

### Messaging

- `BuildMessageKey(chat, sender types.JID, id types.MessageID) *waCommon.MessageKey` - Build a message key
- `BuildReaction(chat, sender types.JID, id types.MessageID, reaction string) *waE2E.Message` - Build a reaction message
- `BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message` - Build a message revocation
- `BuildEdit(chat types.JID, id types.MessageID, newContent *waE2E.Message) *waE2E.Message` - Build a message edit
- `SendMessage(ctx context.Context, to types.JID, message *waE2E.Message, ...) (resp SendResponse, err error)` - Send a message
- `SendFBMessage(ctx context.Context, to types.JID, message armadillo.RealMessageApplicationSub, ...) (resp SendResponse, err error)` - Send a Facebook-style message
- `RevokeMessage(chat types.JID, id types.MessageID) (SendResponse, error)` - Revoke a message (deprecated)
- `SendChatPresence(jid types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error` - Send chat presence (typing, etc.)

### Media Handling

- `Download(msg DownloadableMessage) ([]byte, error)` - Download media from a message
- `DownloadThumbnail(msg DownloadableThumbnail) ([]byte, error)` - Download a thumbnail for media
- `DownloadToFile(msg DownloadableMessage, file File) error` - Download media to a file
- `DownloadAny(msg *waE2E.Message) (data []byte, err error)` - Download any media from a message
- `Upload(ctx context.Context, plaintext []byte, appInfo MediaType) (resp UploadResponse, err error)` - Upload media
- `UploadReader(ctx context.Context, plaintext io.Reader, tempFile io.ReadWriteSeeker, ...) (resp UploadResponse, err error)` - Upload media from a reader

### Group Management

- `CreateGroup(req ReqCreateGroup) (*types.GroupInfo, error)` - Create a new group
- `GetJoinedGroups() ([]*types.GroupInfo, error)` - Get all joined groups
- `GetGroupInfo(jid types.JID) (*types.GroupInfo, error)` - Get group information
- `GetGroupInfoFromLink(code string) (*types.GroupInfo, error)` - Get group info from an invite link
- `GetGroupInfoFromInvite(jid, inviter types.JID, code string, expiration int64) (*types.GroupInfo, error)` - Get group info from an invite
- `GetGroupInviteLink(jid types.JID, reset bool) (string, error)` - Get or reset a group invite link
- `JoinGroupWithLink(code string) (types.JID, error)` - Join a group with an invite link
- `JoinGroupWithInvite(jid, inviter types.JID, code string, expiration int64) error` - Join a group with an invite
- `LeaveGroup(jid types.JID) error` - Leave a group
- `SetGroupName(jid types.JID, name string) error` - Set a group's name
- `SetGroupDescription(jid types.JID, description string) error` - Set a group's description
- `SetGroupPhoto(jid types.JID, avatar []byte) (string, error)` - Set a group's photo
- `SetGroupAnnounce(jid types.JID, announce bool) error` - Set a group's announce mode
- `SetGroupLocked(jid types.JID, locked bool) error` - Set a group's locked mode
- `UpdateGroupParticipants(jid types.JID, participantChanges []types.JID, action ParticipantChange) ([]types.GroupParticipant, error)` - Add/remove participants

### User Information

- `GetUserInfo(jids []types.JID) (map[types.JID]types.UserInfo, error)` - Get user information
- `GetUserDevices(jids []types.JID) ([]types.JID, error)` - Get a user's devices
- `GetProfilePictureInfo(jid types.JID, params *GetProfilePictureParams) (*types.ProfilePictureInfo, error)` - Get profile picture info
- `IsOnWhatsApp(phones []string) ([]types.IsOnWhatsAppResponse, error)` - Check if phone numbers are on WhatsApp
- `GetContactQRLink(revoke bool) (string, error)` - Get a contact QR link

### App State Management

- `FetchAppState(name appstate.WAPatchName, fullSync, onlyIfNotSynced bool) error` - Fetch app state
- `SendAppState(patch appstate.PatchInfo) error` - Send app state update

### Newsletter Functions

- `CreateNewsletter(params CreateNewsletterParams) (*types.NewsletterMetadata, error)` - Create a newsletter
- `GetNewsletterInfo(jid types.JID) (*types.NewsletterMetadata, error)` - Get newsletter information
- `GetNewsletterInfoWithInvite(key string) (*types.NewsletterMetadata, error)` - Get newsletter info from an invite
- `FollowNewsletter(jid types.JID) error` - Follow a newsletter
- `UnfollowNewsletter(jid types.JID) error` - Unfollow a newsletter
- `GetSubscribedNewsletters() ([]*types.NewsletterMetadata, error)` - Get all subscribed newsletters
- `NewsletterToggleMute(jid types.JID, mute bool) error` - Toggle mute status for a newsletter
- `GetNewsletterMessages(jid types.JID, params *GetNewsletterMessagesParams) ([]*types.NewsletterMessage, error)` - Get newsletter messages
- `NewsletterMarkViewed(jid types.JID, serverIDs []types.MessageServerID) error` - Mark newsletter messages as viewed
- `NewsletterSendReaction(jid types.JID, serverID types.MessageServerID, reaction string, ...) error` - Send a reaction to a newsletter message

## Types

### Core Types

#### Client
```go
type Client struct {
    // Contains unexported fields
}
```
The main client for interacting with WhatsApp.

#### APNsPushConfig
```go
type APNsPushConfig struct {
    PushToken  string
    DeviceID   string
    PushPerson string
    Cert       string
    AppID      string
    Key        string
}
```
Configuration for Apple Push Notification service.

#### FCMPushConfig
```go
type FCMPushConfig struct {
    RegisterID string
    Recipient  string
}
```
Configuration for Firebase Cloud Messaging.

#### PushConfig
```go
type PushConfig interface {
    GetPushConfigAttrs() waBinary.Attrs
}
```
Interface for push notification configuration.

### Message Types

#### SendResponse
```go
type SendResponse struct {
    ID           types.MessageID
    Timestamp    time.Time
    ServerID     types.MessageServerID
    DebugTimings types.MessageDebugTimings
}
```
Response after sending a message.

#### UploadResponse
```go
type UploadResponse struct {
    URL           string
    DirectPath    string
    Handle        string
    MediaKey      []byte
    FileEncSHA256 []byte
    FileSHA256    []byte
    FileLength    uint64
    ViewOnce      bool
}
```
Response after uploading media.

### Group Types

#### ReqCreateGroup
```go
type ReqCreateGroup struct {
    Name              string
    Participants      []types.JID
    OptionalFeatures  OptionalFeatures
    ParentGroupID     *types.JID
    CreateParentGroup bool
    AutoSendInvites   *bool
}
```
Parameters for creating a group.

#### ParticipantChange
```go
type ParticipantChange int
```
Type of group participant change (add, remove, promote, demote).

#### ParticipantRequestChange
```go
type ParticipantRequestChange int
```
Type of group participant request change (approve, reject).

### Newsletter Types

#### CreateNewsletterParams
```go
type CreateNewsletterParams struct {
    Name        string
    Description string
    Picture     []byte
}
```
Parameters for creating a newsletter.

#### GetNewsletterMessagesParams
```go
type GetNewsletterMessagesParams struct {
    Newest        *types.MessageServerID
    Oldest        *types.MessageServerID
    Limit         int
    ExcludeNewest bool
    ExcludeOldest bool
}
```
Parameters for getting newsletter messages.

#### GetNewsletterUpdatesParams
```go
type GetNewsletterUpdatesParams struct {
    After  time.Time
    Before time.Time
    Limit  int
}
```
Parameters for getting newsletter updates.

## Constants

### Prekeys
```go
const (
    // WantedPreKeyCount is the number of prekeys that the client should upload to the WhatsApp servers in a single batch.
    WantedPreKeyCount = 50
    // MinPreKeyCount is the number of prekeys when the client will upload a new batch of prekeys to the WhatsApp servers.
    MinPreKeyCount = 5
)
```

### Disappearing Messages Timers
```go
const (
    DisappearingTimerOff     = time.Duration(0)
    DisappearingTimer24Hours = 24 * time.Hour
    DisappearingTimer7Days   = 7 * 24 * time.Hour
    DisappearingTimer90Days  = 90 * 24 * time.Hour
)
```

### URL Prefixes
```go
const (
    BusinessMessageLinkPrefix       = "https://wa.me/message/"
    ContactQRLinkPrefix             = "https://wa.me/qr/"
    BusinessMessageLinkDirectPrefix = "https://api.whatsapp.com/message/"
    ContactQRLinkDirectPrefix       = "https://api.whatsapp.com/qr/"
    NewsletterLinkPrefix            = "https://whatsapp.com/channel/"
    InviteLinkPrefix                = "https://chat.whatsapp.com/"
)
```

### Message Editing
```go
const EditWindow = 20 * time.Minute
```
Specifies how long a message can be edited after it was sent.

### Facebook Message Versions
```go
const (
    FBMessageVersion = 3
    FBMessageApplicationVersion = 2
    FBConsumerMessageVersion = 1
    FBArmadilloMessageVersion = 1
)
```

### QR Channel Events
```go
const (
    QRChannelEventCode = "code"
    QRChannelEventError = "error"
)
```

### Reactions
```go
const RemoveReactionText = ""
```
Empty string to remove reactions.

## Variables

### Error Variables

#### General Errors
```go
var (
    ErrClientIsNil     = errors.New("client is nil")
    ErrNoSession       = errors.New("can't encrypt message for device: no signal session established")
    ErrIQTimedOut      = errors.New("info query timed out")
    ErrNotConnected    = errors.New("websocket not connected")
    ErrNotLoggedIn     = errors.New("the store doesn't contain a device JID")
    ErrMessageTimedOut = errors.New("timed out waiting for message send response")
    ErrAlreadyConnected = errors.New("websocket is already connected")
    ErrQRAlreadyConnected = errors.New("GetQRChannel must be called before connecting")
    ErrQRStoreContainsID  = errors.New("GetQRChannel can only be called when there's no user ID in the client's Store")
    ErrNoPushName = errors.New("can't send presence without PushName set")
    ErrNoPrivacyToken = errors.New("no privacy token stored")
    ErrAppStateUpdate = errors.New("server returned error updating app state")
)
```

#### Pairing Errors
```go
var (
    ErrPairInvalidDeviceIdentityHMAC = errors.New("invalid device identity HMAC in pair success message")
    ErrPairInvalidDeviceSignature    = errors.New("invalid device signature in pair success message")
    ErrPairRejectedLocally           = errors.New("local PrePairCallback rejected pairing")
)
```

## Utilities

### Message Utilities

- `GenerateMessageID() types.MessageID` - Generate a message ID (deprecated)
- `ParseDisappearingTimerString(val string) (time.Duration, bool)` - Parse disappearing message timer string
- `DecryptMediaRetryNotification(evt *events.MediaRetry, mediaKey []byte) (*waMmsRetry.MediaRetryNotification, error)` - Decrypt media retry notifications
- `HashPollOptions(optionNames []string) [][]byte` - Hash poll options for integrity

## Events

The whatsmeow library uses an event-based system for receiving updates from WhatsApp. Here are the main event types:

- `Message` - Received a new message
- `Receipt` - Received a message receipt (read, delivery)
- `ChatPresence` - Chat presence updates (typing, etc.)
- `Presence` - User presence updates (online, offline)
- `JoinedGroup` - Joined a group
- `GroupInfo` - Group information updates
- `GroupParticipants` - Group participant changes

### Handling Events

```go
client.AddEventHandler(func(evt interface{}) {
    switch v := evt.(type) {
    case *events.Message:
        // Handle new message
    case *events.Receipt:
        // Handle receipt
    case *events.Presence:
        // Handle presence update
    // Add more cases as needed
    }
})
```

## Media Handling

### Downloading Media

```go
// Download media from a message
data, err := client.Download(message.GetImageMessage())
if err != nil {
    // Handle error
}

// Download to a file
file, _ := os.Create("image.jpg")
defer file.Close()
err = client.DownloadToFile(message.GetImageMessage(), file)
```

### Uploading Media

```go
// Read image file
data, _ := ioutil.ReadFile("image.jpg")

// Upload image
uploadResp, err := client.Upload(context.Background(), data, whatsmeow.MediaImage)
if err != nil {
    // Handle error
}

// Create image message
imageMsg := &waE2E.Message{
    ImageMessage: &waE2E.ImageMessage{
        Url:           &uploadResp.URL,
        DirectPath:    &uploadResp.DirectPath,
        MediaKey:      uploadResp.MediaKey,
        FileEncSha256: uploadResp.FileEncSHA256,
        FileSha256:    uploadResp.FileSHA256,
        FileLength:    &uploadResp.FileLength,
        
        Mimetype: proto.String("image/jpeg"),
        // Add more fields as needed
    },
}

// Send the message
client.SendMessage(context.Background(), recipient, imageMsg)
```

## Group Management

### Creating a Group

```go
groupInfo, err := client.CreateGroup(whatsmeow.ReqCreateGroup{
    Name: "My Group",
    Participants: []types.JID{
        {User: "1234567890", Server: types.DefaultUserServer},
        {User: "0987654321", Server: types.DefaultUserServer},
    },
})
```

### Getting Group Info

```go
groupInfo, err := client.GetGroupInfo(types.JID{
    User: "1234567890-1234567890",
    Server: types.GroupServer,
})
```

### Managing Group Members

```go
// Add members
participants, err := client.UpdateGroupParticipants(
    groupJID,
    []types.JID{{User: "1234567890", Server: types.DefaultUserServer}},
    whatsmeow.ParticipantChangeAdd,
)

// Remove members
participants, err = client.UpdateGroupParticipants(
    groupJID,
    []types.JID{{User: "1234567890", Server: types.DefaultUserServer}},
    whatsmeow.ParticipantChangeRemove,
)
```

## Newsletter Functions

```go
// Create a newsletter
newsInfo, err := client.CreateNewsletter(whatsmeow.CreateNewsletterParams{
    Name: "My Newsletter",
    Description: "Daily updates",
    Picture: profilePicData,
})

// Follow a newsletter
err := client.FollowNewsletter(newsletterJID)

// Get newsletter messages
messages, err := client.GetNewsletterMessages(newsletterJID, &whatsmeow.GetNewsletterMessagesParams{
    Limit: 10,
})
```

## App State Management

App state includes contacts, chat settings, and other user data.

```go
// Fetch contact list
err := client.FetchAppState(appstate.WAPatchCriticalBlock, true, false)

// Fetch chat settings (mute settings, pin status, etc.)
err = client.FetchAppState(appstate.WAPatchRegular, true, false)
```

## Discussion and Support

Matrix room: [#whatsmeow:maunium.net](https://matrix.to/#/%23whatsmeow:maunium.net)

For questions about the WhatsApp protocol (like how to send a specific type of message), you can also use the [WhatsApp protocol Q&A](https://github.com/tulir/whatsmeow/discussions/categories/whatsapp-protocol-q-a) section on GitHub discussions.