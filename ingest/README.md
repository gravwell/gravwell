# The Gravwell Ingest API

API documentation: [https://pkg.go.dev/github.com/gravwell/gravwell/v4/ingest](https://pkg.go.dev/github.com/gravwell/gravwell/v4/ingest?tab=doc)

This package provides methods to build ingesters for the Gravwell analytics platform. Ingesters take raw data from a particular source (pcap, log files, a camera, etc.), bundle it into Entries, and ship the Entries up to the Gravwell indexers.

An Entry (defined in the sub-package [github.com/gravwell/gravwell/v4/ingest/entry](https://pkg.go.dev/github.com/gravwell/gravwell/v4/ingest/entry?tab=doc)) looks like this:

	type Entry struct {
	    TS   Timestamp
	    SRC  net.IP
	    Tag  EntryTag
	    Data []byte
	}

The most important element is Data; this is simply a discrete piece of information you want to store as an entry, be it binary or text. The Tag field associates the entry with a specific tag in the indexer, making it easier to search later. The timestamp and source IP give additional information about the entry.

Entries are sent to Gravwell indexers using an IngestMuxer, which can connect to multiple destinations simultaneously to improve ingestion rate. The example below (also found in the file `ingester_example`) shows the basics of getting Entries into Gravwell using an IngestMuxer. See github.com/gravwell/gravwell/ingesters for open-source real-world examples.

```
package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	tags    = []string{"test"}
	targets = []string{"tcp://127.0.0.1:4023"}
	secret  = "IngestSecrets" // set this to the actual ingest secret
)

// Example demonstrates how to write a simple ingester, which generates
// and writes some entries to Gravwell
func main() {
	// Configure the ingester
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: targets,
		Tags:         tags,
		Auth:         secret,
		PublicKey:    ``,
		PrivateKey:   ``,
		LogLevel:     "WARN",
	}

	// Start the ingester
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		log.Fatalf("Failed build our ingest system: %v\n", err)
	}
	defer igst.Close()
	if err := igst.Start(); err != nil {
		log.Fatalf("Failed start our ingest system: %v\n", err)
	}

	// Wait for connection to indexers
	if err := igst.WaitForHot(0); err != nil {
		log.Fatalf("Timedout waiting for backend connections: %v\n", err)
	}

	// Generate and send some entries
	tag, err := igst.GetTag("test")
	if err != nil {
		log.Fatalf("Failed to get tag: %v", err)
	}
	var src net.IP
	if src, err = igst.SourceIP(); err != nil {
		log.Fatalf("failed to get source IP: %v", err)
	}
	for i := 0; i < 100; i++ {
		e := &entry.Entry{
			TS:   entry.Now(),
			SRC:  src,
			Tag:  tag,
			Data: []byte(fmt.Sprintf("test entry %d", i)),
		}
		if err := igst.WriteEntry(e); err != nil {
			log.Printf("Failed to write entry: %v", err)
			break
		}
	}

	// Now shut down
	if err := igst.Sync(time.Second); err != nil {
		log.Printf("Failed to sync: %v", err)
	}
	igst.Close()
}
```
