/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package base

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
)

type statusUpdater struct {
	count   *uint64
	bytes   *uint64
	rc      chan bool
	wg      sync.WaitGroup
	started bool
	stopped bool
}

func newStatusUpdater(count, bytes *uint64) (su *statusUpdater, err error) {
	if count == nil || bytes == nil {
		err = errors.New("bad parameters")
	} else {
		su = &statusUpdater{
			count: count,
			bytes: bytes,
			rc:    make(chan bool, 1),
		}
	}
	return
}

func (su *statusUpdater) Start() error {
	if su.started {
		return errors.New("already started")
	}
	su.started = true
	su.wg.Add(1)
	go su.routine()
	return nil
}

func (su *statusUpdater) Stop() (err error) {
	if !su.started {
		return errors.New("not started")
	} else if su.stopped {
		return errors.New("already stopped")
	}
	su.stopped = true
	close(su.rc)
	su.wg.Wait()
	return
}

func (su *statusUpdater) routine() {
	var lastCount, lastBytes uint64
	defer su.wg.Done()
	tmr := time.NewTicker(time.Second)
	ts := time.Now()
	for {
		select {
		case <-tmr.C:
			fmt.Printf("\r")
			currCount := *su.count
			currBytes := *su.bytes
			segCount := currCount - lastCount
			segBytes := currBytes - lastBytes
			su.printStats(currBytes, segBytes, currCount, segCount, time.Since(ts))
			ts = time.Now()
			lastCount = currCount
			lastBytes = currBytes
		case <-su.rc:
			fmt.Printf("\n")
			return
		}
	}
}

func (su *statusUpdater) printStats(totalBytes, segmentBytes, totalCount, segmentCount uint64, dur time.Duration) {
	fmt.Printf("Total: %s %s  Rate: %s %s                        ",
		ingest.HumanSize(totalBytes), ingest.HumanCount(totalCount),
		ingest.HumanRate(segmentBytes, dur),
		ingest.HumanEntryRate(segmentCount, dur))
}
