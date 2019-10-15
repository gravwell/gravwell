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
	"golang.org/x/sys/windows/svc/eventlog"
)

const (
	tsFormat string = `2006-01-02T15:04:05.999999Z07:00`
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
	for !*stop {
		start := time.Now()
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
		time.Sleep(time.Second - time.Since(start))
	}
	return
}

func genData(ts time.Time) string {
	return fmt.Sprintf("%v %v %v", ts.Format(tsFormat), rand.Intn(0xffff), rd.Paragraph())
}