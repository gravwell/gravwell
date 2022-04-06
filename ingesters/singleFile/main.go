/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime/debug"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/args"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

var (
	tso         = flag.String("timestamp-override", "", "Timestamp override")
	tzo         = flag.String("timezone-override", "", "Timezone override e.g. America/Chicago")
	inFile      = flag.String("i", "", "Input file to process (specify - for stdin)")
	ver         = flag.Bool("version", false, "Print version and exit")
	utc         = flag.Bool("utc", false, "Assume UTC time")
	ignoreTS    = flag.Bool("ignore-ts", false, "Ignore timetamp")
	ignorePfx   = flag.String("ignore-prefix", "", "Ignore lines that start with the prefix")
	verbose     = flag.Bool("verbose", false, "Print every step")
	quotable    = flag.Bool("quotable-lines", false, "Allow lines to contain quoted newlines")
	cleanQuotes = flag.Bool("clean-quotes", false, "clean quotes off lines")
	blockSize   = flag.Int("block-size", 0, "Optimized ingest using blocks, 0 disables")
	status      = flag.Bool("status", false, "Output ingest rate stats as we go")
	srcOvr      = flag.String("source-override", "", "Override source with address, hash, or integeter")

	count            uint64
	totalBytes       uint64
	dur              time.Duration
	noTg             bool //no timegrinder
	bsize            int
	ignorePrefixFlag bool
	ignorePrefix     []byte
	srcOverride      net.IP
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *ignoreTS {
		noTg = true
	}
	if *ignorePfx != `` {
		ignorePrefix = []byte(*ignorePfx)
		ignorePrefixFlag = len(ignorePrefix) > 0
	}
	if *blockSize > 0 {
		bsize = *blockSize
	}
}

func main() {
	debug.SetTraceback("all")
	if *inFile == "" {
		log.Fatal("Input file path required")
	}
	a, err := args.Parse()
	if err != nil {
		log.Fatalf("Invalid arguments: %v\n", err)
	}
	if len(a.Tags) != 1 {
		log.Fatal("File oneshot only accepts a single tag")
	}

	//resolve the timestmap override if there is one
	if *tso != "" {
		if err = timegrinder.ValidateFormatOverride(*tso); err != nil {
			log.Fatalf("Invalid timestamp override: %v\n", err)
		}
	}

	if *srcOvr != `` {
		if srcOverride, err = config.ParseSource(*srcOvr); err != nil {
			log.Fatal("Invalid source override")
		}
	}
	var tg *timegrinder.TimeGrinder
	if !noTg {
		//build a new timegrinder
		c := timegrinder.Config{
			EnableLeftMostSeed: true,
			FormatOverride:     *tso,
		}

		if tg, err = timegrinder.NewTimeGrinder(c); err != nil {
			log.Fatalf("failed to build timegrinder: %v\n", err)
		}
		if *utc {
			tg.SetUTC()
		}

		if *tzo != `` {
			if err = tg.SetTimezone(*tzo); err != nil {
				log.Fatalf("Failed to set timegrinder timezeon: %v\n", err)
			}
		}
	}

	//get a handle on the input file with a wrapped decompressor if needed
	var fin io.ReadCloser
	if *inFile == "-" {
		fin = os.Stdin
	} else {
		fin, err = utils.OpenBufferedFileReader(*inFile, 8192)
		if err != nil {
			log.Fatalf("Failed to open %s: %v\n", *inFile, err)
		}
	}

	//fire up a uniform muxer
	igst, err := ingest.NewUniformIngestMuxer(a.Conns, a.Tags, a.IngestSecret, a.TLSPublicKey, a.TLSPrivateKey, "")
	if err != nil {
		log.Fatalf("Failed to create new ingest muxer: %v\n", err)
	}
	if err := igst.Start(); err != nil {
		log.Fatalf("Failed to start ingest muxer: %v\n", err)
	}
	if err := igst.WaitForHot(a.Timeout); err != nil {
		log.Fatalf("Failed to wait for hot connection: %v\n", err)
	}
	tag, err := igst.GetTag(a.Tags[0])
	if err != nil {
		log.Fatalf("Failed to resolve tag %s: %v\n", a.Tags[0], err)
	}

	src := srcOverride
	if src == nil {
		src, _ = igst.SourceIP()
	}

	//go ingest the file
	if err := doIngest(fin, igst, tag, tg, src); err != nil {
		log.Fatalf("Failed to ingest file: %v\n", err)
	}

	if err = igst.Sync(a.Timeout); err != nil {
		log.Fatalf("Failed to sync ingest muxer: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		log.Fatalf("Failed to close the ingest muxer: %v\n", err)
	}
	if err := fin.Close(); err != nil {
		log.Fatalf("Failed to close the input file: %v\n", err)
	}
	fmt.Printf("Completed in %v (%s)\n", dur, ingest.HumanSize(totalBytes))
	fmt.Printf("Total Count: %s\n", ingest.HumanCount(count))
	fmt.Printf("Entry Rate: %s\n", ingest.HumanEntryRate(count, dur))
	fmt.Printf("Ingest Rate: %s\n", ingest.HumanRate(totalBytes, dur))
}

func doIngest(fin io.Reader, igst *ingest.IngestMuxer, tag entry.EntryTag, tg *timegrinder.TimeGrinder, src net.IP) (err error) {
	var ignore [][]byte
	if ignorePrefixFlag {
		ignore = [][]byte{ignorePrefix}
	}
	cfg := utils.LineDelimitedStream{
		Rdr:            fin,
		Proc:           processors.NewProcessorSet(igst),
		Tag:            tag,
		SRC:            src,
		TG:             tg,
		IgnorePrefixes: ignore,
		CleanQuotes:    *cleanQuotes,
		BatchSize:      *blockSize,
		Verbose:        *verbose,
		Quotable:       *quotable,
	}
	//if not doing regular updates, just fire it off
	if !*status {
		c, b, err := utils.IngestLineDelimitedStream(cfg)
		count += c
		totalBytes += b
		return err
	}

	errCh := make(chan error, 1)
	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()
	go func(ch chan error) {
		c, b, err := utils.IngestLineDelimitedStream(cfg)
		count += c
		totalBytes += b
		ch <- err
	}(errCh)

loop:
	for {
		lastts := time.Now()
		lastcnt := count
		lastsz := totalBytes
		select {
		case err = <-errCh:
			fmt.Println("\nDONE")
			break loop
		case _ = <-tckr.C:
			dur := time.Since(lastts)
			cnt := count - lastcnt
			bts := totalBytes - lastsz
			fmt.Printf("\r%s %s                                     ",
				ingest.HumanEntryRate(cnt, dur),
				ingest.HumanRate(bts, dur))
		}
	}
	return
}
