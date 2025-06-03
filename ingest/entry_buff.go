/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"sync"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	ErrInvalidCount = errors.New("invalid buffer size")

	blank entry.Entry
)

type EntryBuffer struct {
	sync.Mutex
	ci   *CircularIndex
	buff []entry.Entry
}

func NewEntryBuffer(sz uint) (stb *EntryBuffer, err error) {
	var ci *CircularIndex
	if ci, err = NewCircularIndex(sz); err != nil {
		return
	}
	stb = &EntryBuffer{
		ci:   ci,
		buff: make([]entry.Entry, sz),
	}
	return
}

func (stb *EntryBuffer) Count() (r uint) {
	stb.Lock()
	r = stb.ci.Count()
	stb.Unlock()
	return
}

func (stb *EntryBuffer) Free() (r uint) {
	stb.Lock()
	r = stb.ci.Free()
	stb.Unlock()
	return
}

func (stb *EntryBuffer) Size() (r uint) {
	stb.Lock()
	r = stb.ci.Size()
	stb.Unlock()
	return
}

func (stb *EntryBuffer) Pop() (r entry.Entry, ok bool) {
	var idx uint
	stb.Lock()
	if idx, ok = stb.ci.Pop(); ok {
		r = stb.buff[idx]
		//set value to zero so that data pointers are nil and we can GC the memory
		stb.buff[idx] = blank
	}
	stb.Unlock()
	return
}

func (stb *EntryBuffer) PopBlock(max uint) (r []entry.Entry) {
	stb.Lock()
	if max > 0 {
		if max > stb.ci.Count() {
			max = stb.ci.Count()
		}
		r = make([]entry.Entry, 0, max)
		for i := uint(0); i < max; i++ {
			if idx, ok := stb.ci.Pop(); ok {
				r = append(r, stb.buff[idx])
				//set value to zero so that data pointers are nil and we can GC the memory
				stb.buff[idx] = blank
			} else {
				break
			}
		}
	}
	stb.Unlock()
	return
}

func (stb *EntryBuffer) Drain() (r []entry.Entry) {
	var idx uint
	stb.Lock()
	//allocate up front
	if cnt := stb.ci.Count(); cnt > 0 {
		r = make([]entry.Entry, 0, cnt)
		ok := true
		for ok {
			if idx, ok = stb.ci.Pop(); ok {
				//set value to zero so that data pointers are nil and we can GC the memory
				r = append(r, stb.buff[idx])
				stb.buff[idx] = blank
			}
		}
	}
	stb.Unlock()
	return
}

func (stb *EntryBuffer) Add(ste entry.Entry) {
	stb.Lock()
	if stb.buff == nil {
		//just in case
		stb.Unlock()
		return
	}
	stb.buff[stb.ci.Add()] = ste
	stb.Unlock()
}

func (stb *EntryBuffer) AddBlock(stes []entry.Entry) {
	stb.Lock()
	if stb.buff == nil {
		//just in case
		stb.Unlock()
		return
	}
	for _, ste := range stes {
		stb.buff[stb.ci.Add()] = ste
	}
	stb.Unlock()
}

// CircularIndex is a re-usable circular buffer index
type CircularIndex struct {
	max   uint
	count uint
	head  uint //points to the first occupied slot.  Special case for empty
	tail  uint //points to the last free slot.   Special case for empty
}

func NewCircularIndex(sz uint) (ci *CircularIndex, err error) {
	if sz == 0 {
		err = ErrInvalidCount
		return
	}
	ci = &CircularIndex{
		max:   sz,
		count: 0,
		head:  0,
		tail:  0,
	}
	return
}

func (cb *CircularIndex) Count() uint {
	return cb.count
}

func (cb *CircularIndex) Size() uint {
	return cb.max
}

func (cb *CircularIndex) Free() uint {
	return cb.max - cb.count
}

func (cb *CircularIndex) Pop() (idx uint, ok bool) {
	if cb.count > 0 {
		idx = cb.head
		ok = true
		if cb.count--; cb.count == 0 {
			//check if we are drained and we can just reset
			// this will also catch the case where we drain and overtake tail
			cb.head = 0
			cb.tail = 0
		} else {
			cb.head = inc(cb.head, cb.max)
		}
	}
	return
}

func (cb *CircularIndex) Add() (idx uint) {
	if cb.count == 0 { //special case
		cb.count = 1
		cb.tail = inc(0, cb.max)
		cb.head = 0
		idx = 0
	} else {
		idx = cb.tail
		cb.tail = inc(cb.tail, cb.max)
		if cb.count == cb.max {
			//we are full, so blow away the current head
			cb.head = inc(cb.head, cb.max)
		} else {
			cb.count++ //not full, grow
		}
	}
	return
}

func inc(curr, max uint) uint {
	//special case for max uint
	if curr >= max {
		return 0
	}
	curr++ //increment
	if curr >= max {
		//if we hit max, wrap
		return 0
	}
	return curr //no wrap
}
