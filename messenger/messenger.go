// Implementatin of a basic TCP server that listens for incoming connections on automatically assigned port
// and prints the received messages and the remote address of the client.

// This file contains the code for the messaging node, which is responsible for starting up a messaging node and handling incoming
// connections, communicating with the registry and fowarding messages based on the routing table

/*
CHECKLIST:
- automatic port configuartion
- registration request

*/

package main

import (
	"encoding/binary"
	"fmt"
	"group8/minichord"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"google.golang.org/protobuf/proto"
)

var myNodeID int // global variable to store the node ID

const I64SIZE = 8 // Size of int64 in bytes

type RoutingTable struct {
	Entries map[int]string
}

// Define global variable for the routing table
var routingTable = RoutingTable{Entries: make(map[int]string)}

// Function to forward the message to the next node in the routing table
func forwardMessage(message string, destID int) {
	// Get the destination in the routing table
	addr, exists := routingTable.Entries[destID]
	if !exists {
		fmt.Println("Destination not found in the routing table")
		return
	}

	// Establish a connection to the next node in the routing table
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	defer conn.Close() // Close the connection when the function returns

	// Send the message to the next node in the routing table
	_, err = conn.Write([]byte(message))
	if err != nil {
		fmt.Println("Error: ", err)
	}
}

func StartNode(registryAddress string) {
	localIP := getLocalIP() // Automatically get the local IP address
	// Create a TCP listener as each messaging node needs to listen for incoming connections from other nodes or the registry server
	listener, err := net.Listen("tcp", localIP+":0") // Automatically assign a port + use the local IP for listening
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}
	defer listener.Close() // Close the listener when the main function returns

	// Print the port number that the server is listening on
	fmt.Println("Node listening on", listener.Addr().String())

	// Register the node with the registry server using its IP address and the port number assigned by the listener
	localAddr := listener.Addr().String() 

	// Register the node with the registry server using its IP address and the port number assigned by the listener
	registerWithRegistry(registryAddress, localAddr)

	// Defer deregistering the node from the registry server until the main function returns
	defer deregisterFromRegistry(registryAddress, myNodeID, localAddr)

	// Accept incoming connections in a loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error: ", err)
			continue
		}

		// Handle the connection in a new goroutine
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close() // Close the connection when the function returns

	// Read the incoming message
	buffer := make([]byte, 1024)
	messageLength, err := conn.Read(buffer)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	// Assuming the message is formatted as "destID: message"
	message := strings.SplitN(string(buffer[:messageLength]), ":", 2)
	if len(message) != 2 {
		fmt.Println("Invalid message format")
		return
	}

	destID, err := strconv.Atoi(message[0])
	if err != nil {
		fmt.Println("Invalid destination ID")
		return
	}

	// Forward the message if the destination is not the current node
	if destID != myNodeID { //// Assuming `myNodeID` holds the current node's ID, which should be set after registration
		forwardMessage(message[1], destID)
		return
	}

	// Print the received message
	fmt.Println("Received message: ", message[1])

	// Print the remote address
	fmt.Println("Remote address: ", conn.RemoteAddr().String())
}

// Function to connect to the registry server and send a registration message
func registerWithRegistry(registryAddress string, serverAddress string) {
	// Attempt to establish a TCP connection to the registry using the provided address
	conn, err := net.Dial("tcp", registryAddress)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	defer conn.Close() // Close the connection when the function returns

	// Create a registration message using Protobuf
	regMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_Registration{
			Registration: &minichord.Registration{
				Address: serverAddress,
			},
		},
	}

	// Send the registration message to the registry server
	if err := sendMiniChordMessage(conn, regMsg); err != nil {
		log.Println("Error sending registration message:", err)
		return
	}

	// Read and process the reponse from the registry server
	response, err := ReceiveMiniChordMessage(conn)
	if err != nil {
		fmt.Println("Error receiving response from registry: ", err)
		return
	}
	if regResponse, ok := response.Message.(*minichord.MiniChord_RegistrationResponse); ok {
		myNodeID = int(regResponse.RegistrationResponse.Result) 
		fmt.Println("Registered with Node ID:", myNodeID)
		log.Printf("Debug: Received registration response with result %d and info '%s'", regResponse.RegistrationResponse.Result, regResponse.RegistrationResponse.Info)
	} else {
		fmt.Println("Unexpected response type from registry")
	}
}

// Function to connect to the registry server and send a deregistration message
func deregisterFromRegistry(registryAddress string, nodeID int, serverAddress string) {
	// Attempt to establish a TCP connection to the registry using the provided address
	conn, err := net.Dial("tcp", registryAddress)
	if err != nil {
		fmt.Println("Error connecting to registry for deregistration:", err)
		return
	}
	defer conn.Close() // Close the connection when the function returns

	// Create a deregistration message using Protobuf
	deregMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_Deregistration{
			Deregistration: &minichord.Deregistration{
				Id:      int32(nodeID),
				Address: serverAddress,
			},
		},
	}

	// Send the deregistration message to the registry server
	if err := sendMiniChordMessage(conn, deregMsg); err != nil {
		log.Println("Error sending deregistration message:", err)
		return
	}

	// It's not necessary to wait for a response from the registry, but you can implement this if needed.
	fmt.Println("Deregistered from registry with Node ID:", nodeID)
}

// getLocalIP attempts to determine the local network IP by creating a UDP connection
// to a well-known address (here we use Google's public DNS). It does not actually establish
// the connection but uses the selected interface to determine the IP.
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

// Define the function to send a MiniChord message
func sendMiniChordMessage(conn net.Conn, message *minichord.MiniChord) (err error) {
	data, err := proto.Marshal(message)
	log.Printf("SendMiniChordMessage(): sending %s (%v), %d to %s\n",
		message, data, len(data), conn.RemoteAddr().String())
	if err != nil {
		log.Println("Failed to marshal message:", err)
	}

	// First send the number of bytes in the marshaled message
	bs := make([]byte, I64SIZE)
	binary.BigEndian.PutUint64(bs, uint64(len(data)))
	length, err := conn.Write(bs)
	if err != nil {
		log.Printf("SendMiniChordMessage() error: %s\n", err)
	}

	if length != I64SIZE {
		log.Panicln("Short write?")
	}

	// Send the marshales message
	length, err = conn.Write(data)
	if err != nil {
		log.Printf("SendMiniChordMessage() error: %s\n", err)
	}

	if length != len(data) {
		log.Panicln("Short write?")
	}

	return
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

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run messenger.go registry-host:registry-port")
		return
	}
	registryHostPort := os.Args[1]
	if !strings.Contains(registryHostPort, ":") {
		fmt.Println("Invalid registry address. Must be in the format host:port.")
		return
	}

	StartNode(registryHostPort)
}
