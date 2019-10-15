/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gravwell/ingest/v3"
	"golang.org/x/sys/windows/svc/eventlog"
)

var (
	srcName    = flag.String("source-name", "GravwellEventGenerator", "Source name for generated events")
	eventCount = flag.Int("event-count", 100, "Number of events to generate")
	streaming  = flag.Bool("stream", false, "Stream events in")
	count      uint64
	totalBytes uint64
	totalCount uint64
)

func init() {
	flag.Parse()
	if *srcName == "" {
		log.Fatal("A source name must be specified\n")
	}
	if *eventCount <= 0 {
		log.Fatal("invalid event count")
	}
	count = uint64(*eventCount)
}

func main() {
	var err error
	start := time.Now()

	wlog, err := eventlog.Open(*srcName)
	if err != nil {
		log.Fatalf("Couldn't open event log handle: %v", err)
	}

	if !*streaming {
		if err = throw(wlog, count); err != nil {
			log.Fatal("Failed to throw events ", err)
		}
	} else {
		var stop bool
		r := make(chan error, 1)
		go func(ret chan error, stp *bool) {
			ret <- stream(wlog, count, stp)
		}(r, &stop)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		select {
		case _ = <-c:
			stop = true
			select {
			case err = <-r:
			case _ = <-time.After(3 * time.Second):
				err = errors.New("Timed out waiting for exit")
			}
		case err = <-r:
		}
		if err != nil {
			log.Fatal("Failed to stream events ", err)
		}
	}

	durr := time.Since(start)
	if err == nil {
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(totalBytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(totalCount))
		fmt.Printf("Event Rate: %s\n", ingest.HumanEntryRate(totalCount, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, durr))
	}
}
