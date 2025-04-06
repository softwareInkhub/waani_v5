# WhatsApp API Interface (WAANI v2)

A modern web interface for managing multiple WhatsApp accounts using the whatsmeow library.

## Features

- Multi-device WhatsApp management
- Real-time device status updates via WebSocket
- QR code-based device authentication
- Send messages to individual contacts
- Fetch and manage WhatsApp groups
- MongoDB integration for persistent storage
- Modern web interface with real-time updates

## Tech Stack

- Backend: Go (Gin framework)
- Frontend: HTML, JavaScript, Tailwind CSS
- Database: MongoDB
- WebSocket for real-time communication
- WhatsApp Integration: whatsmeow library

## Setup

1. Install Go (1.16 or later)
2. Install MongoDB
3. Clone the repository
4. Install dependencies:
   ```bash
   go mod download
   ```
5. Start MongoDB service
6. Run the application:
   ```bash
   go run main.go
   ```
7. Access the web interface at `http://localhost:8080`

## Environment Setup

Make sure you have MongoDB running locally on the default port (27017).

## Usage

1. Open the web interface
2. Click "Add Device" to scan QR code with WhatsApp
3. Manage multiple devices
4. Send messages and manage groups
5. Monitor device status in real-time

## License

MIT License 