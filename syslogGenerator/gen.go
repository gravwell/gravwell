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
	"math/rand"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	streamBlock = 10
)

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

loop:
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
				break loop
			}
			totalBytes += uint64(len(dt))
		}
		time.Sleep(sp)
	}
	return
}

func genData(ts time.Time) []byte {
	sev := rand.Intn(24)
	fac := rand.Intn(7)
	prio := (sev << 3) | fac
	return []byte(fmt.Sprintf("<%d>1 %s %s %s %d - %s %s",
		prio, ts.Format(tsFormat), getHost(), getApp(), rand.Intn(0xffff), genStructData(), rd.Paragraph()))
}

func genStructData() string {
	return fmt.Sprintf(`[%s source-address="%s" source-port=%d destination-address="%s" destination-port=%d useragent="%s"]`, rd.Email(), rd.IpV4Address(), 0x2000+rand.Intn(0xffff-0x2000), rd.IpV4Address(), 1+rand.Intn(2047), rd.UserAgentString())
}
