# The Gravwell Ingest API

API documentation: [godoc.org/github.com/gravwell/ingest](https://godoc.org/github.com/gravwell/ingest)

This package provides methods to build ingesters for the Gravwell analytics platform. Ingesters take raw data from a particular source (pcap, log files, a camera, etc.), bundle it into Entries, and ship the Entries up to the Gravwell indexers.

An Entry (defined in the sub-package [github.com/gravwell/ingest/entry](https://godoc.org/github.com/gravwell/ingest/entry)) looks like this:

	type Entry struct {
	    TS   Timestamp
	    SRC  net.IP
	    Tag  EntryTag
	    Data []byte
	}

The most important element is Data; this is simply a discrete piece of information you want to store as an entry, be it binary or text. The Tag field associates the entry with a specific tag in the indexer, making it easier to search later. The timestamp and source IP give additional information about the entry.

There are two ways to actually get Entries into Gravwell: the IngestConnection and the IngestMuxer. An IngestConnection is a connection to a single destination, while an IngestMuxer can connect to multiple destinations simultaneously to improve ingestion rate.

The example below shows the basics of getting Entries into Gravwell using an IngestConnection (the simpler method). See `example_test.go` for a more detailed (and functional) example using an IngestMuxer, or see [github.com/gravwell/ingesters](https://github.com/gravwell/ingesters) for open-source real-world examples.

	package main
	
	import (
		"github.com/gravwell/ingest"
		"github.com/gravwell/ingest/entry"
		"log"
		"net"
	)
	
	func main() {
		// Get an IngestConnection to localhost, using the shared secret "IngestSecrets"
		// and specifying that we'll be using the tag "test-tag"
		igst, err := ingest.InitializeConnection("tcp://127.0.0.1:4023", "IngestSecrets",[]string{"testtag"}, "", "", false)
		if err != nil {
			log.Fatalf("Couldn't open connection to ingester: %v", err)
		}
		defer igst.Close()
	
		// We need to get the numeric value for the tag we're using
		tagid, ok := igst.GetTag("testtag")
		if !ok {
			log.Fatal("couldn't look up tag")
		}
	
		// Now we'll create an Entry
		ent := entry.Entry{
			TS: entry.Now(),
			SRC: net.ParseIP("127.0.0.1"),
			Tag: tagid,
			Data: []byte("This is my test data!"),
		}
	
		// And finally write the Entry
		igst.WriteEntry(&ent)
	}
