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
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	dataType      = flag.String("type", "", "Data type to generate (json, csv, etc.), call `-type ?` for usage")
	delimOverride = flag.String("fields-delim-override", "", "Override the delimiter (for fields data type)")
	randomSrc     = flag.Bool("random-source", false, "Generate random source values")

	dataTypes = map[string]base.DataGen{
		"binary":   genDataBinary,
		"bind":     genDataBind,
		"csv":      genDataCSV,
		"dnsmasq":  genDataDnsmasq,
		"fields":   genDataFields,
		"json":     genDataJSON,
		"xml":      genDataXML,
		"regex":    genDataRegex,
		"syslog":   genDataSyslog,
		"zeekconn": genDataZeekConn,
		"evs":      genDataEnumeratedValue,
		"megajson": genDataMegaJSON,
	}
	finalizers = map[string]base.Finalizer{
		"evs":      finEnumeratedValue,
		"binary":   fin("binary"),
		"bind":     fin("bind"),
		"csv":      fin("csv"),
		"dnsmasq":  fin("dnsmasq"),
		"fields":   fin("fields"),
		"json":     fin("JSON"),
		"xml":      fin("XML"),
		"regex":    fin("regex"),
		"syslog":   fin("syslog"),
		"zeekconn": fin("zeek conn"),
		"megajson": fin("mega JSON"),
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

	var tag entry.EntryTag
	if igst, src, err = base.NewIngestMuxer(`unifiedgenerator`, `00000000-0000-0000-0000-000000000001`, cfg, time.Second); err != nil {
		log.Fatal(err)
	} else if tag, err = igst.GetTag(cfg.Tag); err != nil {
		log.Fatalf("Failed to lookup tag %s: %v\n", cfg.Tag, err)
	}
	var start time.Time
	if cfg.Count > 0 {
		start = time.Now()
		seedUsers(int(cfg.Count), 256)

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

func fin(val string) base.Finalizer {
	return func(ent *entry.Entry) {
		if val != `` {
			ent.AddEnumeratedValueEx("_type", val)
		}
		if *randomSrc {
			ent.SRC = getIP()
		}
	}
}
