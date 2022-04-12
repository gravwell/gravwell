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
	"time"

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	enableIPv6 = flag.Bool("enable-v6", false, "Enable IPv6 in generated entries")
	recordType = flag.String("type", "conn", "Type of bro log to generate")

	genData  base.DataGen
	broTypes = map[string]base.DataGen{
		"conn": genConnData,
	}
)

func main() {
	var igst base.GeneratorConn
	var totalBytes uint64
	var totalCount uint64
	var src net.IP
	var ok bool
	cfg, err := base.GetGeneratorConfig(`zeek`)
	if err != nil {
		log.Fatal(err)
	}

	if genData, ok = broTypes[*recordType]; !ok {
		msg := fmt.Sprintf("Invalid bro log type %v. Supported types:\n", *recordType)
		for t, _ := range broTypes {
			msg += fmt.Sprintf("\t%s\n", t)
		}
		log.Fatal(msg)
	}

	if igst, src, err = base.NewIngestMuxer(`zeekgenerator`, ``, cfg, time.Second); err != nil {
		log.Fatal(err)
	}
	tag, err := igst.GetTag(cfg.Tag)
	if err != nil {
		log.Fatalf("Failed to lookup tag %s: %v", cfg.Tag, err)
	}
	start := time.Now()

	if !cfg.Streaming {
		if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg, genData); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg, genData); err != nil {
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
