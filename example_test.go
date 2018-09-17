/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gravwell/filewatch"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

var (
	tags    = []string{"syslog"}
	targets = []string{"tcp://127.0.0.1:4023"}
	secret  = "IngestSecrets" // set this to the actual ingest secret
)

// Example demonstrates how to write a simple ingester, which watches
// for entries in /var/log/syslog and sends them to Gravwell.
func Example() {
	// First, set up the filewatcher.
	statefile, err := ioutil.TempFile("", "ingest-example")
	if err != nil {
		log.Fatal(err)
	}
	defer statefile.Close()
	wtcher, err := filewatch.NewWatcher(statefile.Name())
	if err != nil {
		log.Fatalf("Failed to create notification watcher: %v\n", err)
	}

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
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}

	// pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtcher.SetLogger(igst)

	// Wait for connection to indexers
	if err := igst.WaitForHot(0); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	// If we made it this far, we connected to an indexer.
	// Create a channel into which we'll push entries
	ch := make(chan *entry.Entry, 2048)

	// Create a handler to watch /var/log/syslog
	// First, create a log handler. This will emit Entries tagged 'syslog'
	tag, err := igst.GetTag("syslog")
	if err != nil {
		log.Fatalf("Failed to get tag: %v", err)
	}
	lhconf := filewatch.LogHandlerConfig{
		Tag:            tag,
		IgnoreTS:       true,
		AssumeLocalTZ:  true,
		IgnorePrefixes: [][]byte{},
	}
	lh, err := filewatch.NewLogHandler(lhconf, ch)
	if err != nil {
		log.Fatalf("Failed to generate handler: %v", err)
	}

	// Create a watcher usng the log handler we just created, watching /var/log/syslog*
	c := filewatch.WatchConfig{
		ConfigName: "syslog",
		BaseDir:    "/var/log",
		FileFilter: "{syslog,syslog.[0-9]}",
		Hnd:        lh,
	}
	if err := wtcher.Add(c); err != nil {
		wtcher.Close()
		log.Fatalf("Failed to add watch directory: %v", err)
	}

	// Start the watcher
	if err := wtcher.Start(); err != nil {
		wtcher.Close()
		igst.Close()
		log.Fatalf("Failed to start file watcher: %v", err)
	}

	// fire off our relay
	doneChan := make(chan error, 1)
	go relay(ch, doneChan, igst)

	// Our relay is now running, ingesting log entries and sending them upstream

	// listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	<-sch
	if err := wtcher.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close file follower: %v\n", err)
	}
	close(ch) //to inform the relay that no new entries are going to come down the pipe

	//wait for our ingest relay to exit
	<-doneChan
	if err := igst.Sync(time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	igst.Close()
}

func relay(ch chan *entry.Entry, done chan error, igst *ingest.IngestMuxer) {
	// Read any entries coming down the channel, write them to the ingester
	for e := range ch {
		if err := igst.WriteEntry(e); err != nil {
			fmt.Println("Failed to write entry", err)
		}
	}
	done <- nil
}
