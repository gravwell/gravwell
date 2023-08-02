//go:build windows
// +build windows

/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package winevent

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/gravwell/gravwell/v3/winevent/wineventlog"
)

const (
	//if we don't get any events for this long of time, we will poll the
	//event handle to see if something has gone fucky
	eventHandleCheckupInterval time.Duration = 10 * time.Second

	//this CANNOT be less than 2
	//or you will fall into an infinite loop HAMMERING the kernel
	MinHandleRequest = 2
)

type EventStreamParams struct {
	Name         string
	TagName      string
	Channel      string
	Levels       string
	EventIDs     string
	Providers    []string
	ReachBack    time.Duration
	Preprocessor []string
	BuffSize     int
	ReqSize      int
}

func (esp *EventStreamParams) IsFiltering() bool {
	return esp.Levels != `` || esp.EventIDs != `` || len(esp.Providers) > 0
}

type EventStreamHandle struct {
	params       EventStreamParams
	subHandle    wineventlog.EvtHandle
	bmk          wineventlog.EvtHandle
	filePath     string
	fileCreation time.Time
	buff         []byte
	last         uint64
	prev         uint64
	checkGaps    bool
	mtx          *sync.Mutex
	lastRead     time.Time
}

func NewStream(param EventStreamParams, last uint64) (e *EventStreamHandle, err error) {
	if last > 0 {
		//if we have a last value, we don't want to do reachback
		// this is important because this query filters everything
		param.ReachBack = 0
	}
	e = &EventStreamHandle{
		params:    param,
		buff:      make([]byte, param.BuffSize),
		mtx:       &sync.Mutex{},
		last:      last,
		prev:      last,
		checkGaps: !param.IsFiltering(),
		lastRead:  time.Now(),
	}
	if err = e.open(); err != nil {
		e = nil
		return
	}
	return
}

func (e *EventStreamHandle) open() (err error) {
	e.mtx.Lock()
	err = e.openNoLock()
	e.mtx.Unlock()
	return
}

func (e *EventStreamHandle) openNoLock() error {
	var err error
	if e.last == 0 {
		//get a record id
		if e.last, err = e.getRecordID(); err != nil {
			return err
		}
	}
	params := e.params
	//disable the reachback parameter after we get our recordID
	params.ReachBack = 0
	if e.fileCreation, err = wineventlog.GetChannelFileCreationTime(e.params.Channel); err != nil {
		return err
	} else if e.filePath, err = wineventlog.GetChannelFilePath(e.params.Channel); err != nil {
		return err
	}
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(sigEvent)
	query, err := genQuery(params)
	if err != nil {
		return err
	}
	//we build our bookmark
	flags := wineventlog.EvtSubscribeStartAfterBookmark
	if e.bmk, err = wineventlog.CreateBookmarkFromRecordID(e.params.Channel, e.last); err != nil {
		return err
	}

	//subscribe to the channel using our local session
	subHandle, err := wineventlog.Subscribe(0, sigEvent, ``, query, e.bmk, flags)
	if err != nil {
		return err
	}

	e.subHandle = subHandle
	return nil
}

func SeekFileToBookmark(hnd, bookmark wineventlog.EvtHandle) (err error) {
	// This seeks to the last read event and strictly validates that the bookmarked record number exists.
	if err = wineventlog.EvtSeek(hnd, 0, bookmark, wineventlog.EvtSeekRelativeToBookmark|wineventlog.EvtSeekStrict); err == nil {
		// Then we advance past the last read event to avoid sending that event again.
		// This won't fail if we're at the end of the file.
		if seekErr := wineventlog.EvtSeek(hnd, 1, bookmark, wineventlog.EvtSeekRelativeToBookmark); seekErr != nil {
			err = fmt.Errorf("failed to seek past bookmarked position: %w", seekErr)
		}
	} else {
		err = nil //trying to go to the start
		//its too large, go ahead and seek to the beginning
		if seekErr := wineventlog.EvtSeek(hnd, 0, 0, wineventlog.EvtSeekRelativeToFirst); seekErr != nil {
			err = fmt.Errorf("failed to seek to beginning: %w", seekErr)
		}
	}
	return
}

func (e *EventStreamHandle) closeNoLock() (err error) {
	if err = wineventlog.Close(e.subHandle); err != nil {
		wineventlog.Close(e.bmk)
	} else {
		err = wineventlog.Close(e.bmk)
	}
	return
}

func (e *EventStreamHandle) Reset() (err error) {
	e.mtx.Lock()
	err = e.resetNoLock()
	e.mtx.Unlock()
	return
}

func (e *EventStreamHandle) resetNoLock() (err error) {
	e.closeNoLock()
	err = e.openNoLock()
	return
}

// getHandles will iterate on the call to EventHandles, we do this because on big event log entries the kernel throws
// RPC_S_INVALID_BOUND which is basically a really atrocious way to say "i can't give you all the handles due to size"
func (e *EventStreamHandle) getHandles(start int) (evtHnds []wineventlog.EvtHandle, fullRead bool, err error) {
	for cnt := start; cnt >= MinHandleRequest; cnt = cnt / 2 {
		evtHnds, err = wineventlog.EventHandles(e.subHandle, cnt)
		switch err {
		case nil:
			fullRead = len(evtHnds) == cnt
			e.lastRead = time.Now()
			return //got a good read
		case wineventlog.ERROR_NO_MORE_ITEMS:
			err = nil
			return //empty
		case wineventlog.RPC_S_INVALID_BOUND:
			//our buffer isn't big enough, reset the handle and try again
			if err = e.resetNoLock(); err != nil {
				return
			}
			//we will retry
		default:
			return
		}
	}
	//if we hit here, then our buffer is not big enough to handle two entries
	if evtHnds, err = wineventlog.EventHandles(e.subHandle, 1); err == nil && len(evtHnds) == 1 {
		fullRead = true
	}
	return
}

func (e *EventStreamHandle) checkEventHandles() (warn, err error) {
	var ts time.Time
	var pth string
	if ts, err = wineventlog.GetChannelFileCreationTime(e.params.Channel); err != nil {
		return
	} else if ts != e.fileCreation {
		if err = e.resetNoLock(); err != nil {
			err = fmt.Errorf("Failed to reset event stream after time change: %v", err)
		} else {
			warn = fmt.Errorf("Backing event file reset, reinitializing the event stream")
		}
	} else if pth, err = wineventlog.GetChannelFilePath(e.params.Channel); err != nil {
		return
	} else if pth != e.filePath {
		if err = e.resetNoLock(); err != nil {
			err = fmt.Errorf("Failed to reset event stream after path change: %v", err)
		} else {
			warn = fmt.Errorf("Backing event file moved, reinitializing the event stream")
		}
	}
	return
}

// getRecordID just grabs the oldest record that matches our bookmark
func (e *EventStreamHandle) getRecordID() (uint64, error) {
	bb := bytes.NewBuffer(nil)
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)
	query, err := genQuery(e.params)
	if err != nil {
		return 0, err
	}
	//we build our bookmark
	flags := wineventlog.EvtSubscribeStartAfterBookmark
	bmk, err := wineventlog.CreateBookmarkFromRecordID(e.params.Channel, e.last)
	if err != nil {
		return 0, err
	}
	defer wineventlog.Close(bmk)

	subHandle, err := wineventlog.Subscribe(
		0, //localhost session
		sigEvent,
		``,    // channel is in the query
		query, //query has the reachback parameter
		bmk,
		flags)
	if err != nil {
		return 0, err
	}
	defer wineventlog.Close(subHandle)

	evtHnds, err := wineventlog.EventHandles(subHandle, 1)
	switch err {
	case nil:
		if len(evtHnds) != 1 {
			return 0, fmt.Errorf("invalid return count %d != 1 on seek", len(evtHnds))
		}
		if err = wineventlog.UpdateBookmarkFromEvent(bmk, evtHnds[0]); err != nil {
			return 0, err
		}
		id, err := wineventlog.GetRecordIDFromBookmark(bmk, e.buff, bb)
		if err != nil {
			return 0, err
		}
		wineventlog.Close(evtHnds[0])
		if id > 0 {
			//NOTE README FIXME - always decrement or we will throw away the entry from this sample
			id--
		}
		return id, nil
	case wineventlog.ERROR_NO_MORE_ITEMS:
		return 0, nil
	}
	return 0, err
}

func printLogInfo(buff []byte) {
	var l int
	if l = len(buff); l < 16 {
		fmt.Println("unknown buff")
		return
	}

	//print the count and type type
	count := binary.LittleEndian.Uint32(buff[l-8 : l-4])
	tp := binary.LittleEndian.Uint32(buff[l-4:])
	fmt.Println("COUNT:", count, "TYPE:", tp, binary.LittleEndian.Uint32(buff[:4]), binary.LittleEndian.Uint32(buff[4:8]))
}

type RenderedEvent struct {
	Buff []byte
	ID   uint64
}

func (e *EventStreamHandle) Read() (ents []RenderedEvent, fullRead bool, warn, err error) {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	var evtHandles []wineventlog.EvtHandle
	if evtHandles, fullRead, err = e.getHandles(e.params.ReqSize); err != nil {
		return
	} else if len(evtHandles) == 0 {
		//check if we need to poll the log event values
		if time.Since(e.lastRead) > eventHandleCheckupInterval {
			warn, err = e.checkEventHandles()
		}
		return
	}
	bb := bytes.NewBuffer(nil)
	for i, h := range evtHandles {
		var re RenderedEvent
		bb.Reset()
		if err = wineventlog.RenderEventSimple(h, e.buff, bb); err != nil {
			ents = nil
			break
		}
		re.Buff = append(re.Buff, bb.Bytes()...)
		bb.Reset()
		if err = wineventlog.UpdateBookmarkFromEvent(e.bmk, h); err != nil {
			ents = nil
			break
		} else if re.ID, err = wineventlog.GetRecordIDFromBookmark(e.bmk, e.buff, bb); err != nil {
			ents = nil
			break
		}
		if e.checkGaps && (e.prev+1) != re.ID {
			jump := re.ID - e.prev
			warn = fmt.Errorf("RecordID Jumped %d from %d to %d at request batch offset %d / %d", jump, e.prev, re.ID, i, len(evtHandles))
		}
		e.prev = re.ID
		e.last = re.ID
		ents = append(ents, re)
	}
	for _, h := range evtHandles {
		wineventlog.Close(h)
	}
	return
}

func (e *EventStreamHandle) Name() (s string) {
	e.mtx.Lock()
	s = e.params.Name
	e.mtx.Unlock()
	return
}

func (e *EventStreamHandle) Last() (l uint64) {
	e.mtx.Lock()
	l = e.last
	e.mtx.Unlock()
	return
}

func (e *EventStreamHandle) SetLast(v uint64) {
	e.mtx.Lock()
	e.last = v
	e.mtx.Unlock()
}

func (e *EventStreamHandle) SinceLastRead() (d time.Duration) {
	e.mtx.Lock()
	d = time.Since(e.lastRead)
	e.mtx.Unlock()
	return
}

func (e *EventStreamHandle) Close() (err error) {
	e.mtx.Lock()
	err = e.closeNoLock()
	e.mtx.Unlock()
	return
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

/*
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
*/

func genQuery(p EventStreamParams) (string, error) {
	return wineventlog.Query{
		Log:         p.Channel,
		IgnoreOlder: p.ReachBack,
		Level:       p.Levels,
		Provider:    p.Providers,
		EventID:     p.EventIDs, //black list and white list of event IDs (add - in front to remove one) blank is all
	}.Build()
}
