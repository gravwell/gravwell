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
	"io"
	"log"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/bet365/jingo"
	"github.com/gravwell/generators/ipgen"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	streamBlock = 10
)

type datum struct {
	//TS        time.Time `json:"time"`
	TS string `json:"time"`
	Account
	Class     int    `json:"class"`
	Group     string `json:"group"`
	UserAgent string `json:"useragent"`
	IP        string `json:"ip"`
	Data      string `json:"data"`
}

var (
	enc   = jingo.NewStructEncoder(datum{})
	v4gen *ipgen.V4Gen
)

func init() {
	var err error
	v4gen, err = ipgen.RandomWeightedV4Generator(40)
	if err != nil {
		log.Fatal("Failed to instantiate v4 generator: %v", err)
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

func throwFile(w io.Writer, cnt uint64, dur time.Duration) (err error) {
	sp := dur / time.Duration(cnt)
	ts := time.Now()
	for i := uint64(0); i < cnt; i++ {
		dt := genData(ts)
		if _, err = fmt.Fprintf(w, "%s\n", string(dt)); err != nil {
			break
		}
		ts = ts.Add(-1 * sp)
		totalBytes += uint64(len(dt))
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

// genData creates a marshalled JSON buffer
// the jingo encoder is faster, but because we throw the buffers into our entries
// and hand them into the ingest muxer we can't really track those buffers so we won't get the benefit
// of the buffered pool.  The encoder is still about 3X faster than the standard library encoder
func genData(ts time.Time) (r []byte) {
	bb := jingo.NewBufferFromPool()
	var d datum
	//d.TS = ts //for stdlib json encoder
	d.TS = ts.UTC().Format(time.RFC3339)
	d.Class = rand.Int() % 0xffff
	d.Data = rd.Paragraph()
	d.Group = getGroup()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = v4gen.IP().String()
	enc.Marshal(&d, bb)
	r = append(r, bb.Bytes...) //copy out of the pool
	bb.ReturnToPool()
	return
}
