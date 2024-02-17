/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
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
	"time"

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

var (
	custFormatPath = flag.String("custom", "", "Path to custom time format configuration file")
	lms            = flag.Bool("enable-left-most-seed", false, "Activate EnableLeftMostSeed config option")
	fo             = flag.String("format-override", "", "Enable FormatOverride config option")
	lt             = flag.Bool("assume-local-timezone", true, "Assume local timezone on timegrinder")
)

type customFormats struct {
	TimeFormat config.CustomTimeFormat
}

func main() {
	flag.Parse()

	cfg := timegrinder.Config{
		EnableLeftMostSeed: *lms,
	}
	tg, err := timegrinder.New(cfg)
	if err != nil {
		log.Fatalf("Failed to build timegrinder: %v\n", err)
	}
	if *lt {
		tg.SetLocalTime()
	} else {
		tg.SetUTC()
	}
	if *custFormatPath != `` {
		var cf customFormats
		if err := config.LoadConfigFile(&cf, *custFormatPath); err != nil {
			log.Fatalf("Failed to load custom format configs from %q: %v\n", *custFormatPath, err)
		}
		for k, v := range cf.TimeFormat {
			if v == nil {
				continue
			}
			cf := timegrinder.CustomFormat{
				Name:   k,
				Regex:  v.Regex,
				Format: v.Format,
			}
			if cp, err := timegrinder.NewCustomProcessor(cf); err != nil {
				log.Fatalf("Invalid custom format %q: %v\n", k, err)
			} else if _, err := tg.AddProcessor(cp); err != nil {
				log.Fatalf("Failed to load custom time format %q: %v\n", k, err)
			}
		}
	}
	if *fo != `` {
		if err := tg.SetFormatOverride(*fo); err != nil {
			log.Fatalf("Failed to set timestamp format override to %q: %v\n", *fo, err)
		}
	}
	for _, arg := range flag.Args() {
		if ts, name, start, end, ok := tg.DebugMatch([]byte(arg)); !ok {
			outputNoMatch(arg)
		} else {
			outputMatch(arg, name, ts, start, end)
		}

	}
}

func outputNoMatch(val string) {
	fmt.Printf("%sNo Match%s\t%s\n", Red, Reset, val)
}

func outputMatch(val, name string, ts time.Time, start, end int) {
	if end < start || end >= len(val) || start >= len(val) {
		//something is wonky, just do a raw output
		fmt.Printf("%q\t%v\t%s\n", val, ts, name)
		return
	}
	pre := val[0:start]
	mtch := val[start:end]
	post := val[end:]
	fmt.Printf("%s%s%s%s%s\n", pre, Green, mtch, Reset, post)     //print the log with the highlight
	fmt.Printf("\t%s%v\t%s%s%s\n", Blue, ts, Yellow, name, Reset) //print the extraction info
}

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
)
