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
	"math/rand"
	"strconv"
)

type NodeInfo struct {
	ID   int
	Addr string
}

type Registry struct {
	Nodes   map[int]NodeInfo
	AddrMap map[string]bool
	Mutex   sync.RWMutex
}

// Starts the registry server by creating a new instance of a Registry
func NewRegistry() *Registry {
	return &Registry{
		Nodes:   make(map[int]NodeInfo),
		AddrMap: make(map[string]bool),
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
		remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
		r.handleRegister(conn, message[len("REGISTER "):], remoteAddr.IP.String())
	case strings.HasPrefix(message, "DEREGISTER"):
		r.handleDeregister(conn, message[len("DEREGISTER "):])
	}
}

// Manage the registration of a new messaging node with the registry
func (r *Registry) handleRegister(conn net.Conn, message string, ip string) {
	fields := strings.Fields(message)
	if len(fields) != 2 {
		errMsg := "ERROR: Invalid registration message format"
		fmt.Println(errMsg)
		conn.Write([]byte(errMsg)) // Send error response back to the node
		return
	}

	addr := fields[0]
	port := fields[1]

	// Validate that the IP address matches the connection source 
	if !strings.HasPrefix(ip, addr) {
		errMsg := "ERROR: IP address does not match connection source"
		fmt.Println(errMsg)
		conn.Write([]byte(errMsg)) // Send error response back to the node
		return
	}

	// Construct the full address
	fullAddr := addr + ":" + port
	
	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	// TODO: Validate the extracted address, ensuring it is unique with no two nodes having the same IP address and port combination
	// Check if the address is already registered
	if _, exists := r.AddrMap[fullAddr]; exists {
		conn.Write([]byte("-3 ERROR: Node already registered"))
		return
	}

	// Create a unique new node ID randomly between 0 and 127
	var nodeID int
	for {
		nodeID = rand.Intn(128) // Random node ID between 0 and 127
		if _, exists := r.Nodes[nodeID]; !exists {
			break
		}
	}

	// Add the node to the registry
	r.Nodes[nodeID] = NodeInfo{ID: nodeID, Addr: fullAddr}
	r.AddrMap[fullAddr] = true

	// Send the node ID back to the node
	response := fmt.Sprintf("0 Registration request successful. The number of messaging nodes currently constituting the overlay is (%d)", len(r.Nodes))
	conn.Write([]byte(response))
}

// Manage the deregistration of a messaging node with the registry
func (r *Registry) handleDeregister(conn net.Conn, message string) {

	// Split the message to extract the node ID and address
	fields := strings.Fields(message)
	if len(fields) != 2 {
		conn.Write([]byte("-1 ERROR: Invalid deregistration request format"))
		return
	}

	nodeID, err := strconv.Atoi(fields[0]) // Convert the ID from string to int
	if err != nil || nodeID < 0 {
		conn.Write([]byte("-2 ERROR: Invalid node ID"))
		return
	}
	addr := fields[1]
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)

	// Validate that the IP address matches the connection source
	if !strings.HasPrefix(addr, remoteAddr.IP.String()) {
		conn.Write([]byte("-3 ERROR: IP address mismatch"))
		return
	}

	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	// Check if the node was previously registered
	NodeInfo, ok := r.Nodes[nodeID]
	if !ok {
		conn.Write([]byte("-4 ERROR: Node not registered"))
		return
	}

	// Check if the address matches the registered address
	if NodeInfo.Addr != addr {
		conn.Write([]byte("-5 ERROR: Address mismatch"))
		return
	}

	// Remove the node from the registry
	delete(r.Nodes, nodeID)
	delete(r.AddrMap, addr)

	// Construct and send the deregistration response
	response := fmt.Sprintf("0 Deregistration request successful. The number of messaging nodes currently constituting the overlay is (%d)", len(r.Nodes))
	conn.Write([]byte(response))
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run registry.go registry-port")
		return
	}

	registry := NewRegistry() // Create a single instance of the registry
	registry.Start(os.Args[1])
}
