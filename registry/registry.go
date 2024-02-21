// Implementation of the registry server
// Contains the code for starting up the registry server, handling registration and deregistration requests from messaging nodes,
// constructing the overlay network and managing the routing table

package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

type NodeInfo struct {
	ID   int
	Addr string
}

type Registry struct {
	Nodes  map[int]NodeInfo
	Mutex  sync.RWMutex
	NextID int
}

// Starts the registry server by creating a new instance of a Registry
func NewRegistry() *Registry {
	return &Registry{
		Nodes:  make(map[int]NodeInfo),
		NextID: 0,
	}
}

// Sets up the TCP server for the registry and listens for incoming connections
func (r *Registry) Start(port string) {
	listener, err := net.Listen("tcp", ":"+port) // Listen on the specified port
	if err != nil {
		fmt.Printf("Failed to start server: %s\n", err)
		return
	}
	defer listener.Close()

	fmt.Println("Registry listening on", listener.Addr().String())

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Failed to accept connection: %s\n", err)
			continue
		}
		go r.handleConnection(conn)
	}
}

// Interprets the incoming message and calls the appropriate handler based on the message type
func (r *Registry) handleConnection(conn net.Conn) {
	defer conn.Close()

	var buffer [1024]byte
	n, err := conn.Read(buffer[:])
	if err != nil {
		fmt.Println("Failed to read from connection:", err)
		return
	}
	message := strings.TrimSpace(string(buffer[:n]))

	// Process message
	switch {
	case strings.HasPrefix(message, "REGISTER"):
		r.handleRegister(conn, message[len("REGISTER "):])
	case strings.HasPrefix(message, "DEREGISTER"):
		r.handleDeregister(conn, message[len("DEREGISTER "):])
		// You can add more cases here for other types of messages
	}
}

// Manage the registration of a new messaging node with the registry
func (r *Registry) handleRegister(conn net.Conn, addr string) {

	// Validate the extracted address, ensuring it is not empty 
	if addr == "" {
		errMsg := "ERROR: Received empty address for registration" 
		fmt.Println(errMsg)
		conn.Write([]byte(errMsg)) // Send error response back to the node
		return 
	}
	
	r.Mutex.Lock()
	nodeID := r.NextID
	r.Nodes[nodeID] = NodeInfo{ID: nodeID, Addr: addr}
	r.NextID++
	r.Mutex.Unlock()

	response := fmt.Sprintf("REGISTERED %d", nodeID)
	_, err := conn.Write([]byte(response))
    if err != nil {
        fmt.Printf("Failed to send registration response: %s\n", err)
    } else {
        fmt.Printf("Node registered: ID %d, Addr %s\n", nodeID, addr)
    }
}

// Manage the deregistration of a messaging node with the registry
func (r *Registry) handleDeregister(conn net.Conn, message string) {

	// Extract and validate node ID from the message, e
	var nodeID int
	_, err := fmt.Sscanf(message, "%d", &nodeID) // Extract the node ID from the message
	if err != nil {
		errMsg := "ERROR: Failed to parse node ID from message"
		fmt.Println(errMsg)
		conn.Write([]byte(errMsg)) // Send error response back to the node
		return
	}

	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	// Check if the node is actually registered
	if _, ok := r.Nodes[nodeID]; ok {
		delete(r.Nodes, nodeID)
		response := "DEREGISTERED"
		_, err := conn.Write([]byte(response))
		if err != nil {
			fmt.Printf("Failed to send deregistration response: %s\n", err)
		} else {
			fmt.Printf("Node deregistered: ID %d\n", nodeID)
		}
	} else {
		errMsg := fmt.Sprintf("ERROR: Node %d not found", nodeID)
		fmt.Println(errMsg)
		conn.Write([]byte(errMsg)) // Send error response back to the node
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run registry.go registry-port")
		return
	}
	registry := NewRegistry()
	registry.Start(os.Args[1])
}
