package main

import (
	"encoding/binary"
	"fmt"
	"group8/minichord"
	"log"
	"net"

	"google.golang.org/protobuf/proto"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:12345") // Change to your registry's actual address and port
	if err != nil {
		log.Fatalf("Failed to connect to registry: %v", err)
	}
	defer conn.Close()

	deregMsg := &minichord.MiniChord{
		Message: &minichord.MiniChord_Deregistration{
			Deregistration: &minichord.Deregistration{
				Id:      73,
				Address: "130.208.29.127:50507",
			},
		},
	}

	data, err := proto.Marshal(deregMsg)
	if err != nil {
		log.Fatalf("Failed to marshal message: %v", err)
	}

	// Send the message size followed by the message
	if err := binary.Write(conn, binary.BigEndian, uint64(len(data))); err != nil {
		log.Fatalf("Failed to write message size: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		log.Fatalf("Failed to write message: %v", err)
	}

	fmt.Println("Deregistration message sent")
}
