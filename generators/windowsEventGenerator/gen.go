/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	rd "github.com/Pallinder/go-randomdata"
	"golang.org/x/sys/windows/svc/eventlog"
)

const (
	tsFormat   string = `2006-01-02T15:04:05.999999Z07:00`
	kb                = 1024
	mb                = 1024 * kb
	maxLogSize        = 31 * kb
)

func throw(wlog *eventlog.Log, cnt uint64) (err error) {
	for i := uint64(0); i < cnt; i++ {
		ts := time.Now()
		dt := genData(ts)
		// Randomly write error, info, or warning
		switch rand.Intn(3) {
		case 0:
			err = wlog.Error(uint32(rand.Intn(9999)+1), dt)
		case 1:
			err = wlog.Warning(uint32(rand.Intn(9999)+1), dt)
		case 2:
			err = wlog.Info(uint32(rand.Intn(9999)+1), dt)
		}
		if err != nil {
			return
		}
		totalBytes += uint64(len(dt))
		totalCount++
	}
	return
}

func stream(wlog *eventlog.Log, cnt uint64, stop *bool) (err error) {
	timeBetweenEvents := (time.Second / time.Duration(cnt))
	for !*stop {
		ts := time.Now()
		dt := genData(ts)
		// Randomly write error, info, or warning
		switch rand.Intn(3) {
		case 0:
			err = wlog.Error(uint32(rand.Intn(9999)+1), dt)
		case 1:
			err = wlog.Warning(uint32(rand.Intn(9999)+1), dt)
		case 2:
			err = wlog.Info(uint32(rand.Intn(9999)+1), dt)
		}
		if err != nil {
			return
		}
		totalBytes += uint64(len(dt))
		totalCount++
		time.Sleep(timeBetweenEvents)
	}
	return
}

func genData(ts time.Time) string {
	if *big {
		return genBigData(ts)
	}
	return fmt.Sprintf("%v %v %v", ts.Format(tsFormat), rand.Intn(0xffff), rd.Paragraph())
}

var (
	pad []byte
)

func genBigData(ts time.Time) string {
	if pad == nil {
		genPad()
	}
	datalen := 2048 + rand.Intn(maxLogSize)
	if datalen > maxLogSize {
		datalen = maxLogSize
	}
	offset := rand.Intn(len(pad) - datalen)
	return string(pad[offset : offset+datalen])
}

func genPad() {
	if len(pad) == 0 {
		sb := strings.Builder{}
		for sb.Len() < 8*mb {
			sb.WriteString(rd.Paragraph())
		}
		pad = []byte(sb.String())
	}
}
