/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	tagName      = flag.String("tag-name", "hackernews", "Tag name for ingested data")
	pipeConns    = flag.String("pipe-conn", "", "Path to pipe connection")
	clearConns   = flag.String("clear-conns", "", "comma seperated server:port list of cleartext targets")
	ingestSecret = flag.String("ingest-secret", "IngestSecrets", "Ingest key")
	verbose      = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver          = flag.Bool("version", false, "Print the version information and exit")
)

type HNStream struct {
	streamName string
	tag        entry.EntryTag
	eChan      chan *entry.Entry
	die        chan bool
}

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
}

func main() {
	tags := []string{*tagName}

	var connSet []string
	if *clearConns != "" {
		for _, conn := range strings.Split(*clearConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("tcp://%s", conn))
			}
		}
	}
	if *pipeConns != "" {
		for _, conn := range strings.Split(*pipeConns, ",") {
			conn = strings.TrimSpace(conn)
			if len(conn) > 0 {
				connSet = append(connSet, fmt.Sprintf("pipe://%s", conn))
			}
		}
	}
	if len(connSet) <= 0 {
		log.Fatal("No connections were specified\nWe need at least one\n")
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(connSet))

	//fire up the ingesters
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: connSet,
		Tags:         tags,
		Auth:         *ingestSecret,
		LogLevel:     "INFO",
		IngesterName: "hackernews",
	}

	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(2 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")

	igsttag, err := igst.GetTag(*tagName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't lookup tag: %v\n", *tagName)
		return
	}

	streamnames := []string{"news", "comments"}
	var streams []*HNStream
	for _, n := range streamnames {
		streams = append(streams, &HNStream{
			streamName: n,
			tag:        igsttag,
			eChan:      make(chan *entry.Entry, 2048),
			die:        make(chan bool, 1),
		})
	}

	for _, stream := range streams {
		go stream.streamReader()
		go stream.hnIngester(igst)
	}
	utils.WaitForQuit()

	debugout("exiting\n")
}

func (stream *HNStream) streamReader() {
	defer close(stream.eChan)
outerLoop:
	for {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(30*time.Second, func() {
			cancel()
		})

		url := fmt.Sprintf("http://api.hnstream.com/%s/stream", stream.streamName)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("can't create HTTP request: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		req = req.WithContext(ctx)

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("can't execute HTTP request: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		defer res.Body.Close()
		buf := bufio.NewReader(res.Body)
		for {
			select {
			case <-stream.die:
				debugout("Got die message, bailing out.\n")
				return
			default:
				timer.Reset(30 * time.Second)
				bytes, err := buf.ReadBytes('\n')
				if err != nil {
					continue outerLoop
				}
				if strings.HasPrefix(string(bytes), "[opened]") {
					continue
				}
				stream.eChan <- &entry.Entry{
					TS:   entry.Now(),
					SRC:  nil,
					Tag:  stream.tag,
					Data: bytes,
				}
			}
		}
	}
	return
}

// hnIngester pulls individual JSON records from a HNStream and ingests them.
func (stream *HNStream) hnIngester(igst *ingest.IngestMuxer) {
	for e := range stream.eChan {
		if err := igst.WriteEntry(e); err != nil {
			log.Printf("failed to write entry %v", err)
		}
	}
}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}
