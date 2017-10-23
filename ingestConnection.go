/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/gravwell/ingest/entry"
)

var (
	localSrc = net.ParseIP("127.0.0.1")
)

type IngestConnection struct {
	conn       net.Conn
	ew         *EntryWriter
	src        net.IP
	tags       map[string]entry.EntryTag
	running    bool
	errorState error
	mtx        sync.Mutex
}

func (igst *IngestConnection) Close() error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if !igst.running {
		return errors.New("Already closed")
	}
	err := igst.ew.Close()
	igst.running = false
	return err
}

func (igst *IngestConnection) outstandingEntries() []*entry.Entry {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if igst.ew == nil {
		return nil
	}
	return igst.ew.outstandingEntries()
}

func (igst *IngestConnection) Write(ts entry.Timestamp, tag entry.EntryTag, data []byte) error {
	return igst.WriteEntry(&entry.Entry{ts, igst.src, tag, data})
}

func (igst *IngestConnection) WriteEntry(ent *entry.Entry) error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if igst.running == false {
		return errors.New("Not running")
	}
	//if no source set, put the resolved one
	if ent != nil && len(ent.SRC) == 0 {
		ent.SRC = igst.src
	}
	return igst.ew.Write(ent)
}

// WriteBatchEntry DOES NOT populate the source on write, the caller must do so
func (igst *IngestConnection) WriteBatchEntry(ents []*entry.Entry) error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if igst.running == false {
		return errors.New("Not running")
	}
	return igst.ew.WriteBatch(ents)
}

func (igst *IngestConnection) WriteEntrySync(ent *entry.Entry) error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if igst.running == false {
		return errors.New("Not running")
	}
	//if no source set, put the resolved one
	if ent != nil && len(ent.SRC) == 0 {
		ent.SRC = igst.src
	}
	return igst.ew.WriteSync(ent)
}

func (igst *IngestConnection) GetTag(name string) (entry.EntryTag, bool) {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	tg, ok := igst.tags[name]
	if !ok {
		return 0, false
	}
	return tg, true
}

/* Sync causes the entry writer to force an ack from teh server.  This ensures that all
*  entries that have been written are flushed and fully acked by the server. */
func (igst *IngestConnection) Sync() error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	return igst.ew.ForceAck()
}

func (igst *IngestConnection) Running() bool {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	return igst.running
}

func (igst *IngestConnection) Source() (net.IP, error) {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if !igst.running {
		return nil, errors.New("not running")
	}
	if len(igst.src) == 0 {
		return localSrc, nil
	}
	return igst.src, nil
}

func authenticate(conn io.ReadWriter, hash AuthHash, tags []string) (map[string]entry.EntryTag, error) {
	var tagReq TagRequest
	var tagResp TagResponse
	var state StateResponse
	var chal Challenge

	//recieve the challenge
	if err := chal.Read(conn); err != nil {
		return nil, err
	}

	//generate response
	resp, err := GenerateResponse(hash, chal)
	if err != nil {
		return nil, err
	} else if resp == nil {
		return nil, errors.New("Got a new challenge response")
	}

	//throw response
	if err := resp.Write(conn); err != nil {
		return nil, err
	}

	//get state response
	if err := state.Read(conn); err != nil {
		return nil, err
	}
	if state.ID != STATE_AUTHENTICATED {
		return nil, errors.New(state.Info)
	}

	//throw list of tags we need
	tagReq.Tags = tags
	tagReq.Count = uint32(len(tags))
	if err = tagReq.Write(conn); err != nil {
		return nil, err
	}

	//Check list
	if err := tagResp.Read(conn); err != nil {
		return nil, err
	}
	//Throw "we're hot" message
	state.ID = STATE_HOT
	state.Info = ""
	if err = state.Write(conn); err != nil {
		return nil, err
	}
	//ok, we are good to go
	return tagResp.Tags, nil
}

func checkTags(tags []string) error {
	for i := range tags {
		if err := CheckTag(tags[i]); err != nil {
			return err
		}
	}
	return nil
}

func CheckTag(tag string) error {
	if strings.ContainsAny(tag, FORBIDDEN_TAG_SET) {
		if strings.ContainsAny(tag, FORBIDDEN_TAG_SET) {
			return fmt.Errorf("tag %s contains forbidden characters", tag)
		}
	}
	return nil
}
