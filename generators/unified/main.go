/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/generators/ipgen"
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	dataType = flag.String("type", "", "Data type to generate (json, csv, etc.), call `-type ?` for usage")

	dataTypes = map[string]base.DataGen{
		"json":   genDataJSON,
		"binary": genDataBinary,
	}

	v4gen *ipgen.V4Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(40)
	if err != nil {
		log.Fatalf("Failed to instantiate v4 generator: %v", err)
	}
}

func main() {
	flag.Parse()

	// validate the type they asked for
	gen, ok := dataTypes[*dataType]
	if !ok {
		fmt.Fprintf(os.Stderr, "Invalid -type %v. Valid choices:\n", *dataType)
		for k, _ := range dataTypes {
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
	seedUsers(int(cfg.Count), 256)
	if igst, src, err = base.NewIngestMuxer(`unifiedgenerator`, ``, cfg, time.Second); err != nil {
		log.Fatal(err)
	}
	tag, err := igst.GetTag(cfg.Tag)
	if err != nil {
		log.Fatalf("Failed to lookup tag %s: %v", cfg.Tag, err)
	}
	start := time.Now()

	if !cfg.Streaming {
		if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg, gen); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg, gen); err != nil {
			log.Fatal("Failed to stream entries ", err)
		}
	}

	if err = igst.Sync(time.Second); err != nil {
		log.Fatal("Failed to sync ingest muxer ", err)
	}

	if err = igst.Close(); err != nil {
		log.Fatal("Failed to close ingest muxer ", err)
	}

	durr := time.Since(start)
	if err == nil {
		fmt.Printf("Completed in %v (%s)\n", durr, ingest.HumanSize(totalBytes))
		fmt.Printf("Total Count: %s\n", ingest.HumanCount(totalCount))
		fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(totalCount, durr))
		fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, durr))
	}
}
