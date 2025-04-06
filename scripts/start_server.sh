#!/bin/bash

# Build the server
echo "Building server..."
go build -o waani

# Start the server in the background
echo "Starting server..."
./waani &
SERVER_PID=$!

# Run the tests
echo "Running API tests..."
./scripts/run_tests.sh

# Function to handle cleanup
cleanup() {
    echo "Stopping server..."
    kill $SERVER_PID
    exit
}

# Set up trap for cleanup
trap cleanup SIGINT SIGTERM

# Keep the script running
wait $SERVER_PID 