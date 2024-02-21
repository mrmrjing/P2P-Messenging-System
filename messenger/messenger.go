// Implementatin of a basic TCP server that listens for incoming connections on automatically assigned port
// and prints the received messages and the remote address of the client.

// This file contains the code for the messaging node, which is responsible for starting up a messaging node and handling incoming
// connections, communicating with the registry and fowarding messages based on the routing table

package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

func StartNode(registryAddress string) {
	// Create a TCP listener as each messaging node needs to listen for incoming connections from other nodes or the registry server
	listener, err := net.Listen("tcp", ":0") // Automatically assign a port
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}
	defer listener.Close() // Close the listener when the main function returns

	// Print the port number that the server is listening on
	fmt.Println("Node listening on", listener.Addr().String())

	// Register the node with the registry server using its IP address and the port number assigned by the listener
	localAddr := listener.Addr().String() // This is the address that should be sent to the registry

	// Register the node with the registry server using its IP address and the port number assigned by the listener
	registerWithRegistry(registryAddress, localAddr)

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

	// Print the received message
	fmt.Println("Received message: ", string(buffer[:messageLength]))

	// Print the remote address
	fmt.Println("Remote address: ", conn.RemoteAddr().String())
}

// Function to connect to the registry server and send a registration message
func registerWithRegistry(registryAddress string, serverAddress string) {
	// Connect to the registry server
	conn, err := net.Dial("tcp", registryAddress)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	defer conn.Close() // Close the connection when the function returns

	// Send the registration message to the registry server
	message := "REGISTER " + serverAddress
	_, err = conn.Write([]byte(message))
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	// Read and print the response from the registry server
	responseBuffer := make([]byte, 1024)
	_, err = conn.Read(responseBuffer)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	fmt.Println("Registry response: ", string(responseBuffer))
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
