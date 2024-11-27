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
	"io"
	"net"
	"sync"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	nilString     string = `nil`
	nilConnString string = `disconnected`
)

var (
	localSrc        = net.ParseIP("127.0.0.1")
	ErrEmptyTag     = errors.New("Tag name is empty")
	ErrOversizedTag = errors.New("Tag name is too long")
	ErrForbiddenTag = errors.New("Forbidden character in tag")
)

// IngestConnection is a lower-level interface for connecting to and
// communicating with a single indexer. It is kept public for compatibility,
// but should not be used in new projects.
//
// Deprecated: Use the IngestMuxer instead.
type IngestConnection struct {
	conn       net.Conn
	ew         *EntryWriter
	src        net.IP
	tags       map[string]entry.EntryTag
	running    bool
	errorState error
	mtx        sync.RWMutex
}

func (igst *IngestConnection) String() (s string) {
	if igst == nil {
		return nilString
	} else if igst.conn == nil {
		s = nilConnString
	} else if ra := igst.conn.RemoteAddr(); ra == nil {
		s = nilConnString
	} else {
		s = ra.String()
	}
	return
}

func (igst *IngestConnection) Close() error {
	igst.mtx.Lock()
	defer igst.mtx.Unlock()
	if !igst.running {
		return errors.New("Already closed")
	}
	igst.running = false
	return igst.ew.Close()
}

func (igst *IngestConnection) IdentifyIngester(name, version, id string) (err error) {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if err = igst.ew.SendIngesterAPIVersion(); err != nil {
		return
	}
	if err = igst.ew.IdentifyIngester(name, version, id); err != nil {
		return
	}
	return
}

// IngestOK asks the indexer if it is ok to start sending entries yet.
func (igst *IngestConnection) IngestOK() (ok bool, err error) {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	return igst.ew.IngestOK()
}

func (igst *IngestConnection) outstandingEntries() []*entry.Entry {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if igst.ew == nil {
		return nil
	}
	return igst.ew.outstandingEntries()
}

func (igst *IngestConnection) Write(ts entry.Timestamp, tag entry.EntryTag, data []byte) error {
	return igst.WriteEntry(&entry.Entry{TS: ts, SRC: igst.src, Tag: tag, Data: data})
}

func (igst *IngestConnection) WriteEntry(ent *entry.Entry) error {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
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
func (igst *IngestConnection) WriteBatchEntry(ents []*entry.Entry) (err error) {
	_, err = igst.writeBatchEntry(ents)
	return
}

func (igst *IngestConnection) writeBatchEntry(ents []*entry.Entry) (int, error) {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if igst.running == false {
		return 0, errors.New("Not running")
	}
	return igst.ew.WriteBatch(ents)
}

func (igst *IngestConnection) WriteEntrySync(ent *entry.Entry) error {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if igst.running == false {
		return errors.New("Not running")
	}
	//if no source set, put the resolved one
	if ent != nil && len(ent.SRC) == 0 {
		ent.SRC = igst.src
	}
	return igst.ew.WriteSync(ent)
}

func (igst *IngestConnection) WriteDittoBlock(ents []entry.Entry) error {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if igst.running == false {
		return errors.New("Not running")
	}
	return igst.ew.WriteDittoBlock(ents)
}

func (igst *IngestConnection) GetTag(name string) (entry.EntryTag, bool) {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	tg, ok := igst.tags[name]
	if !ok {
		return 0, false
	}
	return tg, true
}

func (igst *IngestConnection) NegotiateTag(name string) (tg entry.EntryTag, err error) {
	if err = CheckTag(name); err != nil {
		return
	}
	igst.mtx.Lock()
	defer igst.mtx.Unlock()

	// First make sure this one hasn't already been negotiated
	tg, ok := igst.tags[name]
	if ok {
		return tg, nil
	}
	if len(igst.tags) >= int(entry.MaxTagId) {
		err = ErrTooManyTags
		return
	}

	if !igst.running {
		err = ErrNotRunning
		return
	}

	// Done! Add it to the tags list and return
	tg, err = igst.ew.NegotiateTag(name)
	if err == nil {
		igst.tags[name] = tg
	}
	return
}

func (igst *IngestConnection) SendIngesterState(state IngesterState) error {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()

	if !igst.running {
		return ErrNotRunning
	}

	return igst.ew.SendIngesterState(state)
}

/* Sync causes the entry writer to force an ack from the server.  This ensures that all
*  entries that have been written are flushed and fully acked by the server. */
func (igst *IngestConnection) Sync() error {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if !igst.running {
		return ErrNotRunning
	}
	return igst.ew.ForceAck()
}

func (igst *IngestConnection) Running() bool {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	return igst.running
}

func (igst *IngestConnection) Source() (net.IP, error) {
	igst.mtx.RLock()
	defer igst.mtx.RUnlock()
	if !igst.running {
		return nil, errors.New("not running")
	}
	if len(igst.src) == 0 {
		return localSrc, nil
	}
	return igst.src, nil
}

func authenticate(conn io.ReadWriter, tenant string, hash AuthHash, tags []string) (map[string]entry.EntryTag, uint16, error) {
	var tagReq TagRequest
	var tagResp TagResponse
	var state StateResponse
	var chal Challenge

	//receive the challenge
	if err := chal.Read(conn); err != nil {
		return nil, 0, err
	}

	//generate response
	resp, err := GenerateResponse(hash, chal)
	if err != nil {
		return nil, 0, err
	} else if resp == nil {
		return nil, 0, ErrNilChallengeResponse
	}
	if tenant != `` && resp.Version < MinTenantAuthVersion {
		return nil, 0, ErrTenantAuthUnsupported
	}
	resp.Tenant = tenant
	//throw response
	if err := resp.Write(conn); err != nil {
		return nil, 0, err
	}

	//get state response
	if err := state.Read(conn); err != nil {
		return nil, 0, err
	}
	if state.ID != STATE_AUTHENTICATED {
		if state.ID == STATE_NOT_AUTHENTICATED {
			return nil, 0, ErrFailedAuth
		}
		return nil, 0, errors.New(state.Info)
	}

	//throw list of tags we need
	tagReq.Tags = tags
	tagReq.Count = uint32(len(tags))
	if err = tagReq.Write(conn); err != nil {
		return nil, 0, err
	}

	//Check list
	if err := tagResp.Read(conn); err != nil {
		return nil, 0, err
	}
	// Make sure the tags were ok
	if tagResp.Count == 0 {
		// We passed an invalid tag
		return nil, 0, ErrFailedTagNegotiation
	}

	//Throw "we're hot" message
	state.ID = STATE_HOT
	state.Info = ""
	if err = state.Write(conn); err != nil {
		return nil, 0, err
	}
	//ok, we are good to go
	return tagResp.Tags, chal.Version, nil
}

func checkTags(tags []string) error {
	for i := range tags {
		if err := CheckTag(tags[i]); err != nil {
			return err
		}
	}
	return nil
}
