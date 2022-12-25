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
	"github.com/gravwell/gravwell/v3/ingest"
)

var (
	dataType      = flag.String("type", "", "Data type to generate (json, csv, etc.), call `-type ?` for usage")
	delimOverride = flag.String("fields-delim-override", "", "Override the delimiter (for fields data type)")

	dataTypes = map[string]base.DataGen{
		"binary":   genDataBinary,
		"bind":     genDataBind,
		"csv":      genDataCSV,
		"dnsmasq":  genDataDnsmasq,
		"fields":   genDataFields,
		"json":     genDataJSON,
		"regex":    genDataRegex,
		"syslog":   genDataSyslog,
		"zeekconn": genDataZeekConn,
		"evs":      genDataEnumeratedValue,
	}
	finalizers = map[string]base.Finalizer{
		"evs": finEnumeratedValue,
	}

	// for fields
	delim string = "\t"
)

func main() {
	flag.Parse()
	if *delimOverride != `` {
		delim = *delimOverride
	}

	// validate the type they asked for, a generator MUST be configured
	gen, ok := dataTypes[*dataType]
	if !ok {
		fmt.Fprintf(os.Stderr, "Invalid -type %v. Valid choices:\n", *dataType)
		for k, _ := range dataTypes {
			fmt.Fprintf(os.Stderr, "	%v\n", k)
		}
		log.Fatal("Must provide valid type argument")
	}
	//its ok if there is no finalizer
	fin := finalizers[*dataType]

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
		if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg, gen, fin); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg, gen, fin); err != nil {
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
