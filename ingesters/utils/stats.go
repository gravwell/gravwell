/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	statsMsg          = `ingest stats`
	statsIntervalName = `sample-interval-ms`
)

type StatsItem struct {
	name string
	last uint64
	curr uint64
}

type StatsManager struct {
	sync.Mutex
	lgr      *log.Logger
	started  bool
	interval time.Duration
	wg       sync.WaitGroup
	ctx      context.Context
	cf       context.CancelFunc

	items []*StatsItem
}

func NewStatsManager(interval time.Duration, lgr *log.Logger) (*StatsManager, error) {
	if interval < 0 {
		return nil, errors.New("invalid interval")
	} else if lgr == nil {
		return nil, errors.New("invalid logger")
	}
	ctx, cf := context.WithCancel(context.Background())
	return &StatsManager{
		interval: interval,
		lgr:      lgr,
		wg:       sync.WaitGroup{},
		ctx:      ctx,
		cf:       cf,
	}, nil
}

func (sm *StatsManager) Start() error {
	sm.Lock()
	defer sm.Unlock()
	if sm.started {
		return errors.New("manager already started")
	}
	sm.started = true
	sm.wg.Add(1)
	go sm.routine()
	return nil
}

func (sm *StatsManager) Stop() {
	var doWait bool
	//if active, throw final stats
	sm.Lock()
	if sm.started {
		sm.cf()
		sm.started = false
		doWait = true
	}
	sm.Unlock()
	if doWait {
		sm.wg.Wait()
	}
}

func (sm *StatsManager) RegisterItem(name string) (si *StatsItem, err error) {
	if name == `` {
		return nil, errors.New("missing name")
	}
	sm.Lock()
	defer sm.Unlock()
	for _, v := range sm.items {
		if name == v.name {
			err = fmt.Errorf("StatsItem %v already registered", name)
			return
		}
	}
	si = &StatsItem{
		name: name,
	}
	sm.items = append(sm.items, si)
	return
}

func (sm *StatsManager) routine() {
	defer sm.wg.Done()
	if sm.interval <= 0 {
		return // we are actively being used, so just bail
	}
	tckr := time.NewTicker(sm.interval)
	ts := time.Now()
loop:
	for {
		select {
		case now := <-tckr.C:
			sm.doTick(now.Sub(ts))
			ts = now
		case <-sm.ctx.Done():
			break loop
		}
	}
}

func (sm *StatsManager) doTick(dur time.Duration) {
	var params []rfc5424.SDParam
	// flag indicating that there were some stats that were not zero
	// we do this so that idle ingesters don't just keep barking empty stats
	var ok bool
	sm.Lock()
	if len(sm.items) > 0 {
		params = make([]rfc5424.SDParam, 0, len(sm.items)+1)
		params = append(params, log.KV(statsIntervalName, dur.Milliseconds()))
		//gather values
		for _, v := range sm.items {
			val := v.reset()
			if val != 0 {
				ok = true
			}
			params = append(params, log.KV(v.name, val))
		}
	}
	sm.Unlock()

	//emit entry
	if len(params) > 0 && ok {
		sm.lgr.Info(statsMsg, params...)
	}
}

func (si *StatsItem) Add(v uint64) {
	if si != nil {
		atomic.AddUint64(&si.curr, v)
	}
}

func (si *StatsItem) reset() (curr uint64) {
	if si != nil {
		//reset and
		si.last = atomic.SwapUint64(&si.curr, 0)
		curr = si.last // this could theoretically race, but reset should be controlled by a ticker, so not a huge worry
	}
	return
}
