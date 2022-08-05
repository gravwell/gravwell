/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"math"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/gravwell/gravwell/v3/generators/base"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

var (
	dcurv = flag.Bool("daily-curve", false, "distribute the values along a curve, implies streaming with a count representing the peak")
)

func main() {
	var igst base.GeneratorConn
	var totalBytes uint64
	var totalCount uint64
	var src net.IP
	cfg, err := base.GetGeneratorConfig(`dnsmasq`)
	if err != nil {
		log.Fatal(err)
	}
	if igst, src, err = base.NewIngestMuxer(`dnsmasq`, ``, cfg, time.Second); err != nil {
		log.Fatal(err)
	}
	tag, err := igst.GetTag(cfg.Tag)
	if err != nil {
		log.Fatalf("Failed to lookup tag %s: %v", cfg.Tag, err)
	}
	start := time.Now()
	if *dcurv {
		if totalCount, totalBytes, err = streamingCurve(igst, tag, src, cfg); err != nil {
			log.Fatal("Failed to throw entries ", err)
		}
	} else {
		if !cfg.Streaming {
			if totalCount, totalBytes, err = base.OneShot(igst, tag, src, cfg, genData); err != nil {
				log.Fatal("Failed to throw entries ", err)
			}
		} else {
			if totalCount, totalBytes, err = base.Stream(igst, tag, src, cfg, genData); err != nil {
				log.Fatal("Failed to stream entries ", err)
			}
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

type spanner struct {
	count uint64
}

func newSpanner(cfg base.GeneratorConfig) spanner {
	return spanner{
		count: cfg.Count,
	}
}

const (
	secsInADay = 3600 * 24
)

func (sp spanner) scale(ts time.Time) float64 {
	//get the second in the day
	sec := (ts.Second() + ts.Hour()*3600)

	// now park the second in a day on the sine wave between 0 and 1.0
	return math.Sin((float64(sec) / float64(secsInADay)) * math.Pi)
}

func streamingCurve(conn base.GeneratorConn, tag entry.EntryTag, src net.IP, cfg base.GeneratorConfig) (totalCount, totalBytes uint64, err error) {
	sp := newSpanner(cfg)
	var stop bool
	r := make(chan error, 1)
	go func(ret chan error, stp *bool) {
		var err error
		totalCount, totalBytes, err = streamRunner(conn, tag, src, cfg.Count, stp, sp)
		r <- err
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
	return
}

func streamRunner(conn base.GeneratorConn, tag entry.EntryTag, src net.IP, cnt uint64, stop *bool, sp spanner) (totalCount, totalBytes uint64, err error) {
	if conn == nil || stop == nil {
		err = errors.New("invalid parameters")
		return
	}
	if src == nil {
		if src, err = conn.SourceIP(); err != nil {
			err = fmt.Errorf("Failed to get source ip: %w", err)
			return
		}
	}
	var ent *entry.Entry
loop:
	for !*stop {
		ts := time.Now()
		start := ts
		//figure out how many we should make in this second
		sc := sp.scale(ts)
		scnt := sc * float64(cnt)
		if scnt == 0 {
			time.Sleep(time.Second) // try the next one
			continue
		} else if scnt < 1 {
			//figure out how many seconds to span per entry and sleep
			time.Sleep(time.Duration(float64(time.Second) / scnt))
			//emit the entry and continue
			ent = &entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				SRC:  src,
				Data: genData(ts),
			}
			if err = conn.WriteEntry(ent); err != nil {
				break loop
			}
			totalBytes += uint64(len(ent.Data))
			totalCount++
			continue
		}

		diff := time.Second / time.Duration(scnt)
		for i := uint64(0); i < uint64(scnt); i++ {
			ent = &entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				SRC:  src,
				Data: genData(ts),
			}
			if err = conn.WriteEntry(ent); err != nil {
				break loop
			}
			totalBytes += uint64(len(ent.Data))
			totalCount++
			ts = ts.Add(diff)
		}
		time.Sleep(time.Second - time.Since(start))
	}
	return
}
