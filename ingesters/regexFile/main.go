/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingesters/args"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	initBuffSize = 4 * 1024 * 1024
	maxBuffSize  = 128 * 1024 * 1024
)

var (
	tso          = flag.String("timestamp-override", "", "Timestamp override")
	tzo          = flag.String("timezone-override", "", "Timezone override e.g. America/Chicago")
	inFile       = flag.String("i", "", "Input file to process (specify - for stdin)")
	ver          = flag.Bool("version", false, "Print version and exit")
	utc          = flag.Bool("utc", false, "Assume UTC time")
	verbose      = flag.Bool("verbose", false, "Print every step")
	rexStr       = flag.String("rexp", "", "Regular expression string to perform entry breaks on")
	igTs         = flag.Bool("ignore-ts", false, "Ignore the timestamp")
	custTsRegex  = flag.String("cust-ts-regex", "", "Regular expression for custom timestamp")
	custTsFormat = flag.String("cust-ts-format", "", "Date format for custom timestamp")

	count      uint64
	totalBytes uint64
	dur        time.Duration
	re         *regexp.Regexp
	nlBytes    = "\n"
	ignoreTS   bool
	custTs     timegrinder.CustomFormat
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *rexStr == `` {
		log.Fatal("Regular expression string required")
	}
	var err error
	if re, err = regexp.Compile(*rexStr); err != nil {
		log.Fatal("Bad regular expression", err)
	}
	ignoreTS = *igTs
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

	if *custTsRegex != "" || *custTsFormat != "" {
		custTs = timegrinder.CustomFormat{
			Name:   "custom",
			Regex:  *custTsRegex,
			Format: *custTsFormat,
		}
		if err = custTs.Validate(); err != nil {
			log.Fatalf("Invalid custom timestamp formats: %v", err)
		}
	}

	//get a handle on the input file with a wrapped decompressor if needed
	var fin io.ReadCloser
	if *inFile == "-" {
		fin = os.Stdin
	} else {
		fin, err = OpenFileReader(*inFile)
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

	//go ingest the file
	if err := ingestFile(fin, igst, tag, *tso); err != nil {
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

func ingestFile(fin io.Reader, igst *ingest.IngestMuxer, tag entry.EntryTag, tso string) error {
	var bts []byte
	var ts time.Time
	var ok bool
	var tg *timegrinder.TimeGrinder

	if !ignoreTS {
		//build a new timegrinder
		c := timegrinder.Config{
			EnableLeftMostSeed: true,
			FormatOverride:     tso,
		}
		var err error
		if tg, err = timegrinder.NewTimeGrinder(c); err != nil {
			return err
		}
		if *utc {
			tg.SetUTC()
		}

		if *tzo != `` {
			if err = tg.SetTimezone(*tzo); err != nil {
				return err
			}
		}
		// If custTs has been set, include it
		if custTs.Name != "" {
			var p timegrinder.Processor
			if p, err = timegrinder.NewCustomProcessor(custTs); err != nil {
				return err
			} else if _, err = tg.AddProcessor(p); err != nil {
				return err
			}
		}
	}

	src, err := igst.SourceIP()
	if err != nil {
		return err
	}

	scn := bufio.NewScanner(fin)
	scn.Split(regexSplitter)
	scn.Buffer(make([]byte, initBuffSize), maxBuffSize)

	start := time.Now()
	for scn.Scan() {
		if bts = bytes.Trim(scn.Bytes(), nlBytes); len(bts) == 0 {
			continue
		}
		if ignoreTS {
			ts = time.Now()
		} else {
			if ts, ok, err = tg.Extract(bts); err != nil {
				return err
			} else if !ok {
				ts = time.Now()
			}
		}
		ent := &entry.Entry{
			TS:  entry.FromStandard(ts),
			Tag: tag,
			SRC: src,
		}
		ent.Data = append(ent.Data, bts...) //force reallocation due to the scanner
		if err = igst.WriteEntry(ent); err != nil {
			return err
		}
		if *verbose {
			fmt.Println(ent.TS, ent.Tag, ent.SRC, string(ent.Data))
		}
		count++
		totalBytes += uint64(len(ent.Data))
	}
	dur = time.Since(start)
	return scn.Err()
}

func regexSplitter(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if idx := getREIdx(data); idx > 0 {
		return idx, data[0:idx], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	//request more data
	return 0, nil, nil
}

func getREIdx(data []byte) (r int) {
	r = -1
	//attempt to get the index of our regexp
	idxs := re.FindIndex(data)
	if idxs == nil || len(idxs) != 2 {
		return
	}
	if idxs[0] > 0 {
		r = idxs[0]
	} else {
		if idxs2 := re.FindIndex(data[idxs[1]:]); len(idxs2) != 2 {
			return
		} else {
			r = idxs[1] + idxs2[0]
		}
	}

	return
}
