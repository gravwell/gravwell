/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/generators/ipgen"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	streamBlock = 10
)

var (
	v4gen *ipgen.V4Gen
	v6gen *ipgen.V6Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(3)
	if err != nil {
		log.Fatal("Failed to instantiate v4 generator: %v", err)
	}
	v6gen, err = ipgen.RandomWeightedV6Generator(30)
	if err != nil {
		log.Fatal("Failed to instantiate v6 generator: %v", err)
	}
}

func throw(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64, dur time.Duration) (err error) {
	sp := dur / time.Duration(cnt)
	ts := time.Now().Add(-1 * dur)
	for i := uint64(0); i < cnt; i++ {
		dt := genData(ts)
		if err = igst.WriteEntry(&entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			SRC:  src,
			Data: dt,
		}); err != nil {
			return
		}
		ts = ts.Add(sp)
		totalBytes += uint64(len(dt))
		totalCount++
	}
	return
}

func stream(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64, stop *bool) (err error) {
	sp := time.Second / time.Duration(cnt)
	var ent *entry.Entry
loop:
	for !*stop {
		ts := time.Now()
		start := ts
		for i := uint64(0); i < cnt; i++ {
			dt := genData(ts)
			ent = &entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				SRC:  src,
				Data: dt,
			}
			if err = igst.WriteEntry(ent); err != nil {
				break loop
			}
			totalBytes += uint64(len(dt))
			totalCount++
			ts = ts.Add(sp)
		}
		time.Sleep(time.Second - time.Since(start))
	}
	return
}

func genData(ts time.Time) []byte {
	ipa, ipb := ips()
	return []byte(fmt.Sprintf("%s%s%s%s%s%s%d%s%s%s%d%s\"%s\"",
		ts.Format(tsFormat), delim, getApp(), delim,
		ipa, delim, 1+rand.Intn(2048), delim,
		ipb, delim, 2048+rand.Intn(0xffff-2048), delim,
		rd.Paragraph()))
}

func ips() (string, string) {
	if (rand.Int() & 3) == 0 {
		//more IPv4 than 6
		return v6gen.IP().String(), v6gen.IP().String()
	}
	return v4gen.IP().String(), v4gen.IP().String()
}
