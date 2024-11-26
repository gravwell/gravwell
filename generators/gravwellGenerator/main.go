/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/generators/base"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	dataType      = flag.String("type", "", "Data type to generate (json, csv, etc.), call `-type ?` for usage")
	delimOverride = flag.String("fields-delim-override", "", "Override the delimiter (for fields data type)")
	randomSrc     = flag.Bool("random-source", false, "Generate random source values")

	// for fields
	delim string = "\t"
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if *delimOverride != `` {
		delim = *delimOverride
	}

	// validate the type they asked for, a generator MUST be configured
	gen, fin, ok := getGenerator(*dataType)
	if !ok {
		fmt.Fprintf(os.Stderr, "Invalid -type %v. Valid choices:\n", *dataType)
		for _, k := range getList() {
			fmt.Fprintf(os.Stderr, "	%v\n", k)
		}
		log.Fatal("Must provide valid type argument")
	}
	var igst base.GeneratorConn
	var totalBytes uint64
	var totalCount uint64
	var src net.IP
	cfg, err := base.GetGeneratorConfig(*dataType)
	if err != nil {
		log.Fatal(err)
	}

	var tag entry.EntryTag
	if igst, src, err = base.NewIngestMuxer(`unifiedgenerator`, `00000000-0000-0000-0000-000000000001`, cfg, time.Second); err != nil {
		log.Fatal(err)
	} else if tag, err = igst.GetTag(cfg.Tag); err != nil {
		log.Fatalf("Failed to lookup tag %s: %v\n", cfg.Tag, err)
	}
	var start time.Time
	if cfg.Count > 0 {
		start = time.Now()
		seedVars(int(cfg.Count))

		if !cfg.Streaming {
			if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg, gen, fin); err != nil {
				log.Fatal("Failed to throw entries ", err)
			}
		} else {
			if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg, gen, fin); err != nil {
				log.Fatal("Failed to stream entries ", err)
			}
		}
	} else {
		log.Println("Connection successful")
	}

	if err = igst.Sync(time.Second); err != nil {
		log.Fatal("Failed to sync ingest muxer ", err)
	}

	if err = igst.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}

	if err == nil && cfg.Count > 0 {
		durr := time.Since(start)
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(totalBytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(totalCount))
		fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(totalCount, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, durr))
	}
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, "Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(out, "\nRandom Data Pool Overrides via environment variables:\n")
	fmt.Fprintf(out, "\tUSER_COUNT\n")
	fmt.Fprintf(out, "\tGROUP_COUNT\n")
	fmt.Fprintf(out, "\tHOST_COUNT\n")
	fmt.Fprintf(out, "\tAPP_COUNT\n")
}
