package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"group8/minichord"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto" // For Protocol Buffers, Google's data interchange format
)

// NodeInfo contains the information about a node in the network
type NodeInfo struct {
	ID           int        // Unique identifier for the node
	Addr         string     // Network address of the node
	Conn         net.Conn   // TCP connection to the node
	RoutingTable []NodeInfo // Routing table for the node
}

// Constant representing the size of an int64 in bytes, used when encoding/decoding binary data
const I64SIZE = 8

// Registry is a data structure which holds the state of the server, including registered nodes and their information
type Registry struct {
	Nodes             map[int]NodeInfo                  // Map from node IDs to NodeInfo, representing all registered nodes
	AddrMap           map[string]bool                   // Map from node addresses to a boolean, used to check for address uniqueness
	Mutex             sync.RWMutex                      // Mutex for safe concurrent access to the Registry's fields
	NodesTaskFinished map[int]bool                      // Map from node IDs to a boolean, representing if the node has finished the task
	AllNodesReady     bool                              // Flag to indicate if all nodes are ready to start
	TrafficSummaries  map[int]*minichord.TrafficSummary // Map to store traffic summaries for each node
}

// This function creates a new instance of Registry with initialized fields, returns a pointer to the Registry type
func NewRegistry() *Registry {
	return &Registry{
		Nodes:             make(map[int]NodeInfo),                  // Initialize the Nodes map
		AddrMap:           make(map[string]bool),                   // Initialize the AddrMap
		NodesTaskFinished: make(map[int]bool),                      // Initialize the NodesTaskFinished map
		TrafficSummaries:  make(map[int]*minichord.TrafficSummary), // Initialize the TrafficSummaries map
	}
}

// This function initializes the TCP server for the registry and listens for incoming connections on the specified port
func (r *Registry) Start(port string) {
	listener, err := net.Listen("tcp", ":"+port) // Listen on all interfaces at the specified port
	if err != nil {
		fmt.Printf("Failed to start server: %s\n", err)
		return // If server fails to start, exit the function
	}
	defer listener.Close() // Ensure the listener is closed when the function exits using the defer keyword for cleanup

	// Continuously accept new connections to ensure that the server is always available to clients, and handle each connection in a new goroutine
	for {
		conn, err := listener.Accept() // Accept a new connection
		if err != nil {
			fmt.Printf("Failed to accept connection: %s\n", err)
			continue // If there's an error accepting the connection, skip to the next loop iteration to accept the next connection
		}
		go r.handleConnection(conn) // Handle the connection concurrently in a new goroutine. This allows the server to handle multiple connections simultaneously
	}
}

// This function deals with incoming messages from a connection, directing them to the appropriate handlers
func (r *Registry) handleConnection(conn net.Conn) {

	// Receive and decode the incoming message from the connection
	msg, err := ReceiveMiniChordMessage(conn)
	if err != nil {
		log.Printf("Error receiving MiniChord message: %v\n", err)
		return // If there's an error, exit the function
	}

	// Handle the message based on its type
	switch msg := msg.Message.(type) { // Initiate a type switch on the message type
	case *minichord.MiniChord_Registration:
		r.handleRegister(conn, msg.Registration) // Handle registration messages
	case *minichord.MiniChord_Deregistration:
		r.handleDeregister(conn, msg.Deregistration) // Handle deregistration messages
	case *minichord.MiniChord_TaskFinished: // Handle task finished messages
		r.handleTaskFinished(conn, msg.TaskFinished)
	case *minichord.MiniChord_NodeRegistryResponse: // Handle node registry response messages
		var success bool
		const failureCode uint32 = 4294967294
		result := msg.NodeRegistryResponse.Result // A sfixed 32 signed integer
		if result != failureCode {
			success = true
		} else { // If the result is the failure code, set success to false
			success = false
		}
		r.HandleNodeRegistryResponse(int(result), success) // Dispatch to the handler
	case *minichord.MiniChord_ReportTrafficSummary: // Handle traffic summary messages
		r.handleTrafficSummary(conn, msg.ReportTrafficSummary)

	default:
		log.Printf("Unknown message type received\n") // Log if an unknown message type is received
	}
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
	numBytes := uint64(binary.BigEndian.Uint64(bs)) // Decode the length prefix

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

// This function encodes and sends a MiniChord message to the specified connection
func SendMiniChordMessage(conn net.Conn, message *minichord.MiniChord) (err error) {
	data, err := proto.Marshal(message) // Marshal the message into binary format
	log.Printf("SendMiniChordMessage(): sending %s (%v), %d to %s\n",
		message, data, len(data), conn.RemoteAddr().String())
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
	length_, err := conn.Write(data)
	if err != nil {
		log.Printf("SendMiniChordMessage() error sending data: %s\n", err)
	}
	if length_ != len(data) {
		log.Panicln("Short write?")
	}
	return // Successfully sent the message
}

// This function handles the registration of new nodes within the registry
func (r *Registry) handleRegister(conn net.Conn, registration *minichord.Registration) {
	log.Printf("[handleRegister] Attempting to register node")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[handleRegister] Lock acquired for registration")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handleRegister] Lock released after registration")
	}()
	// defer r.Mutex.Unlock() // Unlock when the function exits

	fullAddr := registration.Address                                     // Extract the address from the registration message
	log.Printf("Attempting to register node with address: %s", fullAddr) // Log the attempt to register

	// Prevent duplicate registrations by checking if the address is already in use
	if _, exists := r.AddrMap[fullAddr]; exists { // check if the value associated with the key fullAddr exists in the Addrmap
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_RegistrationResponse{
				RegistrationResponse: &minichord.RegistrationResponse{
					Result: -3,
					Info:   "Node already registered",
				},
			},
		})
		return // Exit the function to prevent further processing
	}

	// Generate a unique new node ID while ensuring that no duplicate IDs are assigned
	var nodeID int
	for {
		nodeID = rand.Intn(128)                    // Generate a random node ID (0-127)
		if _, exists := r.Nodes[nodeID]; !exists { // Check if the generated ID is already in use by searching through the Nodes map
			break // Stop the loop if the generated ID is not in use, otherwise continue to generate a new ID
		}
	}

	// Verify that the connection's remote address matches the provided address, handling potential mismatches
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr) // Type assert the remote address to a TCP address
	providedIP := strings.Split(fullAddr, ":")[0]  // Extract the IP part of the provided address, and leaving out the port number

	// Log the remote and provided IP addresses for diagnostics
	// log.Printf("Provided IP: %s, Remote IP: %s", providedIP, remoteAddr.IP.String())

	// Allow local connections regardless of the provided IP
	if !remoteAddr.IP.IsLoopback() && remoteAddr.IP.String() != providedIP {
		// Only reject if it's a non-local connection and the IPs don't match
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_RegistrationResponse{
				RegistrationResponse: &minichord.RegistrationResponse{
					Result: -4,
					Info:   "IP address mismatch between request and connection",
				},
			},
		})
		return // Exit if the IP addresses do not match
	}

	// Add the node to the registry after passing all checks
	r.Nodes[nodeID] = NodeInfo{ID: nodeID, Addr: fullAddr, Conn: conn}
	r.AddrMap[fullAddr] = true // Mark the address as registered

	// Construct the success message including the current size of the registry
	successMessage := fmt.Sprintf("Registration request successful. The number of messaging nodes currently constituting the overlay is (%d).", len(r.Nodes))

	// Upon successful registration, send a success messge back to client
	SendMiniChordMessage(conn, &minichord.MiniChord{
		Message: &minichord.MiniChord_RegistrationResponse{
			RegistrationResponse: &minichord.RegistrationResponse{
				Result: int32(nodeID),
				Info:   successMessage,
			},
		},
	})
}

// This function handles the deregistration of nodes from the registry
func (r *Registry) handleDeregister(conn net.Conn, deregistration *minichord.Deregistration) {
	// r.Mutex.Lock()         // Lock the registry for writing
	// defer r.Mutex.Unlock() // Ensure unlocking at the end of the function
	log.Printf("[handleDeregister] Attempting to deregister node")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[handleDeregister] Lock acquired for deregistration")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handledeRegister] Lock released after deregistration")
	}()

	nodeID := int(deregistration.Id) // Convert the node ID from the request to an integer

	// Retrieve the registered node information using the provided ID and validate
	nodeInfo, ok := r.Nodes[nodeID]
	if !ok {
		log.Printf("Node ID %d not found for deregistration", nodeID)
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_DeregistrationResponse{
				DeregistrationResponse: &minichord.DeregistrationResponse{
					Result: -1,
					Info:   "Node not registered",
				},
			},
		})
		return // Exit if the node ID is not found
	}

	// Verify that the address matches the registered information
	if nodeInfo.Addr != deregistration.Address {
		log.Printf("Address mismatch for Node ID %d: expected %s, got %s", nodeID, nodeInfo.Addr, deregistration.Address)
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_DeregistrationResponse{
				DeregistrationResponse: &minichord.DeregistrationResponse{
					Result: -2,
					Info:   "Invalid node ID or address mismatch",
				},
			},
		})
		return // Exit if the address does not match
	}

	// Remove the node from the registry after passing all validations
	delete(r.Nodes, nodeID)
	delete(r.AddrMap, deregistration.Address)

	// Send a positive response to the node indicating successful deregistration
	// TODO: Test if response is sent to client after deregistration
	SendMiniChordMessage(conn, &minichord.MiniChord{
		Message: &minichord.MiniChord_DeregistrationResponse{
			DeregistrationResponse: &minichord.DeregistrationResponse{
				Result: 0,
				Info:   "Deregistration successful",
			},
		},
	})
}

// This function calculates and sets up routing tables for each node based on the overlay network's structure
func (r *Registry) setupOverlay(nr int) {
	// Default to 3 routing entries if not specified, where nr is the number of routing entries (table size)
	if nr == 0 {
		nr = 3
	}

	// Sort node IDs to facilitate table setup
	// This is necessary to ensure that the routing tables are consistent across all nodes in determining which nodes are neighbors
	nodeIDs := make([]int, 0, len(r.Nodes))
	for id := range r.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Ints(nodeIDs) // Sort the IDs for consistent ordering
	log.Printf("Sorted Node IDs: %v", nodeIDs)

	// Iterate through each node to calculate its routing table
	// First entry is one hop away, second entry is two hops away, third entry is four hops away, and so on
	for _, nodeID := range nodeIDs {
		var routingTable []NodeInfo // Dynamically size the table to allow skipping self-references
		// Loop through the number of routing entries to populate the routing table
		for i := 0; i < nr; i++ {
			hop := int(math.Pow(2, float64(i)))                              // Define a variable hop that represents the distance to the next node, where i is the index of the entry in the table
			index := (sort.SearchInts(nodeIDs, nodeID) + hop) % len(nodeIDs) // Find the corresponding node index, wrapping around to the beginning if hop calculation exceeds the number of nodes
			peerID := nodeIDs[index]                                         // Get the peer node's ID
			// Check to avoid adding the same node to the routing table
			if peerID != nodeID {
				routingTable = append(routingTable, r.Nodes[peerID]) // Add the peer node's info to the routing table
			}
		}
		log.Printf("Node %d routing table: %v", nodeID, routingTable)

		// Update the node's routing table in the registry
		nodeInfo := r.Nodes[nodeID]
		nodeInfo.RoutingTable = routingTable
		r.Nodes[nodeID] = nodeInfo // Update the node info in the registry

		// Send the routing table to each node
		r.sendNodeRegistry(nodeID, routingTable, nodeIDs)
	}
	log.Println("Completed setupOverlay")
}

// This function sends the routing table to the specified node
func (r *Registry) sendNodeRegistry(nodeID int, routingTable []NodeInfo, allNodeIDs []int) {
	log.Printf("Sending NodeRegistry to Node %d", nodeID)
	// Construct the message with the routing table information
	nodeRegistryMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_NodeRegistry{
			NodeRegistry: &minichord.NodeRegistry{
				NR:    uint32(len(routingTable)),            // Number of routing entries
				Peers: convertNodeInfoToProto(routingTable), // Convert NodeInfo to the corresponding protobuf structure
				NoIds: uint32(len(allNodeIDs)),              // Total number of nodes
				Ids:   convertIDsToInt32s(allNodeIDs),       // Convert node IDs to int32 slice
			},
		},
	}

	// Retrieve the connection for the specified node in order to establish a connection to it to send them their initial setup information
	conn, err := r.getNodeConnectionByID(nodeID)
	if err != nil {
		log.Printf("Failed to get connection for node %d: %s", nodeID, err)
		return // Exit if the connection could not be retrieved
	}

	log.Printf("Connection for Node %d retrieved", nodeID)

	// Send the NodeRegistry message to the node, ensuring that each node receives its unique routing table and information about all other nodes
	if err := SendMiniChordMessage(conn, nodeRegistryMsg); err != nil {
		log.Printf("Failed to send NodeRegistry message to node %d: %s", nodeID, err)
	} else {
		log.Printf("Successfully sent NodeRegistry to Node %d", nodeID)
	}
}

// This function converts a slice of NodeInfo into a slice of protobuf Deregistration messages
func convertNodeInfoToProto(routingTable []NodeInfo) []*minichord.Deregistration {
	peers := make([]*minichord.Deregistration, len(routingTable))
	for i, nodeInfo := range routingTable {
		peers[i] = &minichord.Deregistration{
			Id:      int32(nodeInfo.ID),
			Address: nodeInfo.Addr,
		}
	}
	return peers
}

// This function converts a slice of int to a slice of int32
func convertIDsToInt32s(ids []int) []int32 {
	int32Ids := make([]int32, len(ids))
	for i, id := range ids {
		int32Ids[i] = int32(id) // Convert each int to int32
	}
	return int32Ids
}

// This function retrieves a TCP connection to a node identified by its ID
func (r *Registry) getNodeConnectionByID(nodeID int) (net.Conn, error) {
	log.Printf("Retrieving connection for Node %d", nodeID)
	log.Printf("[getNodeConnectionByID] Attempting to retrieve connection for Node %d", nodeID)
	// r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	// log.Printf("[getNodeConnectionByID] Lock acquired forNode %d", nodeID)
	// defer func() {
	// 	r.Mutex.Unlock()
	// 	log.Printf("[getNodeConnectionByID] Lock released for Node %d", nodeID)
	// }()
	// r.Mutex.Lock() // Use Lock instead of RLock to synchronize read-modify-write operations
	// defer r.Mutex.Unlock()
	nodeInfo, exists := r.Nodes[nodeID]

	log.Printf("NodeInfo: %v, exists: %v", nodeInfo, exists)

	if !exists {
		log.Printf("No node found with ID %d", nodeID)
		return nil, fmt.Errorf("no node with ID %d found", nodeID) // Return an error if the node does not exist
	}

	if nodeInfo.Conn != nil {
		log.Printf("Connection to Node %d already established", nodeID)
		return nodeInfo.Conn, nil
	}
	log.Printf("No existing connection to Node %d", nodeID)
	// Otherwise, establish a new connection to the node's address
	conn, err := net.DialTimeout("tcp", nodeInfo.Addr, 5*time.Second)
	if err != nil {
		log.Printf("Failed to connect to Node %d at address %s: %s", nodeID, nodeInfo.Addr, err)
		return nil, fmt.Errorf("failed to connect to node %d at %s: %w", nodeID, nodeInfo.Addr, err)
	}

	nodeInfo.Conn = conn       // Update the node info with the new connection
	r.Nodes[nodeID] = nodeInfo // Update the registry with the new connection
	log.Printf("Connection established with Node %d", nodeID)
	return conn, nil // Return the established connection
}

// This function prints all currently registered nodes
func (r *Registry) ListNodes() {
	log.Printf("[ListNodes] Attempting to list nodes")
	r.Mutex.RLock() // Read-lock the registry for safe reading
	log.Printf("[ListNodes] Lock acquired for listing nodes")
	defer func() {
		r.Mutex.RUnlock() // Ensure unlocking at the end of the function
		log.Printf("[ListNodes] Lock released after listing nodes")
	}()

	fmt.Println("Listing all registered nodes:")
	for id, info := range r.Nodes {
		// Split the address into hostname and port
		parts := strings.Split(info.Addr, ":")                                  // Assume the address is always in the "hostname:port" format
		hostname, port := parts[0], parts[1]                                    // Separate the hostname and the port
		fmt.Printf("Node ID: %d, Hostname: %s, Port: %s\n", id, hostname, port) // Print each node's ID, hostname, and port number
	}
}

// TODO: find out why setup (nr) is not working
// This function sets up the overlay network with the specified number of routing entries per node (n)
func (r *Registry) SetupOverlay(entries int) {
	log.Printf("[SetupOverlay] Attempting to setup overlay")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[SetupOverlay] Lock acquired for setting up overlay")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[SetupOverlay] Lock released after setting up overlay")
	}()
	// r.Mutex.Lock()         // Lock the registry for writing
	// defer r.Mutex.Unlock() // Ensure unlocking at the end of the function

	fmt.Printf("Setting up overlay with %d entries per node...\n", entries)
	r.setupOverlay(entries) // Setup the overlay network

	fmt.Println("Overlay setup complete.") // Indicate that the setup is complete
}

// This function would list the computed routing tables for each node in the overlay
func (r *Registry) ListRoutes() {
	log.Printf("[ListRoutes] Attempting to list routes")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[ListRoutes] Lock acquired for listing routes")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[ListRoutes] Lock released after listing routes")
	}()

	// r.Mutex.RLock()         // Read-lock the registry for safe reading
	// defer r.Mutex.RUnlock() // Ensure unlocking at the end of the function

	fmt.Println("Listing routing tables for all nodes:")
	for id, node := range r.Nodes {
		fmt.Printf("Routing table for Node ID: %d, Address: %s\n", id, node.Addr)
		// check if the node has a routing table and print it
		if len(node.RoutingTable) > 0 {
			fmt.Println("  Routes to Node IDs:")
			for _, routeID := range node.RoutingTable {
				fmt.Printf("    %d\n", routeID.ID)
			}
		} else {
			fmt.Println("  No routes available.")
		}
	}
}

// TODO: find out why start (n) is not working (2.5), check that the start command can only be issued after all nodes successfully establish connections to nodes that comprise its routing table
// This function sends a message TaskInitiate to all nodes in the overlay network
func (r *Registry) StartMessaging(messageCount int) {
	log.Printf("[StartMessaging] Attempting to start messaging with %d messages per node", messageCount)
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[StartMessaging] Lock acquired for start messaging with %d messages per node", messageCount)
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[StartMessaging] Lock released after start messaging with %d messages per node", messageCount)
	}()
	// r.Mutex.RLock()         // Read-lock the registry for safe reading
	// defer r.Mutex.RUnlock() // Ensure unlocking at the end of the function

	// Check if all nodes are ready, this is necessary as the nodes can only start messaging after all nodes are successfully connected
	for _, ready := range r.NodesTaskFinished {
		if !ready {
			fmt.Println("Not all nodes are ready. Aborting messaging start.")
			return // Exit the function if any node is not ready
		}
	}
	fmt.Printf("Initiating messaging with %d messages per node...\n", messageCount)
	for _, nodeInfo := range r.Nodes {
		msg := &minichord.MiniChord{
			Message: &minichord.MiniChord_InitiateTask{
				InitiateTask: &minichord.InitiateTask{
					Packets: uint32(messageCount), // Set the number of messages to be sent
				},
			},
		}
		if err := SendMiniChordMessage(nodeInfo.Conn, msg); err != nil {
			log.Printf("Error initiating messaging for Node ID %d: %s\n", nodeInfo.ID, err)
		}
	}
}

// This function handles the task finished messages from nodes, if the tasks is done, request traffic summaries from all nodes
func (r *Registry) handleTaskFinished(_ net.Conn, taskFinished *minichord.TaskFinished) {
	log.Printf("[handleTaskFinished] Attempting to handle task finished message")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[handlesTaskFinished] Lock acquired for handling task finished message")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handlesTaskFinished] Lock released after handling task finished message")
	}()

	// r.Mutex.Lock()         // Lock the registry for writing
	// defer r.Mutex.Unlock() // Ensure unlocking at the end of the function

	nodeID := int(taskFinished.Id)     // Convert the node ID from the request to an integer
	r.NodesTaskFinished[nodeID] = true // Set the task finished flag to true for the specified node

	// Check if all nodes have finished the task
	if len(r.NodesTaskFinished) == len(r.Nodes) {
		// Request traffic summaries from all nodes
		for _, nodeInfo := range r.Nodes {
			msg := &minichord.MiniChord{
				Message: &minichord.MiniChord_RequestTrafficSummary{
					RequestTrafficSummary: &minichord.RequestTrafficSummary{},
				},
			}
			if err := SendMiniChordMessage(nodeInfo.Conn, msg); err != nil {
				log.Printf("Error requesting traffic summary for Node ID %d: %s\n", nodeInfo.ID, err)
			}
		}
	}
}

// This function handles the update the setup status of nodes
func (r *Registry) HandleNodeRegistryResponse(nodeID int, success bool) {

	log.Printf("[HandleNodeRegistryResponse] Attempting to handle node registry response")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[HandleNodeRegistryResponse] Lock acquired for handling node registry response")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[HandleNodeRegistryResponse] Lock released after handling node registry response")
	}()
	// r.Mutex.Lock()
	// defer r.Mutex.Unlock()

	// Update the node's setup status
	r.NodesTaskFinished[nodeID] = success

	// Check if all nodes have finished setup successfully
	allReady := true // Assume all nodes are ready
	for _, ready := range r.NodesTaskFinished {
		if !ready {
			allReady = false
			break
		}
	}

	// If all nodes are ready, print the readiness message and set the flag
	if allReady {
		r.AllNodesReady = true
		fmt.Println("The registry is now ready to initiate tasks.")
	}
}

// This function handles the traffic summary messages from nodes
func (r *Registry) handleTrafficSummary(_ net.Conn, trafficSummary *minichord.TrafficSummary) {

	log.Printf("[handleTrafficSummary] Attempting to print traffic summary")
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations
	log.Printf("[handleTrafficSummary] Lock acquired for printing traffic summary")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handleTrafficSummary] Lock released after printing traffic summary")
	}()
	// r.Mutex.Lock()         // Lock the registry for writing
	// defer r.Mutex.Unlock() // Ensure unlocking at the end of the function

	// Store the traffic summary for the corresponding node
	nodeID := int(trafficSummary.Id) // Convert the node ID from the request to an integer
	r.TrafficSummaries[nodeID] = trafficSummary

	// Check if traffic summaries have been received from all nodes
	if len(r.TrafficSummaries) == len(r.Nodes) {
		// All summaries received, print the traffic summary for each node
		r.printTrafficSummaries()
	}
}

// This function prints the traffic summaries for all nodes in the form of a table
func (r *Registry) printTrafficSummaries() {
	fmt.Printf("%-8s %-10s %-10s %-10s %-20s %-20s\n", "Node", "Sent", "Received", "Relayed", "Total Sent", "Total Received")
	var totalSent, totalReceived, totalRelayed int
	var totalSumSent, totalSumReceived int64

	for id, summary := range r.TrafficSummaries {
		fmt.Printf("%-8d %-10d %-10d %-10d %-20d %-20d\n",
			id, summary.Sent, summary.Received, summary.Relayed,
			summary.TotalSent, summary.TotalReceived)

		// Accumulate totals
		totalSent += int(summary.Sent)
		totalReceived += int(summary.Received)
		totalRelayed += int(summary.Relayed)
		totalSumSent += summary.TotalSent
		totalSumReceived += summary.TotalReceived
	}

	// Print total row
	fmt.Printf("%-8s %-10d %-10d %-10d %-20d %-20d\n",
		"Sum", totalSent, totalReceived, totalRelayed, totalSumSent, totalSumReceived)

	// Check for correctness
	if totalSent == totalReceived && totalSumSent == totalSumReceived {
		fmt.Println("Correctness verified: True")
	} else {
		fmt.Println("Correctness verified: False")
	}
}

// main is the entry point of the program
func main() {
	// Check for the correct number of command-line arguments
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run registry.go <registry-port>") // Print usage if incorrect arguments
		return                                                   // Exit the program
	}

	registry := NewRegistry()     // Create a new registry instance
	go registry.Start(os.Args[1]) // Start the registry server in a new goroutine

	// Command-line interface loop
	reader := bufio.NewReader(os.Stdin) // Create a new reader for reading commands from stdin
	fmt.Println("Registry command interface started. Enter commands:")
	for {
		fmt.Print("> ")                     // Print a prompt
		cmd, err := reader.ReadString('\n') // Read a command from stdin
		if err != nil {
			fmt.Println("Error reading command:", err)
			continue // Skip to the next iteration on error
		}
		cmd = strings.TrimSpace(cmd) // Trim whitespace from the command

		// Handle commands
		switch {
		case cmd == "list":
			registry.ListNodes() // List all registered nodes
		case strings.HasPrefix(cmd, "setup "):
			parts := strings.Split(cmd, " ") // Split the command into parts
			if len(parts) == 2 {
				n, err := strconv.Atoi(parts[1]) // Parse the number of entries for the setup command
				if err != nil {
					fmt.Println("Invalid number for setup command:", err)
				} else {
					registry.SetupOverlay(n) // Setup the overlay with the specified number of entries}
				}
			} else {
				fmt.Println("Invalid setup command usage. Use 'setup <number>'.")
			}
		case cmd == "route":
			registry.ListRoutes() // List routing tables for all nodes
		case strings.HasPrefix(cmd, "start "):
			if registry.AllNodesReady { // Check if all nodes are ready
				parts := strings.Split(cmd, " ") // Split the command into parts
				if len(parts) == 2 {
					n, err := strconv.Atoi(parts[1]) // Parse the number of messages for the start command
					if err != nil {
						fmt.Println("Invalid number for start command:", err)
					} else {
						registry.StartMessaging(n) // Start the messaging process with the specified number of messages
					}
				} else {
					fmt.Println("Invalid start command usage. Use 'start <number>'.")
				}
			} else {
				fmt.Println("Cannot start messaging: Not all nodes are ready.")
			}
		case cmd == "exit":
			fmt.Println("Exiting registry.") // Exit command
			return                           // Exit the program
		default:
			fmt.Printf("Command not understood: %s\n", cmd) // Print an error for unrecognized commands
		}
	}
}
