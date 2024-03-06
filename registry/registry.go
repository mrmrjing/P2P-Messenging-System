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

	"google.golang.org/protobuf/proto"
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
	Nodes              map[int]NodeInfo                  // Map from node IDs to NodeInfo, representing all registered nodes
	AddrMap            map[string]bool                   // Map from node addresses to a boolean, used to check for address uniqueness
	Mutex              sync.RWMutex                      // Mutex for safe concurrent access to the Registry's fields
	NodesTaskFinished  map[int]bool                      // Map from node IDs to a boolean, representing if the node has finished the task
	NodesSetupFinished map[int]bool                      // Map from node IDs to a boolean, representing if the node has finished the setup
	TrafficSummaries   map[int]*minichord.TrafficSummary // Map to store traffic summaries for each node
}

// This function creates a new instance of Registry with initialized fields, returns a pointer to the Registry type
func NewRegistry() *Registry {
	return &Registry{
		Nodes:              make(map[int]NodeInfo),                  // Initialize the Nodes map
		AddrMap:            make(map[string]bool),                   // Initialize the AddrMap
		NodesTaskFinished:  make(map[int]bool),                      // Initialize the NodesTaskFinished map
		NodesSetupFinished: make(map[int]bool),                      // Initialize the NodesSetupFinished map
		TrafficSummaries:   make(map[int]*minichord.TrafficSummary), // Initialize the TrafficSummaries map
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

	// Receive and decode the incoming message sent from the messaging nodes
	msg, err := ReceiveMiniChordMessage(conn)
	if err != nil {
		log.Printf("Error receiving MiniChord message: %v\n", err)
		return
	}

	// Handle the message based on its type
	switch msg := msg.Message.(type) { // Initiate a type switch on the message type
	case *minichord.MiniChord_Registration:
		r.handleRegister(conn, msg.Registration) // Handle registration messages
	case *minichord.MiniChord_Deregistration:
		r.handleDeregister(conn, msg.Deregistration) // Handle deregistration messages
	case *minichord.MiniChord_TaskFinished:
		r.handleTaskFinished(conn, msg.TaskFinished) // Handle task finished messages
	case *minichord.MiniChord_NodeRegistryResponse: // Handle node registry response messages
		var success bool
		const failureCode uint32 = 4294967294
		result := msg.NodeRegistryResponse.Result // A sfixed 32 signed integer
		if result != failureCode {
			success = true
		} else {
			success = false // If the result is the failure code, set success to false
		}
		r.HandleNodeRegistryResponse(int(result), success) // Dispatch to the handler
	case *minichord.MiniChord_ReportTrafficSummary:
		r.handleTrafficSummary(conn, msg.ReportTrafficSummary) // Handle traffic summary messages
	default:
		log.Printf("Unknown message type received\n") // Log if an unknown message type is received
	}
}

// This function reads and decodes a MiniChord message from the specified connection
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
		log.Printf("ReceivedMiniChordMessage() length error: expected %d bytes, got %d\n", I64SIZE, length)
		return
	}
	numBytes := int(binary.BigEndian.Uint64(bs))
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
		return
	}
	message = &minichord.MiniChord{}
	err = proto.Unmarshal(data[:length], message)
	if err != nil {
		log.Printf("ReceivedMiniChordMessage() unmarshal error: %s\n", err)
		return
	}
	return
}

// This function encodes and sends a MiniChord message to the specified connection
func SendMiniChordMessage(conn net.Conn, message *minichord.MiniChord) (err error) {
	data, err := proto.Marshal(message)
	log.Printf("SendMiniChordMessage(): sending %s (%v), %d to %s\n",
		message, data, len(data), conn.RemoteAddr().String())
	if err != nil {
		log.Panicln("Failed to marshal message:", err)
	}
	// First send the number of bytes in the marshaled message
	bs := make([]byte, I64SIZE)
	binary.BigEndian.PutUint64(bs, uint64(len(data)))
	length, err := conn.Write(bs)
	if err != nil {
		log.Printf("SendMiniChordMessage() error sending length: %s\n", err)
	}
	if length != I64SIZE {
		log.Panicln("Short write?")
	}
	length, err = conn.Write(data)
	if err != nil {
		log.Printf("SendMiniChordMessage() error sending data: %s\n", err)
	}
	if length != len(data) {
		log.Panicln("Short write?")
	}
	return
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
	fullAddr := registration.Address                                     // Extract the address from the registration message
	log.Printf("Attempting to register node with address: %s", fullAddr) // Log the attempt to register

	// Prevent duplicate registrations by checking if the address is already in use
	if _, exists := r.AddrMap[fullAddr]; exists { // check if the value associated with the key fullAddr exists in the Addrmap
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_RegistrationResponse{
				RegistrationResponse: &minichord.RegistrationResponse{
					Result: -3, // A negative number indicates failure
					Info:   "Node already registered",
				},
			},
		})
		return
	}

	// Verify that the connection's remote address matches the provided address
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr) // Type assert the remote address to a TCP address
	providedIP := strings.Split(fullAddr, ":")[0]  // Extract the IP part of the provided address, and leaving out the port number

	// Allow local connections regardless of the provided IP, only reject if it's a non-local connection and the IPs don't match
	if !remoteAddr.IP.IsLoopback() && remoteAddr.IP.String() != providedIP {
		SendMiniChordMessage(conn, &minichord.MiniChord{
			Message: &minichord.MiniChord_RegistrationResponse{
				RegistrationResponse: &minichord.RegistrationResponse{
					Result: -4,
					Info:   "IP address mismatch between request and connection",
				},
			},
		})
		return
	}

	// Generate a unique new node ID (0-127) while ensuring that no duplicate IDs are assigned
	var nodeID int
	for {
		nodeID = rand.Intn(128)                    // Generate a random node ID (0-127)
		if _, exists := r.Nodes[nodeID]; !exists { // Check if the generated ID is already in use by searching through the Nodes map
			break // Stop the loop if the generated ID is not in use, otherwise continue to generate a new ID
		}
	}

	// Add the node to the registry after passing all checks
	r.Nodes[nodeID] = NodeInfo{ID: nodeID, Addr: fullAddr, Conn: conn}
	r.AddrMap[fullAddr] = true // Mark the address as registered

	// Construct the success message including the current size of the registry
	successMessage := fmt.Sprintf("Registration request successful. The number of messaging nodes currently constituting the overlay is (%d).", len(r.Nodes))

	// Upon successful registration, send a success messge back to client
	err := SendMiniChordMessage(conn, &minichord.MiniChord{
		Message: &minichord.MiniChord_RegistrationResponse{
			RegistrationResponse: &minichord.RegistrationResponse{
				Result: int32(nodeID),
				Info:   successMessage,
			},
		},
	})
	// Check if the message was successfully sent. In the rare case that a messaging node fails just after it sends a registration request, delete the node from the NodeInfo
	if err != nil {
		log.Printf("[handleRegister] Failed to send registration response to node %d at %s", nodeID, fullAddr)
		r.Mutex.Lock()
		delete(r.Nodes, nodeID)
		delete(r.AddrMap, fullAddr)
		r.Mutex.Unlock()
		log.Printf("[handleRegister] Removed node %d from registry due to communication failure", nodeID)
		return
	}
}

// This function handles the deregistration of nodes from the registry
func (r *Registry) handleDeregister(conn net.Conn, deregistration *minichord.Deregistration) {
	log.Printf("[handleDeregister] Attempting to deregister node")
	r.Mutex.Lock()
	log.Printf("[handleDeregister] Lock acquired for deregistration")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handledeRegister] Lock released after deregistration")
	}()
	nodeID := int(deregistration.Id)

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
		return
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
		return
	}

	// Remove the node from the registry after passing all validations
	delete(r.Nodes, nodeID)
	delete(r.AddrMap, deregistration.Address)
	delete(r.TrafficSummaries, nodeID)  // Remove the traffic summary for the node
	delete(r.NodesTaskFinished, nodeID) // Remove the task finished flag for the node

	// Send a positive response to the node indicating successful deregistration
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
	log.Printf("[setupOverlay] Attempting to setup overlay")
	r.Mutex.Lock()
	log.Printf("[setupOverlay] Lock acquired for setting up overlay")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[setupOverlay] Lock released after setting up overlay")
	}()

	// Check if there are nodes in the registry
	if len(r.Nodes) == 0 {
		fmt.Println("No nodes registered in the registry. Overlay setup aborted.")
		return
	}

	// Default to 3 routing entries if not specified, where nr is the number of routing entries (table size)
	if nr == 0 {
		nr = 3
	}

	// Sort node IDs to facilitate table setup, which is to ensure that the routing tables are consistent across all nodes in determining which nodes are neighbors
	nodeIDs := make([]int, 0, len(r.Nodes))
	for id := range r.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Ints(nodeIDs) // Sort the IDs for consistent ordering

	// Iterate through each node to calculate its routing table, First entry is one hop away, second entry is two hops away, third entry is four hops away, and so on
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
}

// This function sends the routing table to the specified node
func (r *Registry) sendNodeRegistry(nodeID int, routingTable []NodeInfo, allNodeIDs []int) {
	// Construct the message with the routing table information
	nodeRegistryMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_NodeRegistry{
			NodeRegistry: &minichord.NodeRegistry{
				NR:    uint32(len(routingTable)),            // Number of routing entries
				Peers: convertNodeInfoToProto(routingTable), // Convert NodeInfo to a repeated Deregistration map
				NoIds: uint32(len(allNodeIDs)),              // Total number of nodes
				Ids:   convertIDsToInt32s(allNodeIDs),       // Convert node IDs to int32 slice
			},
		},
	}

	// Retrieve the node's address from your system's node information storage
	nodeAddress, err := r.getNodeAddressByID(nodeID)
	if err != nil {
		log.Printf("Failed to get address for node %d: %s", nodeID, err)
		return
	}

	// Establish a new connection to the node using the retrieved address
	conn, err := net.Dial("tcp", nodeAddress)
	if err != nil {
		log.Printf("Failed to establish a new connection to node %d at %s: %s", nodeID, nodeAddress, err)
		return
	}
	defer conn.Close() // Ensure the connection is closed after this function exits

	// Send the NodeRegistry message to the node, ensuring that each node receives its unique routing table and information about all other nodes
	if err := SendMiniChordMessage(conn, nodeRegistryMsg); err != nil {
		log.Printf("Failed to send NodeRegistry message to node %d: %s", nodeID, err)
	} else {
		log.Printf("Successfully sent NodeRegistry to Node %d", nodeID)
	}
}

// This function retrieves the network address of a node identified by its ID
func (r *Registry) getNodeAddressByID(nodeID int) (string, error) {
	nodeInfo, exists := r.Nodes[nodeID]
	if !exists {
		// No node with the provided ID was found in the registry
		log.Printf("No node found with ID %d", nodeID)
		return "", fmt.Errorf("no node with ID %d found", nodeID)
	}

	// Return the address of the node if it exists
	return nodeInfo.Addr, nil
}

// This function converts a slice of NodeInfo into a slice of protobuf Deregistration messages
func convertNodeInfoToProto(routingTable []NodeInfo) []*minichord.Deregistration {
	peers := make([]*minichord.Deregistration, len(routingTable))
	for i, nodeInfo := range routingTable { // Iterate through the routing table and convert each entry to a Deregistration message
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
		int32Ids[i] = int32(id)
	}
	return int32Ids
}

// This function sends a message TaskInitiate to all nodes in the overlay network
func (r *Registry) StartMessaging(messageCount int) {
	log.Printf("[StartMessaging] Attempting to start messaging with %d messages per node", messageCount)
	r.Mutex.Lock()
	log.Printf("[StartMessaging] Lock acquired for start messaging with %d messages per node", messageCount)
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[StartMessaging] Lock released after start messaging with %d messages per node", messageCount)
	}()

	// Check if there are nodes in the registry
	if len(r.Nodes) == 0 {
		fmt.Println("No nodes registered in the registry. Messaging cannot be started.")
		return // Exit the function early if there are no nodes
	}

	// Check if all nodes have finished their setup, not just their tasks
	allSetupComplete := true
	for id := range r.Nodes {
		if setupComplete, exists := r.NodesSetupFinished[id]; !exists || !setupComplete {
			allSetupComplete = false
			break
		}
	}
	if !allSetupComplete {
		fmt.Println("Not all nodes are ready. Aborting messaging start.")
		return
	}

	fmt.Printf("Initiating messaging with %d messages per node...\n", messageCount)
	for _, nodeInfo := range r.Nodes {
		// Establish a new connection to the node
		conn, err := net.Dial("tcp", nodeInfo.Addr)
		if err != nil {
			log.Printf("Failed to establish a new connection to node %d at %s: %s", nodeInfo.ID, nodeInfo.Addr, err)
			continue
		}
		defer conn.Close()

		// Construct the message for initiating messaging
		msg := &minichord.MiniChord{
			Message: &minichord.MiniChord_InitiateTask{
				InitiateTask: &minichord.InitiateTask{
					Packets: uint32(messageCount), // Set the number of messages to be sent
				},
			},
		}
		// Send the InitiateTask message to the node over the new connection
		if err := SendMiniChordMessage(conn, msg); err != nil {
			log.Printf("Error initiating messaging for Node ID %d: %s\n", nodeInfo.ID, err)
		} else {
			log.Printf("Successfully initiated messaging for Node ID %d", nodeInfo.ID)
		}
	}
}

// This function handles the update of the node setup status
func (r *Registry) HandleNodeRegistryResponse(nodeID int, success bool) {
	log.Printf("[HandleNodeSetupComplete] Node %d setup complete status: %v", nodeID, success)
	r.Mutex.Lock() // Lock the registry for writing, to prevent concurrent write operations

	// Update the node's setup completion status
	r.NodesSetupFinished[nodeID] = success
	r.Mutex.Unlock() // Unlock as soon as critical section is over to avoid holding lock while printing

	r.Mutex.RLock() // Read-lock the registry for safe reading
	allSetupComplete := true
	// Check if all nodes have finished setup successfully
	for id := range r.Nodes {
		setupComplete, exists := r.NodesSetupFinished[id]
		// If a node is missing from the setup finished map or it's not marked as complete, setup is not complete
		if !exists || !setupComplete {
			allSetupComplete = false
			break // Exit loop early if any node hasn't finished setup or failed
		}
	}
	r.Mutex.RUnlock() // Unlock as soon as critical section is over to avoid holding lock while printing

	// Log the setup status update after releasing the lock
	log.Printf("[HandleNodeSetupComplete] Node %d setup status updated", nodeID)

	// If all nodes have finished setup, proceed to the next stage
	if allSetupComplete {
		fmt.Println("All nodes have completed setup. The registry is now ready to initiate tasks.")
	}
}

// This function handles the task finished messages from nodes, if the tasks are done, request traffic summaries from all nodes
func (r *Registry) handleTaskFinished(_ net.Conn, taskFinished *minichord.TaskFinished) {
	log.Printf("[handleTaskFinished] Attempting to handle task finished message")
	r.Mutex.Lock()
	log.Printf("[handlesTaskFinished] Lock acquired for handling task finished message")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[handlesTaskFinished] Lock released after handling task finished message")
	}()
	nodeID := int(taskFinished.Id)
	r.NodesTaskFinished[nodeID] = true // Set the task finished flag to true for the specified node

	// Print length of nodes task finished
	log.Printf("[handleTaskFinished] Nodes task finished: %d", len(r.NodesTaskFinished))

	// Print length of nodes
	log.Printf("[handleTaskFinished] Nodes: %d", len(r.Nodes))

	// Check if all nodes have finished the task
	if len(r.NodesTaskFinished) == len(r.Nodes) {
		// Request traffic summaries from all nodes
		for _, nodeInfo := range r.Nodes {
			// Establish a new TCP connection to the node
			conn, err := net.Dial("tcp", nodeInfo.Addr)
			if err != nil {
				log.Printf("Failed to establish a new connection to node %d at %s: %s", nodeInfo.ID, nodeInfo.Addr, err)
				continue
			}

			// Construct the message for requesting traffic summaries
			msg := &minichord.MiniChord{
				Message: &minichord.MiniChord_RequestTrafficSummary{
					RequestTrafficSummary: &minichord.RequestTrafficSummary{},
				},
			}

			// Send the RequestTrafficSummary message to the node over the new connection
			if err := SendMiniChordMessage(conn, msg); err != nil {
				log.Printf("Error requesting traffic summary for Node ID %d: %s\n", nodeInfo.ID, err)
			} else {
				log.Printf("Successfully requested traffic summary for Node ID %d", nodeInfo.ID)
			}
			// Close the connection after sending the message
			conn.Close()
		}
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

	// Store the traffic summary for the corresponding node
	nodeID := int(trafficSummary.Id) // Convert the node ID from the request to an integer
	r.TrafficSummaries[nodeID] = trafficSummary

	// Check if traffic summaries have been received from all nodes
	if len(r.TrafficSummaries) == len(r.Nodes) {
		// All summaries received, print the traffic summary for each node
		r.printTrafficSummariesCSV()
	}
}

// This function prints the traffic summaries for all nodes in the form of a table
func (r *Registry) printTrafficSummariesCSV() {
	// Print the header in CSV format
	fmt.Println("Node, Sent, Packets Received, Relayed, TotalSent, TotalReceived")

	var totalSent, totalReceived, totalRelayed int
	var totalSumSent, totalSumReceived int64

	// Iterate through the summaries and print each in CSV format
	for id, summary := range r.TrafficSummaries {
		fmt.Printf("%d,%d,%d,%d,%d,%d\n",
			id, summary.Sent, summary.Received, summary.Relayed,
			summary.TotalSent, summary.TotalReceived)

		// Accumulate totals for final correctness verification
		totalSent += int(summary.Sent)            // sendTracker
		totalReceived += int(summary.Received)    // receiveTracker
		totalRelayed += int(summary.Relayed)      // relayTracker
		totalSumSent += summary.TotalSent         // sumSummation
		totalSumReceived += summary.TotalReceived // receiveSummation
	}

	// Print total summaries in CSV format
	fmt.Printf("Total: %d, %d, %d, %d, %d\n",
		totalSent, totalReceived, totalRelayed, totalSumSent, totalSumReceived)

	// Print correctness verification
	if totalSent == totalReceived && totalSumSent == totalSumReceived {
		fmt.Println("Correctness: Verified")
	} else {
		fmt.Println("Correctness: Not Verified")
	}

	// After printing the traffic summaries and verifying correctness, clear the traffic summaries map and the nodes task finished map
	r.TrafficSummaries = make(map[int]*minichord.TrafficSummary)
	r.NodesTaskFinished = make(map[int]bool)
}

// This function prints all currently registered nodes' hostnames, port number and node ID
func (r *Registry) ListNodes() {
	log.Printf("[ListNodes] Attempting to list nodes")
	r.Mutex.RLock()
	log.Printf("[ListNodes] Lock acquired for listing nodes")
	defer func() {
		r.Mutex.RUnlock()
		log.Printf("[ListNodes] Lock released after listing nodes")
	}()
	fmt.Println("Listing all registered nodes:")
	if len(r.Nodes) == 0 {
		fmt.Println("No nodes available in the registry.")
		return
	}
	for id, info := range r.Nodes {
		// Split the address into hostname and port
		parts := strings.Split(info.Addr, ":") // The address is always in the "hostname:port" format
		var hostname, port string
		if len(parts) == 2 {
			hostname, port = parts[0], parts[1] // Separate the hostname and the port if address format is correct
		} else {
			log.Printf("Invalid address format for Node ID: %d, Address: %s", id, info.Addr)
			continue
		}
		fmt.Printf("Node ID: %d, Hostname: %s, Port: %s\n", id, hostname, port) // Print each node's ID, hostname, and port number
	}
}

// This function sets up the overlay network with the specified number of routing entries per node (n)
func (r *Registry) SetupOverlay(entries int) {
	fmt.Printf("Setting up overlay with %d entries per node...\n", entries)

	// Check if there are nodes in the registry
	r.Mutex.Lock()
	if len(r.Nodes) == 0 {
		fmt.Println("No nodes registered in the registry. Overlay setup aborted.")
		r.Mutex.Unlock()
		return
	}
	r.Mutex.Unlock()

	// If there are nodes, proceed with setting up the overlay
	r.setupOverlay(entries)
	fmt.Println("Overlay setup complete.")
}

// This function would list the computed routing tables for each node in the overlay
func (r *Registry) ListRoutes() {
	log.Printf("[ListRoutes] Attempting to list routes")
	r.Mutex.Lock()
	log.Printf("[ListRoutes] Lock acquired for listing routes")
	defer func() {
		r.Mutex.Unlock()
		log.Printf("[ListRoutes] Lock released after listing routes")
	}()

	fmt.Println("Listing routing tables for all nodes:")
	if len(r.Nodes) == 0 {
		fmt.Println("No nodes available in the registry.")
		return
	}

	for id, node := range r.Nodes {
		fmt.Printf("Routing table for Node ID: %d, Address: %s\n", id, node.Addr)
		if len(node.RoutingTable) > 0 {
			fmt.Println("  Routes to Node IDs:")
			for _, route := range node.RoutingTable {
				fmt.Printf("    %d\n", route.ID)
			}
		} else {
			fmt.Println("  No routes available.")
		}
	}
}

// main is the entry point of the program
func main() {
	// Check for the correct number of command-line arguments
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run registry.go <registry-port>") // Print usage if incorrect arguments
		return
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

		case strings.HasPrefix(cmd, "setup"):
			parts := strings.Split(cmd, " ") // Split the command into parts
			n := 3                           // Default number of entries
			if len(parts) == 2 {
				var err error
				n, err = strconv.Atoi(parts[1])
				if err != nil {
					fmt.Println("Invalid number for setup command, using default value of 3:", err)
				}
			} else if len(parts) > 2 {
				fmt.Println("Invalid setup command usage. Use 'setup <number>'. Using default value of 3.")
			}
			registry.SetupOverlay(n)

		case cmd == "route":
			registry.ListRoutes() // List routing tables for all nodes

		case strings.HasPrefix(cmd, "start "):
			// Check if all nodes have completed their setup instead of just checking AllNodesReady
			allSetupComplete := true
			for id := range registry.Nodes {
				if setupComplete, exists := registry.NodesSetupFinished[id]; !exists || !setupComplete {
					allSetupComplete = false
					fmt.Printf("Node %d is not ready.\n", id)
					break
				}
			}
			if allSetupComplete { // If all nodes have finished setup, start messaging
				parts := strings.Split(cmd, " ") // Split the command into parts
				if len(parts) == 2 {
					n, err := strconv.Atoi(parts[1])
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
			fmt.Println("Exiting registry.")
			return
		default:
			fmt.Printf("Command not understood: %s\n", cmd)
		}
	}
}
