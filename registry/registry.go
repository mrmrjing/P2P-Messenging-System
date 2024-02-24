// Implementation of the registry server
// Contains the code for starting up the registry server, handling registration and deregistration requests from messaging nodes,
// constructing the overlay network and managing the routing table

/*
CHECKLIST:
- allows messaging nodes to register and deregister
- prevents duplicate IDs and mismatched addresses
- sends responses to registration and deregistration requests
*/

package main

import (
	"encoding/binary"
	"fmt"
	"group8/minichord"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

// NodeInfo contains the information about a node in the network.
type NodeInfo struct {
	ID   int
	Addr string
}

const I64SIZE = 8 // Size of int64 in bytes

type Registry struct {
	Nodes   map[int]NodeInfo
	AddrMap map[string]bool
	Mutex   sync.RWMutex
}

// Starts the registry server by creating a new instance of a Registry, maps to keep track of the registered nodes and their addresses
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

	// Receive and unmarshal the Protobuf message using the new function
	msg, err := ReceiveMiniChordMessage(conn)
	if err != nil {
		log.Printf("Error receiving MiniChord message: %v\n", err)
		return // Exit if there was an error receiving the message
	}

	// Process the message based on its type
	switch msg := msg.Message.(type) {
	case *minichord.MiniChord_Registration:
		r.handleRegister(conn, msg.Registration)
	case *minichord.MiniChord_Deregistration:
		r.handleDeregister(conn, msg.Deregistration)
	// Add more cases as necessary for other message types
	default:
		log.Printf("Unknown message type received\n")
	}
}

func ReceiveMiniChordMessage(conn net.Conn) (message *minichord.MiniChord, err error) {
	// First, get the number of bytes to received
	bs := make([]byte, I64SIZE)
	length, err := conn.Read(bs)
	if err != nil {
		if err != io.EOF {
			log.Printf("ReceivedMiniChordMessage() read error: %s\n", err)
		}
		return
	}
	if length != I64SIZE {
		log.Printf("ReceivedMiniChordMessage() length error: %d\n", length)
		return
	}
	numBytes := uint64(binary.BigEndian.Uint64(bs))

	// Get the marshaled message from the connection
	data := make([]byte, numBytes)
	length, err = conn.Read(data)
	if err != nil {
		if err != io.EOF {
			log.Printf("ReceivedMiniChordMessage() read error: %s\n", err)
		}
		return
	}
	if length != int(numBytes) {
		log.Printf("ReceivedMiniChordMessage() length error: %d\n", length)
		return
	}

	// Unmarshal the message
	message = &minichord.MiniChord{}
	err = proto.Unmarshal(data[:length], message)
	if err != nil {
		log.Printf("ReceivedMiniChordMessage() unmarshal error: %s\n",
			err)
		return
	}
	log.Printf("ReceiveMiniChordMessage(): received %s (%v), %d from %s\n",
		message, data[:length], length, conn.RemoteAddr().String())
	return
}

// Manage the registration of a new messaging node with the registry
func (r *Registry) handleRegister(conn net.Conn, registration *minichord.Registration) {
	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	fullAddr := registration.Address // Directly use the address from the Protobuf message

	// Check if the address is already registered
	if _, exists := r.AddrMap[fullAddr]; exists {
		sendProtoResponse(conn, -3, "Node already registered", "registration")
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

	// Before adding the node to the registry, check if the connection's remote address matches the provided address
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	providedIP := strings.Split(fullAddr, ":")[0]

	// Handle localhost cases
	if providedIP == "localhost" {
		providedIP = "127.0.0.1"
	}
	if remoteAddr.IP.String() == "::1" {
		remoteAddr.IP = net.ParseIP("127.0.0.1")
	}

	// Allow loopback if the provided IP belongs to the local machine
	if (remoteAddr.IP.String() == "::1" || remoteAddr.IP.String() == "127.0.0.1") && isLocalMachineIP(providedIP) {
		log.Printf("Allowing registration from loopback since provided IP belongs to the local machine")
	} else if remoteAddr.IP.String() != providedIP {
		sendProtoResponse(conn, -4, "IP address mismatch between request and connection", "registration")
		return
	}

	// Add the node to the registry
	r.Nodes[nodeID] = NodeInfo{ID: nodeID, Addr: fullAddr}
	r.AddrMap[fullAddr] = true

	// Convert nodeID to int32 and send the node ID back to the node
	sendProtoResponse(conn, int32(nodeID), "Registration successful", "registration")
}

// Checks if the provided IP is one of the local machine's IP addresses
func isLocalMachineIP(ipAddr string) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("Error getting local IP addresses: %v", err)
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip.String() == ipAddr {
			return true
		}
	}
	return false
}

// Manage the deregistration of a messaging node with the registry
func (r *Registry) handleDeregister(conn net.Conn, deregistration *minichord.Deregistration) {
	r.Mutex.Lock()
	defer r.Mutex.Unlock()

	// Convert the node ID from the request to the proper integer format.
	nodeID := int(deregistration.Id)
	log.Printf("Deregistration attempt for Node ID: %d", nodeID)

	// Retrieve the registered node information using the node ID.
	nodeInfo, ok := r.Nodes[nodeID]

	// Validate node ID and address.
	// Check if the node is actually registered and if the address matches.
	if !ok {
		log.Printf("Node ID %d not found for deregistration", nodeID)
		sendProtoResponse(conn, int32(nodeID), "Node not registered", "deregistration")
		return
	}

	// Check if the address from the deregistration request matches the registered address.
	if nodeInfo.Addr != deregistration.Address {
		log.Printf("Address mismatch for Node ID %d: registered address %s, deregistration address %s", nodeID, nodeInfo.Addr, deregistration.Address)
		sendProtoResponse(conn, int32(nodeID), "Invalid node ID or address mismatch", "deregistration")
		return
	}

	// Remove the node from registry records if validation passes.
	delete(r.Nodes, nodeID)
	delete(r.AddrMap, deregistration.Address) // Assuming AddrMap uses the same format as deregistration.Address.

	// log.Printf("Node ID %d successfully deregistered", nodeID)
	// Send a successful deregistration response.
	sendProtoResponse(conn, int32(nodeID), "Deregistration successful", "deregistration")
}

// Function to send a response back to the client based on the type of message received
func sendProtoResponse(conn net.Conn, result int32, info string, responseType string) {
	var response *minichord.MiniChord

	// Construct response based on the type
	switch responseType {
	case "registration":
		// Construct a registration response
		response = &minichord.MiniChord{
			Message: &minichord.MiniChord_RegistrationResponse{
				RegistrationResponse: &minichord.RegistrationResponse{
					Result: result,
					Info:   info,
				},
			},
		}
	case "deregistration":
		// Construct a deregistration response
		response = &minichord.MiniChord{
			Message: &minichord.MiniChord_DeregistrationResponse{
				DeregistrationResponse: &minichord.DeregistrationResponse{
					Result: result,
					Info:   info,
				},
			},
		}

	default:
		// Log an error or handle unexpected response types as necessary
		log.Printf("sendProtoResponse() called with unknown response type: %s", responseType)
		return // Exit the function as we don't want to proceed with an unknown response type
	}

	// Marshal the response into a protobuf message
	data, err := proto.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal %s response: %s", responseType, err)
		return
	}

	// Send the size of the marshaled message first
	size := uint64(len(data)) // Store the length of the data to send for logging
	if err := binary.Write(conn, binary.BigEndian, size); err != nil {
		log.Printf("Error sending size of %s response (%d bytes): %s", responseType, size, err)
		return
	}

	// Send the marshaled message
	sentBytes, err := conn.Write(data)
	if err != nil {
		log.Printf("Error sending %s response message: %s", responseType, err)
		return
	}
	log.Printf("Sent %s response message: %d bytes sent.", responseType, sentBytes)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run registry.go registry-port")
		return
	}

	registry := NewRegistry() // Create a single instance of the registry
	registry.Start(os.Args[1])
}
