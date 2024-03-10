#!/bin/bash 

# Registry information
REGISTRY_HOST="localhost"
REGISTRY_HOST_PORT="12345"

# Number of nodes to start 
NODES=10

# Directory where messenger.go is located
MESSENGER_DIR="/Users/hongjing/Documents/RU /Concurrent and Distributed Programming /Programming Assignment 2/Group 8/messenger"

# Start the messenger nodes
for ((i=1; i <= NODES; i++))
do 
# For macOS
     osascript -e "tell app \"Terminal\" to do script \"cd '${MESSENGER_DIR}'; go run messenger.go ${REGISTRY_HOST}:${REGISTRY_HOST_PORT}\"" 
    
    sleep 1
done
