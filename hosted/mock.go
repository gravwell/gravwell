/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hosted

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/entry"
)

// Mock is a full in-memory implementation of Runtime for use in plugin tests, so each plugin
// package doesn't need to hand-roll its own. Construct with NewMock, optionally set WriteErrs
// or NegotiateErr to inject failures, then call Entries (and Storage getters) to inspect what
// the plugin under test did.
var _ Runtime = (*Mock)(nil)

type Mock struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	entries []entry.Entry
	store   map[string][]byte
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag

	// WriteErrs is the number of subsequent Write calls that should fail.
	WriteErrs int
	// NegotiateErr, if set, is returned by every call to NegotiateTag.
	NegotiateErr error
}

// NewMock returns a ready-to-use Mock. Canceling ctx (or calling the returned Mock's internal
// cancel, exposed indirectly via Context().Done()) stops any in-progress Sleep call.
func NewMock(ctx context.Context) *Mock {
	ctx, cancel := context.WithCancel(ctx)
	return &Mock{
		ctx:    ctx,
		cancel: cancel,
		store:  make(map[string][]byte),
		tags:   make(map[string]entry.EntryTag),
	}
}

func (m *Mock) Alive() bool              { return true }
func (m *Mock) Context() context.Context { return m.ctx }
func (m *Mock) Sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return false
	case <-m.ctx.Done():
		return true
	}
}

func (m *Mock) Debug(_ string, _ ...rfc5424.SDParam)    {}
func (m *Mock) Info(_ string, _ ...rfc5424.SDParam)     {}
func (m *Mock) Warn(_ string, _ ...rfc5424.SDParam)     {}
func (m *Mock) Error(_ string, _ ...rfc5424.SDParam)    {}
func (m *Mock) Critical(_ string, _ ...rfc5424.SDParam) {}

func (m *Mock) Write(e entry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteErrs > 0 {
		m.WriteErrs--
		return errors.New("write failed")
	}
	m.entries = append(m.entries, e)
	return nil
}

func (m *Mock) NegotiateTag(name string) (entry.EntryTag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.NegotiateErr != nil {
		return 0, m.NegotiateErr
	}
	if t, ok := m.tags[name]; ok {
		return t, nil
	}
	m.nextTag++
	m.tags[name] = m.nextTag
	return m.nextTag, nil
}

func (m *Mock) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return v, nil
}

func (m *Mock) Put(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}

func (m *Mock) GetString(key string) (string, error) {
	v, err := m.Get(key)
	return string(v), err
}

func (m *Mock) PutString(key, value string) error { return m.Put(key, []byte(value)) }
func (m *Mock) GetInt64(_ string) (int64, error)  { return 0, storage.ErrStorageNotFound }
func (m *Mock) PutInt64(_ string, _ int64) error  { return nil }

func (m *Mock) GetTime(key string) (time.Time, error) {
	v, err := m.GetString(key)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}

func (m *Mock) PutTime(key string, value time.Time) error {
	return m.PutString(key, value.Format(time.RFC3339Nano))
}

// Entries returns a snapshot of every entry.Entry written so far, in write order.
func (m *Mock) Entries() []entry.Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]entry.Entry(nil), m.entries...)
}
