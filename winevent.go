/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
// +build windows

package winevent

import (
	"bytes"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"

	"gravwell/oss/winevent/wineventlog"
)

const (
	defaultBuffSize  = 512 * 1024 //512kb?  Sure... why not
	maxHandleRequest = 1024
	minHandleRequest = 2 //this CANNOT be less than 2, or you will fall into an infinite loop HAMMERING the kernel
)

type EventStreamHandle struct {
	name      string
	sigEvent  windows.Handle
	subHandle wineventlog.EvtHandle
	bookmark  wineventlog.EvtHandle
	last      uint64
	buff      []byte
	mtx       *sync.Mutex
}

func NewStream(param EventStreamParams, last uint64) (*EventStreamHandle, error) {
	bookmark, err := wineventlog.CreateBookmark(param.Channel, last)
	if err != nil {
		return nil, err
	}

	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	query, err := genQuery(param)
	if err != nil {
		return nil, err
	}

	subHandle, err := wineventlog.Subscribe(
		0, //localhost session
		sigEvent, ``,
		query,
		bookmark,
		wineventlog.EvtSubscribeStartAfterBookmark)
	if err != nil {
		windows.CloseHandle(sigEvent)
		return nil, err
	}
	return &EventStreamHandle{
		name:      param.Name,
		sigEvent:  sigEvent,
		subHandle: subHandle,
		bookmark:  bookmark,
		last:      last,
		buff:      make([]byte, defaultBuffSize),
		mtx:       &sync.Mutex{},
	}, nil
}

//getHandles will iterate on the call to EventHandles, we do this because on big event log entries the kernel throws
//RPC_S_INVALID_BOUND which is basically a really shitty way to say "i can't give you all the handles due to size"
func getHandles(sub wineventlog.EvtHandle, min, max int) ([]wineventlog.EvtHandle, error) {
	for cnt := max; cnt >= min; cnt = cnt / 2 {
		evtHandles, err := wineventlog.EventHandles(sub, cnt)
		if err != nil {
			if err == wineventlog.RPC_S_INVALID_BOUND {
				continue //try again with smaller bounds
			}
			return nil, err
		}
		return evtHandles, nil
	}
	return nil, wineventlog.RPC_S_INVALID_BOUND
}

func (e *EventStreamHandle) Read() ([]([]byte), error) {
	var ents []([]byte)
	evtHandles, err := getHandles(e.subHandle, minHandleRequest, maxHandleRequest)
	if err != nil {
		if err == wineventlog.ERROR_NO_MORE_ITEMS {
			return nil, nil
		}
		return nil, err
	}
	bb := bytes.NewBuffer(nil)
	for _, h := range evtHandles {
		bb.Reset()
		if err := wineventlog.RenderEvent(h, 0, e.buff, nil, bb); err != nil {
			wineventlog.Close(h)
			return nil, err
		}
		wineventlog.Close(h)
		ents = append(ents, append([]byte(nil), bb.Bytes()...))
	}
	return ents, nil
}

func (e *EventStreamHandle) Name() string {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	return e.name
}

func (e *EventStreamHandle) Last() uint64 {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	return e.last
}

func (e *EventStreamHandle) SetLast(v uint64) {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	e.last = v
}

func (e *EventStreamHandle) Close() error {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	if err := wineventlog.Close(e.bookmark); err != nil {
		return err
	}
	if err := windows.CloseHandle(e.sigEvent); err != nil {
		return err
	}
	if err := wineventlog.Close(e.subHandle); err != nil {
		return err
	}
	return nil
}

func ChannelAvailable(c string) (bool, error) {
	chs, err := wineventlog.Channels()
	if err != nil {
		return false, err
	}
	for i := range chs {
		if chs[i] == c {
			return true, nil
		}
	}
	return false, nil
}

func printChannels() {
	chs, err := wineventlog.Channels()
	if err != nil {
		fmt.Println("Channels error", err)
		return
	}
	for i := range chs {
		fmt.Println("channel", i, chs[i])
	}
}

func genQuery(p EventStreamParams) (string, error) {
	return wineventlog.Query{
		Log:         p.Channel,
		IgnoreOlder: p.ReachBack,
		Level:       p.Levels,
		Provider:    p.Providers,
		EventID:     p.EventIDs, //black list and white list of event IDs (add - in front to remove one) blank is all
	}.Build()
}
