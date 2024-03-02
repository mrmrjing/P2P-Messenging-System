package main

import (
	"bufio"
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
	"time"

	"google.golang.org/protobuf/proto"
)

// Define global variables
var (
	sendTracker      int                                           = 0 // Counter for the number of data packets sent by the node
	receiveTracker   int                                           = 0 // Counter for the number of data packets received by this node
	relayTracker     int                                           = 0 // Counter for the number of data packets relayed by this node
	sendSummation    int64                                         = 0 // Counter for the summation of the values of the data packets sent by this node
	receiveSummation int64                                         = 0 // Counter for the summation of the values of the data packets received by this node
	myNodeID         int                                               // Global variable to store the node ID assigned by the registry
	routingTable     = RoutingTable{Entries: make(map[int]string)}     // Routing table for forwarding messages
	mutex            sync.Mutex                                        // Mutex to ensure thread-safe access to global variables
	registryHostPort string                                            // Address of the registry server
	localAddr        string                                            // Local address of this node
)

const I64SIZE = 8 // Constant representing the size of an int64 in bytes, used for encoding/decoding

// RoutingTable holds the routing information for forwarding messages
type RoutingTable struct {
	Entries map[int]string // Map of node IDs to their addresses
}

// StartNode sets up the node, registers it with the registry, and starts listening for incoming messages
func StartNode(registryAddress string) {
	log.Printf("[StartNode] Start: registryHostPort: %s", registryAddress) // ADDITION

	localIP := getLocalIP()                          // Automatically determine the local IP address.
	listener, err := net.Listen("tcp", localIP+":0") // Start a TCP listener on an automatically assigned port to accept incoming TCP connections
	if err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1) // Exit the program if unable to start listening
	}
	defer listener.Close() // Ensure the listener is closed when the function exits

	localAddr = listener.Addr().String()          // Store the local address and port where the node is listening and store as a string
	log.Printf("Node listening on %s", localAddr) // Print the address and port

	registerWithRegistry(registryAddress, localAddr)                   // Register the node with the registry server
	defer deregisterFromRegistry(registryAddress, myNodeID, localAddr) // Ensure the node is deregistered on exit

	log.Printf("[StartNode] After registering with registry: registryHostPort: %s", registryAddress)
	for {
		conn, err := listener.Accept() // Accept new incoming connections.
		if err != nil {
			log.Printf("[StartNode] Error accepting connection: %s", err)
			continue // Continue accepting other connections if there's an error.
		}
		log.Printf("[StartNode] Accepted new connection from %s", conn.RemoteAddr())
		// Call handleConnection in a new goroutine.
		go handleConnection(conn) // Handle each connection in a new goroutine
	}
}

// This function determines the local IP address by creating a dummy UDP connection
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80") // This does not actually connect but selects an appropriate interface
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close() // Ensure the dummy connection is closed.

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String() // Return the local IP address determined by the dummy connection.
}

// This function sends a registration message to the registry server
func registerWithRegistry(registryAddress string, serverAddress string) {
	conn, err := net.Dial("tcp", registryAddress) // Establish a TCP connection to the registry
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	regMsg := &minichord.MiniChord{ // Create the registration message
		Message: &minichord.MiniChord_Registration{
			Registration: &minichord.Registration{
				Address: serverAddress, // Address of the messaging node
			},
		},
	}

	if err := SendMiniChordMessage(conn, regMsg); err != nil { // Send the registration message to the registry, log if there is an error
		log.Println("Error sending registration message:", err)
		return
	}
	response, err := ReceiveMiniChordMessage(conn) // Wait for and process the registration response from the registry
	if err != nil {
		fmt.Println("Error receiving response from registry: ", err)
		return
	}

	// After successful registration, wait for and process the registration response from the registry
	if regResponse, ok := response.Message.(*minichord.MiniChord_RegistrationResponse); ok {
		mutex.Lock()
		myNodeID = int(regResponse.RegistrationResponse.Result) // Update the node ID based on the response and convert it to an integer
		mutex.Unlock()
		log.Printf("[registerWithRegistry] Registered with Node ID: %d", myNodeID)
	} else {
		log.Println("[registerWithRegistry] Unexpected response type from registry")
	}
}

// This function sends a deregistration message to the registry server and handles deregistration of a node
func deregisterFromRegistry(registryAddress string, nodeID int, serverAddress string) {
	// Debugging statement to check the value before attempting to deregister
	log.Printf("Attempting to deregister. Registry address: '%s', Node ID: %d, Server Address: '%s'", registryAddress, nodeID, serverAddress)

	if registryAddress == "" {
		log.Println("Deregistration failed: Registry address is empty")
		return
	}
	if !strings.Contains(registryAddress, ":") {
		log.Printf("Deregistration failed: Invalid registry address '%s'", registryAddress)
		return
	}
	conn, err := net.Dial("tcp", registryAddress) // Establish a connection to the registry for deregistration
	if err != nil {
		fmt.Println("Error connecting to registry for deregistration:", err)
		return
	}
	defer conn.Close() // Ensure the connection is closed on function exit

	deregMsg := &minichord.MiniChord{ // Create the deregistration message.
		Message: &minichord.MiniChord_Deregistration{
			Deregistration: &minichord.Deregistration{
				Id:      int32(nodeID), // Node ID to be deregistered
				Address: serverAddress, // Address of the messaging node
			},
		},
	}

	if err := SendMiniChordMessage(conn, deregMsg); err != nil { // Send the deregistration message to the registry
		log.Println("Error sending deregistration message:", err)
		return
	}

	// Wait for a response from the registry
	response, err := ReceiveMiniChordMessage(conn)
	if err != nil {
		log.Println("Error receiving response from registry:", err)
		return
	}

	// If the response is a DeregistrationResponse
	switch resp := response.Message.(type) {
	case *minichord.MiniChord_DeregistrationResponse:
		// Handle the response based on the result and info in the message
		switch resp.DeregistrationResponse.Result {
		case 0: // Success
			fmt.Println("Successfully deregistered from registry with Node ID:", nodeID)
			os.Exit(0) // Exit the program after successful deregistration
		case -1: // Node not registered
			fmt.Println("Deregistration failed: Node not registered.")
		case -2: // Invalid node ID or address mismatch
			fmt.Println("Deregistration failed: Invalid node ID or address mismatch.")
		default:
			fmt.Printf("Deregistration failed with unknown error code: %d\n", resp.DeregistrationResponse.Result)
		}
	default:
		log.Println("Received unknown response type from registry")
	}
}

// This function encodes and sends a MiniChord message to the specified connection
func SendMiniChordMessage(conn net.Conn, message *minichord.MiniChord) (err error) {
	data, err := proto.Marshal(message) // Marshal the message into binary format
	if err != nil {
		log.Panicln("Failed to marshal message:", err)
	}
	// First send the number of bytes in the marshaled message
	bs := make([]byte, I64SIZE)                       // Create a buffer for the length prefix
	binary.BigEndian.PutUint64(bs, uint64(len(data))) // Encode the length of the data
	length, err := conn.Write(bs)                     // Send the length prefix
	if err != nil {
		log.Printf("SendMiniChordMessage() error sending length: %s\n", err)
	}
	if length != I64SIZE {
		log.Panicln("Short write?")
	}
	// Send the marshales message
	length, err = conn.Write(data)
	if err != nil {
		log.Printf("SendMiniChordMessage() error sending data: %s\n", err)
	}
	if length != len(data) {
		log.Panicln("Short write?")
	}
	return // Successfully sent the message
}

// This function reads and decodes a MiniChord message from the specified connection
func ReceiveMiniChordMessage(conn net.Conn) (message *minichord.MiniChord, err error) {
	// First, get the number of bytes to received
	bs := make([]byte, I64SIZE)  // Create a buffer to hold the length of the incoming message
	length, err := conn.Read(bs) // Read the length prefix from the connection
	if err != nil {
		if err != io.EOF {
			log.Printf("ReceivedMiniChordMessage() read error: %s\n", err)
		}
		return
	}
	if length != I64SIZE {
		log.Printf("ReceivedMiniChordMessage() length error: expected %d bytes, got %d\n", I64SIZE, length)
		return // Return early if the length of the received data does not match the expected size
	}
	numBytes := int(binary.BigEndian.Uint64(bs)) // Decode the length prefix

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
		log.Printf("ReceivedMiniChordMessage() length error: expected %d bytes, got %d\n", numBytes, length)
		return // Return early if the length of the received data does not match the expected size
	}

	// Unmarshal the binary data into a MiniChord message
	message = &minichord.MiniChord{}
	err = proto.Unmarshal(data[:length], message)
	if err != nil {
		log.Printf("ReceivedMiniChordMessage() unmarshal error: %s\n", err)
		return // Return early on unmarshal error
	}
	return // Return the successfully unmarshaled message
}

// This function processes messages received from a connection
func handleConnection(conn net.Conn) {
	log.Printf("[handleConnection] New connection from %s", conn.RemoteAddr())
	log.Printf("[handleConnection] Raw data received from %s", conn.RemoteAddr())

	receivedMsg, err := ReceiveMiniChordMessage(conn) // Read and decode the incoming message
	if err != nil {
		fmt.Println("Error receiving message:", err)
		return // Exit if there is an error receiving the message
	}
	log.Printf("[handleConnection] Decoded message from %s: %v", conn.RemoteAddr(), receivedMsg)

	// Handle the message based on its type
	switch msg := receivedMsg.Message.(type) {
	case *minichord.MiniChord_RegistrationResponse: // If the message is a registration response from the registry
		// Update the node's ID based with the new ID assigned by the registry
		fmt.Println("Received registration response: ", msg.RegistrationResponse.Info)
		myNodeID = int(msg.RegistrationResponse.Result)

	case *minichord.MiniChord_NodeRegistry: // If the message is a node registry message
		mutex.Lock()
		// Update the node's routing table based on the received NodeRegistry message
		log.Printf("[handleConnection] Received NodeRegistry message: %+v", msg.NodeRegistry)
		mutex.Unlock()
		handleNodeRegistry(msg.NodeRegistry)

	case *minichord.MiniChord_NodeData: // If the message is a NodeData message received from another node
		// Update the sum of received messages and increment the counter for received messages
		mutex.Lock()   // Acquire the mutex to safely update the global counters
		mutex.Unlock() // Release the mutex
		processNodeDataMessage(msg.NodeData)

	case *minichord.MiniChord_RequestTrafficSummary: // If the message is a request for the traffic summary
		log.Println("Received request for traffic summary")
		// Send the traffic summary to the registry
		sendTrafficSummary()

	case *minichord.MiniChord_InitiateTask:
		// Initiate the task of sending messages to other nodes
		go handleInitiateTask(int(msg.InitiateTask.Packets))

	default:
		log.Printf("[handleConnection] Received an unknown type of message: %T", msg)
	}
}

// This function calculates the distance from source to destination in the ID space
// ie. this function calculates the distance between two nodes
func calculateDistance(source, destination int) int {
	const IDSpaceSize = 128
	distance := (destination - source + IDSpaceSize) % IDSpaceSize
	return distance
}

// (2.4) This function processes a NodeRegistry message, and initiate connections to the nodes that comprise its routing table, and report back to the registry
func handleNodeRegistry(msg *minichord.NodeRegistry) { // Acquire the mutex to safely update the routing table
	log.Printf("[handleNodeRegistry] Processing NodeRegistry message: %+v", msg)

	newEntries := make(map[int]string) // Create a new map for the updated routing table entries.
	var successfulConnections []int    // List to keep track of successful connections
	var failedConnections []int        // List to keep track of failed connections

	for _, peer := range msg.Peers { // Iterate over the peers received in the message
		if int(peer.Id) != myNodeID { // Avoid adding the node itself to its routing table
			newEntries[int(peer.Id)] = peer.Address // Add the peer to the new routing table entries
			// Initiate connection to the peer
			if err := initiateConnection(peer.Address); err != nil {
				log.Printf("Failed to connect to node %d at %s\n", peer.Id, peer.Address)
				failedConnections = append(failedConnections, int(peer.Id))
			} else {
				log.Printf("Successfully connected to node %d at %s\n", peer.Id, peer.Address)
				successfulConnections = append(successfulConnections, int(peer.Id))
			}
		}
	}
	mutex.Lock()
	routingTable.Entries = newEntries // Update the routing table with the new entries
	mutex.Unlock()                    // Ensure the mutex is released.

	// Log the updated routing table
	log.Printf("Updated routing table: %+v", routingTable.Entries)

	// Now, report back to the registry the status of the connections
	reportConnectionsStatus(successfulConnections, failedConnections)
}

// This function tries to establish a TCP connection to the given address and returns error if unsuccessful
func initiateConnection(address string) error {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return err // return error if connection failed
	}
	defer conn.Close() // Close the connection immediately after establishing it in this case
	return nil         // return nil if connection was successful
}

// (2.4, 4) This function reports the status of the connections to the registry, and report if all nodes have successfully established connections to nodes in their routing table
func reportConnectionsStatus(_, failedConnections []int) {
	log.Printf("[reportConnectionsStatus] Entry: registryHostPort: %s", getRegistryHostPort())
	var info string
	var result uint32              // To indicate success or failure
	const failureCode = 4294967294 // Maximum value of uint32 to indicate failure, as nodeId ranges from 0 to 127
	if len(failedConnections) == 0 {
		info = "Successfully connected to all nodes in the routing table."
		result = uint32(myNodeID) // Use your node's ID as the result if successful
	} else {
		info = fmt.Sprintf("Failed to connect to nodes with IDs: %v", failedConnections)
		result = failureCode // Use the maximum uint32 value to indicate failure
	}
	// Construct the NodeRegistryResponse message
	response := &minichord.MiniChord{
		Message: &minichord.MiniChord_NodeRegistryResponse{
			NodeRegistryResponse: &minichord.NodeRegistryResponse{
				Result: result, // Use your node's ID as the result if successful, this field can't be negative as it is an uint32
				Info:   info,   // A string that describes the error or provides additional information
			},
		},
	}
	// Send the NodeRegistry response back to the registry
	conn, err := net.Dial("tcp", getRegistryHostPort())
	if err != nil {
		log.Printf("[reportConnectionsStatus] Error connecting: %s, registryHostPort: %s", err, registryHostPort)
		return
	}
	// defer conn.Close() // Make sure to close the connection after sending the message
	if err := SendMiniChordMessage(conn, response); err != nil {
		log.Println("Error sending NodeRegistryResponse message:", err)
		return
	}
	log.Printf("[reportConnectionsStatus] Exit: Successfully sent NodeRegistryResponse")
}

// This function forwards the given message to the next hop according to the routing table
func forwardMessage(nodeData *minichord.NodeData, nextHop string) error {

	// Check if this node is already in the trace to prevent loops
	currentNodeID := int32(myNodeID)
	for _, id := range nodeData.Trace {
		if id == currentNodeID {
			log.Printf("Loop detected in trace: %v\n, dropping the message to avoid duplicates", nodeData.Trace)
			return nil
		}
	}

	// Add the current node to the trace before forwarding
	nodeData.Trace = append(nodeData.Trace, currentNodeID)
	// Increment the hop count
	nodeData.Hops++

	// Establish a connection to the next hop
	conn, err := net.Dial("tcp", nextHop)
	if err != nil {
		log.Printf("Failed to connect to next hop %s: %s\n", nextHop, err)
		return err
	}
	defer conn.Close() // Ensure the connection is closed on function exit

	// Wrap the original NodeData message in a new MiniChord message container
	wrappedMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_NodeData{NodeData: nodeData},
	}
	// Use the SendMiniChordMessage function to send the wrapped message to the next hop
	if err := SendMiniChordMessage(conn, wrappedMsg); err != nil {
		log.Printf("Failed to send NodeData to next hop %s: %s\n", nextHop, err)
		return err
	} else {
		// Increment the relay counter only after successful send
		mutex.Lock()   // Acquire the lock to safely update the global counters
		relayTracker++ // Increment the counter for relayed data packets
		mutex.Unlock() // Release the lock
	}
	return nil
}

// This function process and forwards the given message to the next hop according to the routing table
func processNodeDataMessage(nodeData *minichord.NodeData) {
	log.Printf("Received NodeData message: %+v", nodeData)
	if int(nodeData.Destination) == myNodeID { // If the message is for this node
		// The message has reached its destination.
		fmt.Println("Message received at the destination node.")
		mutex.Lock()                                // Safely update message counters
		receiveTracker++                            // Increment the counter for received data
		receiveSummation += int64(nodeData.Payload) // Add to the sum of received data packets
		mutex.Unlock()
	} else {
		// Determine the directionality and check for overshooting the sink
		var nextHop string
		destinationDistance := calculateDistance(myNodeID, int(nodeData.Destination))
		closestNextHop := -1
		closestDistance := destinationDistance

		// Iterate through the routing table to find the closest next hop towards the destination
		for id, address := range routingTable.Entries {
			nextHopDistance := calculateDistance(id, int(nodeData.Destination))
			if nextHopDistance < closestDistance { // Check if the next hop is closer to the destination
				closestNextHop = id               // Update the closestNextHop with the ID of the closest next hop
				closestDistance = nextHopDistance // Update the closestDistance with the distance to the closest next hop
				nextHop = address                 // Update the nextHop with the address of the closest next hop
			}
		}

		// Check if we found a valid next hop that does not overshoot the destination
		if closestNextHop != -1 {
			fmt.Printf("Forwarding message to closest next hop: %s\n", nextHop)
			forwardMessage(nodeData, nextHop)
		} else {
			fmt.Println("No appropriate next hop found in routing table; dropping message.")
		}
	}
	log.Printf("Updated message counters: Sent: %d, Received: %d, Relayed: %d", sendTracker, receiveTracker, relayTracker)
}

// This function is called when an InitiateTask message is received
func handleInitiateTask(packets int) {
	log.Println("[handleInitiateTask] Entry: Starting task")
	for i := 0; i < packets; i++ {
		// Randomly select a destination node (excluding self)
		destNodeID := chooseRandomNode()
		if destNodeID == -1 {
			log.Println("No valid destination node found")
			continue
		}

		// Construct and send a data packet to the chosen node
		sendDataPacket(destNodeID)

		// Implement throttling to avoid overwhelming network
		time.Sleep(time.Millisecond * 10) // wait 10ms between messages
	}

	// After all messages have been sent, inform the registry of task completion by sending a TaskFinished message
	sendTaskFinishedMessage()
	log.Println("[handleInitiateTask] Exit: Task completed")
}

// This function randomly selects a node from the routing table, excluding the current node
func chooseRandomNode() int {
	mutex.Lock() // Ensure thread-safe access to the routing table
	defer mutex.Unlock()

	nodeIDs := make([]int, 0, len(routingTable.Entries))
	for id := range routingTable.Entries {
		if id != myNodeID { // Exclude the current node
			nodeIDs = append(nodeIDs, id)
		}
	}

	if len(nodeIDs) == 0 {
		return -1 // No valid nodes found
	}

	// Select a random node ID from the list
	randIndex := rand.Intn(len(nodeIDs))
	return nodeIDs[randIndex]
}

// This function sends a data packet to the specified destination node
func sendDataPacket(destNodeID int) {
	// Construct a NodeData message with a random payload
	nodeData := &minichord.NodeData{
		Destination: int32(destNodeID),
		Source:      int32(myNodeID),
		Payload:     int32(rand.Int63n(4294967296) - 2147483648), // Random integer between -2147483648 and 2147483647
		Hops:        0,
		Trace:       []int32{}, // Initially empty trace
	}

	// Find the next hop to the destination
	nextHop, exists := routingTable.Entries[destNodeID]
	if !exists {
		log.Printf("No next hop found for destination node %d\n", destNodeID)
		return
	}

	// Attempt to forward the message to the next hop
	if err := forwardMessage(nodeData, nextHop); err != nil {
		log.Printf("Failed to forward message to node %d: %s", destNodeID, err)
		return
	}

	// Update global counters only after successful send
	mutex.Lock()                             // Acquire the lock to update the global counters safely
	sendTracker++                            // Increment the counter for sent data packets
	sendSummation += int64(nodeData.Payload) // Add to the sum of sent data packets
	mutex.Unlock()                           // Release the lock
}

// This function sends a TaskFinished message to the registry once the node has finished its task
func sendTaskFinishedMessage() {
	log.Printf("[sendTaskFinishedMessage] Entry: registryHostPort: %s", getRegistryHostPort())
	conn, err := net.Dial("tcp", getRegistryHostPort()) // Establish a connection to the registry for sending
	if err != nil {
		log.Printf("[sendTaskFinishedMessage] Error connecting: %s, registryHostPort: %s", err, registryHostPort)
		return
	}
	defer conn.Close() // Ensure the connection is closed on function exit

	// Construct the TaskFinished message
	taskFinishedMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_TaskFinished{
			TaskFinished: &minichord.TaskFinished{
				Id:      int32(myNodeID), // Use the node's ID as the ID of the task finished message
				Address: localAddr,
			},
		},
	}
	// Send the TaskFinished message to the registry
	if err := SendMiniChordMessage(conn, taskFinishedMsg); err != nil {
		log.Println("Error sending TaskFinished message:", err)
	} else {
		log.Println("Sent TaskFinished message to registry")
	}
	log.Printf("[sendTaskFinishedMessage] Exit: Successfully sent TaskFinished message")
}

// This function sends the traffic summary to the registry
func sendTrafficSummary() {
	log.Printf("[sendTrafficSummary] Entry: registryHostPort: %s", getRegistryHostPort())
	// Establish a TCP connection to the registry
	conn, err := net.Dial("tcp", getRegistryHostPort())
	if err != nil {
		log.Println("Error establishing connection to registry:", err)
		return
	}
	// Construct the TrafficSummary message
	mutex.Lock()
	trafficSummaryMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_ReportTrafficSummary{
			ReportTrafficSummary: &minichord.TrafficSummary{
				Id:            int32(myNodeID),        // Use the node's ID as the ID of the traffic summary message
				Sent:          uint32(sendTracker),    // no. of packets that were sent by that node
				Received:      uint32(receiveTracker), // no. of packets that were received by that node
				TotalSent:     sendSummation,          // summation of the sent packets
				TotalReceived: receiveSummation,       // the summation of the received packets
				Relayed:       uint32(relayTracker),   // no. of packets that were relayed by that node
			},
		},
	}
	mutex.Unlock()

	// Send the TrafficSummary message to the registry
	if err := SendMiniChordMessage(conn, trafficSummaryMsg); err != nil {
		log.Println("Error sending TrafficSummary message:", err)
	} else {
		log.Println("Sent TrafficSummary message to registry")
	}

	// Reset the message counters after sending the traffic summary
	resetMessageCounters()
	log.Printf("[sendTrafficSummary] Exit: Successfully sent TrafficSummary message")
}

// This function will reset the message counters after sending the traffic summary
func resetMessageCounters() {
	mutex.Lock() // Acquire the lock to safely reset the message counters
	sendTracker = 0
	receiveTracker = 0
	sendSummation = 0
	receiveSummation = 0
	relayTracker = 0
	mutex.Unlock() // Release the lock
}

// printStatistics prints the message statistics of the node
func printStatistics() {
	// mutex.Lock()         // Acquire the mutex to safely read the global counters
	// defer mutex.Unlock() // Ensure the mutex is released when the function exits

	// Print the counters for sent, received, and relayed messages, as well as the sums of sent and received messages
	fmt.Printf("Messages Sent: %d\n", sendTracker)
	fmt.Printf("Messages Received: %d\n", receiveTracker)
	fmt.Printf("Messages Relayed: %d\n", relayTracker)
	fmt.Printf("Sum of Sent Messages: %d\n", sendSummation)
	fmt.Printf("Sum of Received Messages: %d\n", receiveSummation)
}

func setRegistryHostPort(value string) {
	mutex.Lock()         // Lock the mutex before modifying the global variable
	defer mutex.Unlock() // Unlock the mutex after modifying the global variable

	registryHostPort = value
}

func getRegistryHostPort() string {
	mutex.Lock()         // Lock the mutex before reading the global variable
	defer mutex.Unlock() // Unlock the mutex after reading the global variable

	return registryHostPort
}

// main is the entry point of the program.
func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run messenger.go <registry-host:registry-port>") // Print usage instructions if incorrect arguments are provided.
		return
	}

	registryHostPort := os.Args[1] // Get the registry address from command-line arguments.
	if !strings.Contains(registryHostPort, ":") {
		fmt.Println("Invalid registry address. Must be in the format host:port.") // Validate the registry address format.
		return
	}

	// Debugging statement to check the initial value of registryHostPort
	log.Printf("[main] Initial registryHostPort: %s", registryHostPort)

	// Set the registry host port safely
	setRegistryHostPort(os.Args[1])

	// Start the node with the safe getter for registryHostPort
	go StartNode(getRegistryHostPort())

	startCommandInterface() // Start the interactive command interface.
}

// startCommandInterface runs the interactive command interface for the node.
func startCommandInterface() {
	reader := bufio.NewReader(os.Stdin)                                      // Create a reader for stdin
	fmt.Println("Messaging Node command interface started. Enter commands:") // Print a welcome message

	for {
		fmt.Print("> ")                     // Print the command prompt
		cmd, err := reader.ReadString('\n') // Read a command from stdin
		if err != nil {
			fmt.Println("Error reading command:", err) // Handle errors in reading commands.
			continue
		}
		cmd = strings.TrimSpace(cmd) // Remove trailing newline characters from the command.

		switch cmd {
		case "print":
			printStatistics() // Handle the 'print' command

		case "exit":
			fmt.Println("Exiting messaging node.") // Handle the 'exit' command.
			log.Printf("[startCommandInterface] Exit command received. registryHostPort: %s, Node ID: %d, Address: %s", registryHostPort, myNodeID, localAddr)
			deregisterFromRegistry(registryHostPort, myNodeID, localAddr) // Deregister from the registry before exiting.

		default:
			fmt.Printf("Command not understood: %s\n", cmd) // Handle unrecognized commands.
		}
	}
}
