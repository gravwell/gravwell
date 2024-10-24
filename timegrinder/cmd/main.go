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
	"os"
	"regexp"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/timegrinder"
)

var (
	cName   = flag.String("custom-format-name", "", "Name for a custom format")
	cRegex  = flag.String("custom-format-regex", "", "Extraction regular expression for custom format")
	cFormat = flag.String("custom-format", "", "Parse format for custom format")
)

func main() {
	var custActive bool
	var cust timegrinder.CustomFormat
	flag.Parse()
	if *cFormat != `` {
		if *cRegex == `` {
			log.Fatalf("missing custom-format-regex for %s", *cFormat)
		} else if *cName == `` {
			log.Fatalf("missing custom-format-name for %s", *cFormat)
		} else if _, err := regexp.Compile(*cRegex); err != nil {
			log.Fatalf("Failed to parse regex %q %v", *cRegex, err)
		}
		cust = timegrinder.CustomFormat{
			Name:   *cName,
			Regex:  *cRegex,
			Format: *cFormat,
		}
	}

	cfg := timegrinder.Config{
		EnableLeftMostSeed: true,
	}
	tg, err := timegrinder.New(cfg)
	if err != nil {
		log.Fatal("failed to create new timegrinder", err)
	}
	if custActive {
		if p, err := timegrinder.NewCustomProcessor(cust); err != nil {
			log.Fatal("failed to create custom processor", cust.Name, err)
		} else if _, err := tg.AddProcessor(p); err != nil {
			log.Fatal("failed to add custom processor", cust.Name, err)
		}
	}

	if len(os.Args) == 0 {
		log.Fatal("not values to test")
	}
	for _, arg := range os.Args[1:] {
		ts, offset, name, err := tg.DebugExtract([]byte(arg))
		if err != nil {
			fmt.Printf("Extraction error %q - %v\n", arg, err)
		} else if offset < 0 {
			fmt.Printf("Failed to extract on %q\n", arg)
		} else {
			fmt.Printf("%q - %d -> %v\tvia %s\n", arg, offset, ts, name)
		}
	}
}
