/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/gravwell/gravwell/v3/sflow"
)

func listenToPackets(conn *net.UDPConn, stopChan chan struct{}) {
	buffer := make([]byte, datagramSize)
	packetCount := 0
	successCount := 0
	failCount := 0

	for {
		select {
		case <-stopChan:
			log.Printf("\nCollection complete: %d total packets, %d decoded successfully, %d failed",
				packetCount, successCount, failCount)
			return
		default:
			// Set read deadline to allow checking stopChan periodically
			// It's a shitty script, get off my lawn
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			n, remoteAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, check stopChan and continue
				}
				log.Printf("Error reading from UDP: %v", err)
				continue
			}

			packetCount++
			packetData := make([]byte, n)
			copy(packetData, buffer[:n])

			// Attempt to decode the packet
			decoder := sflow.NewDecoder(bytes.NewReader(packetData))
			dgram, err := decoder.Decode()

			timestamp := time.Now().Unix()
			var baseName string

			if err != nil {
				failCount++
				baseName = fmt.Sprintf("sflow_decodefail_%d", timestamp)
				log.Printf("[FAIL] Packet #%d from %s: decode error: %v", packetCount, remoteAddr, err)
			} else {
				successCount++
				baseName = buildBaseName(dgram)
				log.Printf("[OK] Packet #%d from %s: v%d seq=%d samples=%d",
					packetCount, remoteAddr, dgram.Version, dgram.SequenceNumber, dgram.SamplesCount)
			}

			basePath := filepath.Join(fixturesDir, baseName)

			// Write fixtures if decode was successful
			if err == nil && dgram != nil {
				goFileBuf, genErr := generateGoFixture(baseName, dgram)
				if genErr != nil {
					log.Printf("Failed to generate Go test: %v", genErr)
				} else if writeErr := writeFixtures(basePath, packetData, goFileBuf); writeErr != nil {
					log.Printf("Failed to write fixtures: %v", writeErr)
				} else {
					log.Printf("Saved test: %s.bin and %s_test.go (%d bytes)", baseName, baseName, n)
				}
			} else {
				// Just write the .bin file for failed decodes
				binPath := basePath + ".bin"
				if writeErr := os.WriteFile(binPath, packetData, 0644); writeErr != nil {
					log.Printf("Failed to write packet to %s: %v", binPath, writeErr)
				} else {
					log.Printf("Saved binary to: %s.bin (%d bytes)", baseName, n)
				}
			}
		}
	}
}
