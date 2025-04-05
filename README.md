# WhatsApp API Integration

A Go-based WhatsApp API integration using the whatsmeow library, with support for multiple devices, message handling, group management, and DynamoDB storage.

## Features

- Multi-device support
- Real-time message handling
- Group management
- Chat management
- Media message support
- DynamoDB integration for persistent storage
- WebSocket for real-time updates
- Modern web interface

## Prerequisites

- Go 1.19 or higher
- AWS Account with DynamoDB access
- Node.js (for frontend development)
- WhatsApp account

## Environment Variables

Create a `.env` file in the root directory with the following variables:

```env
AWS_REGION=your_aws_region
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key
```

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/waani_v5.git
cd waani_v5
```

2. Install Go dependencies:
```bash
go mod download
```

3. Build the project:
```bash
go build -o waani
```

4. Run the server:
```bash
./waani
```

The server will start on `http://localhost:8080` by default.

## API Documentation

### Authentication

- `GET /qr` - Generate QR code for device authentication
- `POST /logout/{deviceId}` - Logout a device

### Messages

- `GET /messages/list` - Get all messages
- `GET /messages/list/{chatId}` - Get messages by chat
- `POST /messages/text` - Send text message
- `POST /messages/{type}` - Send media message (image/video/audio/document)
- `POST /messages/location` - Send location message
- `POST /messages/link_preview` - Send link with preview
- `POST /messages/sticker` - Send sticker
- `POST /messages/story` - Send story

### Groups

- `GET /groups` - Get all groups
- `POST /groups` - Create new group
- `PUT /groups` - Join group
- `GET /groups/{groupId}` - Get group info
- `PUT /groups/{groupId}` - Update group
- `DELETE /groups/{groupId}` - Leave group
- `GET /groups/{groupId}/invite` - Get group invite link
- `POST /groups/{groupId}/participants` - Add participants
- `DELETE /groups/{groupId}/participants` - Remove participants

### Chats

- `GET /chats` - Get all chats
- `GET /chats/{chatId}` - Get specific chat
- `DELETE /chats/{chatId}` - Delete chat
- `POST /chats/{chatId}/archive` - Archive/unarchive chat
- `POST /chats/{chatId}/settings` - Update chat settings

## Web Interface

The project includes two web interfaces:

1. Main Dashboard (`/`):
   - Device management
   - Quick message sending
   - Group management
   - Chat management

2. API Testing Interface (`/api-test`):
   - Interactive API documentation
   - Test all API endpoints
   - Real-time response viewing

## Database Schema

### DynamoDB Tables

1. `waani_devices`:
   - Primary key: `jid` (string)
   - Attributes: connected, pushName, platform, lastSeen

2. `waani_groups`:
   - Primary key: `id` (string)
   - Attributes: name, participants, created, deviceId

3. `waani_messages`:
   - Primary key: `id` (string)
   - Sort key: `timestamp` (number)
   - Attributes: chatId, content, type, sender

4. `waani_chats`:
   - Primary key: `id` (string)
   - Attributes: name, lastMessage, timestamp, unreadCount

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [whatsmeow](https://github.com/tulir/whatsmeow) - Go WhatsApp library
- [Gin Web Framework](https://github.com/gin-gonic/gin)
- [AWS SDK for Go](https://github.com/aws/aws-sdk-go-v2) 