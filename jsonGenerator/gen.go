/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	streamBlock = 10
)

type datum struct {
	TS    time.Time `json:"time"`
	Class int       `json:"class"`
	Account
	Group     string `json:"group"`
	UserAgent string `json:"useragent"`
	IP        string `json:"ip"`
	Data      string `json:"data"`
}

func throw(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64, dur time.Duration) (err error) {
	sp := dur / time.Duration(cnt)
	ts := time.Now()
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
		ts = ts.Add(-1 * sp)
		totalBytes += uint64(len(dt))
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

func stream(igst *ingest.IngestMuxer, tag entry.EntryTag, cnt uint64) (err error) {
	var blksize uint64
	if cnt < streamBlock {
		blksize = 1
	} else {
		blksize = streamBlock
	}
	sp := time.Second / time.Duration((cnt / blksize))

	for {
		for i := uint64(0); i < blksize; i++ {
			ts := time.Now()
			dt := genData(ts)
			if err = igst.WriteEntry(&entry.Entry{
				TS:   entry.FromStandard(ts),
				Tag:  tag,
				SRC:  src,
				Data: dt,
			}); err != nil {
				return
			}
			totalBytes += uint64(len(dt))
		}
		time.Sleep(sp)
	}
	return
}

func genData(ts time.Time) (r []byte) {
	var d datum
	d.TS = ts
	d.Class = rand.Int() % 0xffff
	d.Data = rd.Paragraph()
	d.Group = getGroup()
	d.Account = getUser()
	d.UserAgent = rd.UserAgentString()
	d.IP = rd.IpV4Address()
	r, _ = json.Marshal(d)
	return
}
