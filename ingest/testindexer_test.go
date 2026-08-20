/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// testIndexer is a minimal in-process stand-in for an indexer's ingest
// listener.  It speaks the real ingest protocol using only the reader side
// that already lives in this package (auth.go and entryReader.go), which means
// it exercises the same handshake, ack, and keepalive machinery that a real
// indexer does.  It exists so that muxer level behavior can be tested without
// standing up any part of the backend.
//
// The interesting knobs are the read gate and the read throttle:
//
//	hold()/release()  stop and restart the indexer consuming entries, which
//	                  lets a test build a known backlog in the muxer pipeline
//	throttle(d)       sleep d between entries, which lets a test control how
//	                  long that backlog takes to drain
type testIndexer struct {
	//these are accessed atomically and must stay at the top of the struct so
	//they land on an 8 byte boundary, otherwise 32bit architectures panic
	throttleNS int64  // nanoseconds to sleep per entry
	entries    uint64 // total entries consumed
	conns      int64  // ingest connections currently attached

	lst    net.Listener
	secret string

	gate readGate

	tagMtx  sync.Mutex
	tags    map[string]entry.EntryTag
	tagNext entry.EntryTag

	wg   sync.WaitGroup
	done chan struct{}

	errMtx sync.Mutex
	errs   []error
}

// newTestIndexer starts a test indexer bound to a random loopback port.
// Callers must call Close when finished.
func newTestIndexer(secret string) (*testIndexer, error) {
	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ti := &testIndexer{
		lst:    lst,
		secret: secret,
		tags:   map[string]entry.EntryTag{},
		done:   make(chan struct{}),
	}
	// tag zero is always the default tag
	ti.tags[entry.DefaultTagName] = 0
	ti.tagNext = 1

	ti.wg.Add(1)
	go ti.acceptRoutine()
	return ti, nil
}

// Destination is the muxer destination string pointing at this indexer.
func (ti *testIndexer) Destination() string {
	return `tcp://` + ti.lst.Addr().String()
}

// Entries is the total count of entries this indexer has consumed.
func (ti *testIndexer) Entries() uint64 {
	return atomic.LoadUint64(&ti.entries)
}

// Conns is the number of ingest connections currently attached.
func (ti *testIndexer) Conns() int64 {
	return atomic.LoadInt64(&ti.conns)
}

// hold stops the indexer from consuming entries.  Entries already in flight
// stay in the socket and in the muxer pipeline.
func (ti *testIndexer) hold() {
	ti.gate.hold()
}

// release lets the indexer start consuming entries again.
func (ti *testIndexer) release() {
	ti.gate.release()
}

// throttle sets a per entry sleep, which caps how fast the indexer will drain
// a backlog.  A zero duration removes the throttle.
func (ti *testIndexer) throttle(d time.Duration) {
	atomic.StoreInt64(&ti.throttleNS, int64(d))
}

func (ti *testIndexer) Close() error {
	select {
	case <-ti.done:
		return errors.New("already closed")
	default:
	}
	close(ti.done)
	err := ti.lst.Close()
	ti.gate.release() //make sure nothing is parked on the gate
	ti.wg.Wait()
	return err
}

// Errors returns any errors hit by connection handlers.  Connection teardown
// at the end of a test is normal, so callers generally only consult this when
// something else has already gone wrong.
func (ti *testIndexer) Errors() []error {
	ti.errMtx.Lock()
	defer ti.errMtx.Unlock()
	return append([]error(nil), ti.errs...)
}

func (ti *testIndexer) addError(err error) {
	if err == nil || err == io.EOF {
		return
	}
	select {
	case <-ti.done:
		return //we are shutting down, errors are expected
	default:
	}
	ti.errMtx.Lock()
	ti.errs = append(ti.errs, err)
	ti.errMtx.Unlock()
}

func (ti *testIndexer) acceptRoutine() {
	defer ti.wg.Done()
	for {
		conn, err := ti.lst.Accept()
		if err != nil {
			select {
			case <-ti.done:
			default:
				ti.addError(err)
			}
			return
		}
		ti.wg.Add(1)
		go func(c net.Conn) {
			defer ti.wg.Done()
			defer c.Close()
			if err := ti.handleConn(c); err != nil {
				ti.addError(err)
			}
		}(conn)
	}
}

func (ti *testIndexer) handleConn(conn net.Conn) error {
	if err := ti.authenticate(conn); err != nil {
		return fmt.Errorf("authentication failed %w", err)
	}

	rdr, err := NewEntryReaderEx(EntryReaderWriterConfig{
		Conn:                  conn,
		OutstandingEntryCount: MAX_UNCONFIRMED_COUNT,
		BufferSize:            READ_BUFFER_SIZE,
		TagMan:                testTagManager{ti},
	})
	if err != nil {
		return err
	}
	defer rdr.Close()

	// The ack routine has to be running before the identification exchange,
	// the responses to those commands go out through the ack channel.
	if err = rdr.Start(); err != nil {
		return err
	}
	if err = rdr.SetupConnection(); err != nil {
		return fmt.Errorf("SetupConnection failed %w", err)
	}
	if err = rdr.IngestOK(true); err != nil {
		return fmt.Errorf("IngestOK failed %w", err)
	}
	if err = rdr.ConfigureStream(); err != nil {
		return fmt.Errorf("ConfigureStream failed %w", err)
	}

	atomic.AddInt64(&ti.conns, 1)
	defer atomic.AddInt64(&ti.conns, -1)

	for {
		select {
		case <-ti.done:
			return nil
		default:
		}
		// the gate is checked before every read so a held indexer leaves
		// entries sitting in the socket and in the muxer pipeline
		ti.gate.wait(ti.done)
		if _, err = rdr.Read(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		atomic.AddUint64(&ti.entries, 1)
		if d := atomic.LoadInt64(&ti.throttleNS); d > 0 {
			time.Sleep(time.Duration(d))
		}
	}
}

func (ti *testIndexer) authenticate(conn net.Conn) error {
	hash, err := GenAuthHash(ti.secret)
	if err != nil {
		return err
	}
	chal, err := NewChallenge(hash)
	if err != nil {
		return err
	}
	if err = chal.Write(conn); err != nil {
		return err
	}

	var resp ChallengeResponse
	if err = resp.Read(conn); err != nil {
		return err
	}
	if err = VerifyResponse(hash, chal, resp); err != nil {
		bad := StateResponse{ID: STATE_NOT_AUTHENTICATED, Info: err.Error()}
		bad.Write(conn)
		return err
	}
	good := StateResponse{ID: STATE_AUTHENTICATED}
	if err = good.Write(conn); err != nil {
		return err
	}

	var req TagRequest
	if err = req.Read(conn); err != nil {
		return err
	}
	tresp := TagResponse{Tags: map[string]entry.EntryTag{}}
	for _, name := range req.Tags {
		tg, err := ti.tagID(name)
		if err != nil {
			return err
		}
		tresp.Tags[name] = tg
	}
	tresp.Count = uint32(len(tresp.Tags))
	if err = tresp.Write(conn); err != nil {
		return err
	}

	var hot StateResponse
	if err = hot.Read(conn); err != nil {
		return err
	}
	if hot.ID != STATE_HOT {
		return fmt.Errorf("ingester did not go hot, got state %x", hot.ID)
	}
	return nil
}

// tagID hands back the indexer side tag value for a name, minting a new one
// if we have not seen it before.
func (ti *testIndexer) tagID(name string) (entry.EntryTag, error) {
	if err := CheckTag(name); err != nil {
		return 0, err
	}
	ti.tagMtx.Lock()
	defer ti.tagMtx.Unlock()
	if tg, ok := ti.tags[name]; ok {
		return tg, nil
	}
	if ti.tagNext >= entry.MaxTagId {
		return 0, ErrTooManyTags
	}
	tg := ti.tagNext
	ti.tagNext++
	ti.tags[name] = tg
	return tg, nil
}

// testTagManager satisfies the TagManager interface so the entry reader can
// negotiate tags on the fly.
type testTagManager struct {
	ti *testIndexer
}

func (ttm testTagManager) GetAndPopulate(name string) (entry.EntryTag, error) {
	return ttm.ti.tagID(name)
}

// readGate is a simple resumable stop signal.  A held gate parks callers of
// wait until it is released.
type readGate struct {
	mtx sync.Mutex
	ch  chan struct{}
}

func (rg *readGate) hold() {
	rg.mtx.Lock()
	if rg.ch == nil {
		rg.ch = make(chan struct{})
	}
	rg.mtx.Unlock()
}

func (rg *readGate) release() {
	rg.mtx.Lock()
	if rg.ch != nil {
		close(rg.ch)
		rg.ch = nil
	}
	rg.mtx.Unlock()
}

func (rg *readGate) wait(abort <-chan struct{}) {
	rg.mtx.Lock()
	ch := rg.ch
	rg.mtx.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-abort:
	}
}
