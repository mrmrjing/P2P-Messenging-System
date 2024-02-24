# PA2

This project implements a simplified peer-to-peer messaging system based on distributed hash tables (DHTs). 
This projects involves constructing a logical overlay over a distributed set of nodes, and then use partial information about nodes within the overlay to route packets. The system is divided into two main components: a central registry and multiple messaging nodes. The registry manages node registrations and assists in constructing the overlay network, while messaging nodes communicate directly with each other to send and receive messages.



## Brief introduction about project: 
Nodes within the system are organized into an overlay. The overlay encompasses how nodes are organized, how they are located, and how information is maintained at each node. The logical overlay helps with locating nodes and routing content efficiently. The overlay will contain at least 10 messaging nodes, and each messaging node will be connected to some other messaging nodes. Once the overlay has been setup, messaging nodes in the system will select a node at random and send a message to that node (also known as the sink or destination
node). Rather than send this message directly to the sink node, the source node will use the overlay for communications. Each node consults its routing table, and either routes the packet to its final destination or forwards it to an intermediate
node closest (in the node ID space) to the final destination. Depending on the overlay, there may be zero or more intermediate messaging nodes between a particular source and sink that packets must pass through. Such intermediate nodes are said to relay the message. 

## Getting Started 
### Prerequisites
- Go 
- Git

### Installing 
1. Clone the repository to your local machine:
git clone https://github.com/<your-username>/<repository-name>.git
cd <repository-name>

2. Then, navigate to the project directory and build the registry and messenger node executables:
cd Group 8/registry 
go build 

cd ../messenger 
go build 

### Running the system 
1. Start the Registry 
In the registry directory:
./registry <registry-port>
Replace <registry-port> with the desired port number for the registry to listen on.
ie: go run registry.go 12345

2. Start Messaging Nodes:
In the messenger directory:
./messenger <registry-host>:<registry-port>
ie:  go run messenger.go localhost:12345
