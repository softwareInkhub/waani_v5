# WhatsApp API Interface (WAANI v5)

A modern, scalable WhatsApp API interface that enables managing multiple WhatsApp accounts using the whatsmeow library. This API provides a robust interface for WhatsApp automation and integration.

## Features

- Multi-device WhatsApp management with real-time status updates
- QR code-based device authentication
- Message handling (send/receive text, media, location)
- Group management (create, join, leave, update)
- Real-time WebSocket updates for device status
- DynamoDB integration for message persistence
- SQLite for session storage
- Comprehensive test suite with automated test reporting
- Rate limiting for API endpoints
- Modern web interface with real-time updates

## Tech Stack

- Backend: Go 1.24+
- Databases: 
  - DynamoDB (message storage)
  - SQLite (whatsmeow session storage)
- Real-time: WebSocket for live updates
- WhatsApp: whatsmeow library
- Testing: Go testing framework with custom reporting

## Prerequisites

1. Go 1.24 or later
2. AWS account with DynamoDB access
3. Git for version control
4. SQLite (usually comes with the OS)

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

3. Create a `.env` file in the project root:
   ```env
   # Required Configuration
   AWS_ACCESS_KEY_ID=your_access_key
   AWS_SECRET_ACCESS_KEY=your_secret_key
   AWS_REGION=your_region  # defaults to us-east-1
   ```

## Running the Application

1. First, verify your setup:
   ```bash
   # Check Go installation
   go version  # Should show Go 1.24 or later

   # Verify environment variables
   cat .env  # Should show your AWS credentials
   ```

2. Start the server:
   ```bash
   # Give execute permission to scripts
   chmod +x scripts/start_server.sh scripts/run_tests.sh

   # Start the server
   ./scripts/start_server.sh
   ```

3. The server will be available at:
   - API: http://localhost:8081
   - WebSocket: ws://localhost:8081/ws

## Server Startup Troubleshooting

If you encounter issues during server startup, follow these steps:

1. **"Waiting for server to be ready..." message persists**:
   - Check if port 8081 is available:
     ```bash
     lsof -i :8081  # On Linux/Mac
     # or
     netstat -ano | findstr :8081  # On Windows
     ```
   - If port is in use, kill the process or change the port

2. **AWS Configuration Issues**:
   - Verify your AWS credentials:
     ```bash
     # Test AWS credentials
     aws configure list
     aws sts get-caller-identity
     ```
   - Ensure DynamoDB tables exist in your AWS region
   - Check AWS permissions for DynamoDB access

3. **Server Fails to Start**:
   - Check server logs:
     ```bash
     # Start server in foreground for debugging
     go run main.go
     ```
   - Verify SQLite file permissions:
     ```bash
     # Ensure write permissions in directory
     ls -l whatsapp.db
     chmod 666 whatsapp.db  # If needed
     ```

4. **Common Solutions**:
   - Clear any existing database files:
     ```bash
     rm -f whatsapp.db
     rm -f *.db-journal
     ```
   - Rebuild the application:
     ```bash
     go clean
     go build -o waani
     ```
   - Start server with debug logging:
     ```bash
     DEBUG=1 ./scripts/start_server.sh
     ```

## API Endpoints

### Device Management
- `GET /qr` - Get QR code for device pairing
- `GET /devices` - List all connected devices
- `DELETE /devices?deviceId=xxx` - Logout and remove device

### Messaging
- `POST /messages/text` - Send text message
  ```json
  {
    "deviceId": "device_id",
    "to": "recipient_id",
    "message": "Hello, World!"
  }
  ```
- `GET /messages` - Get messages
  ```
  Query params:
  - deviceId: Device ID
  - chatId: Chat ID
  - limit: Number of messages (default: 50)
  ```

### Chats
- `GET /chats` - List all chats
- `GET /chats/{chatId}` - Get specific chat details

### Groups
- `GET /groups` - List all groups
- `POST /groups/create` - Create new group
- `GET /groups/{groupId}` - Get group info
- `DELETE /groups/{groupId}` - Leave group

## Development

### Running Tests
```bash
./scripts/run_tests.sh
```
Test reports are generated in the `tests` directory.

### Project Structure
```
waani_v5/
├── main.go           # Main application entry
├── config/          # Configuration management
├── db/              # Database interfaces
├── scripts/         # Build and test scripts
├── static/          # Web interface files
└── tests/           # Test suite
```

### Error Handling
The API uses standard HTTP status codes:
- 200: Success
- 400: Bad Request
- 401: Unauthorized
- 404: Not Found
- 500: Internal Server Error

### Rate Limiting
- Group operations: 1 request per second
- Message sending: 10 messages per minute per device

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

MIT License - See LICENSE file for details

## Support

For issues and feature requests, please use the GitHub issue tracker. 