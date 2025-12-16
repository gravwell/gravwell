package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	flag.Parse()

	if err := os.MkdirAll(testsDir, 0755); err != nil {
		log.Fatalf("Failed to create tests directory: %v", err)
	}
	log.Printf("Tests directory ready at: %s", testsDir)

	// Setup UDP listener
	addr := fmt.Sprintf(":%d", port)
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Fatalf("Failed to resolve UDP address: %v", err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("Failed to listen on UDP port %d: %v", port, err)
	}
	defer conn.Close()

	log.Printf("Listening for sFlow packets on UDP port %d...", port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	stopChan := make(chan struct{})

	go listenToPackets(conn, stopChan)

	<-sigChan
	log.Println("\nReceived interrupt signal, shutting down...")
	close(stopChan)
	time.Sleep(100 * time.Millisecond)
}
