/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"context"
	"encoding/binary"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// testRuntime is a minimal in-memory implementation of hosted.Runtime for the
// wiz plugin tests.
type testRuntime struct {
	ctx context.Context

	mu      sync.Mutex
	store   map[string][]byte
	entries []entry.Entry
	tags    map[string]entry.EntryTag
	nextTag entry.EntryTag
}

func newTestRuntime(ctx context.Context) *testRuntime {
	return &testRuntime{
		ctx:   ctx,
		store: make(map[string][]byte),
		tags:  make(map[string]entry.EntryTag),
	}
}

func (r *testRuntime) Alive() bool              { return r.ctx.Err() == nil }
func (r *testRuntime) Sleep(time.Duration) bool { return r.ctx.Err() != nil }
func (r *testRuntime) Context() context.Context { return r.ctx }

func (r *testRuntime) Get(key string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[key]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return append([]byte(nil), v...), nil
}

func (r *testRuntime) Put(key string, value []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[key] = append([]byte(nil), value...)
	return nil
}

func (r *testRuntime) GetString(key string) (string, error) {
	v, err := r.Get(key)
	return string(v), err
}
func (r *testRuntime) PutString(key, value string) error { return r.Put(key, []byte(value)) }

func (r *testRuntime) GetInt64(key string) (int64, error) {
	v, err := r.Get(key)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(v)), nil
}

func (r *testRuntime) PutInt64(key string, value int64) error {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(value))
	return r.Put(key, b)
}

func (r *testRuntime) GetTime(key string) (time.Time, error) {
	v, err := r.Get(key)
	if err != nil {
		return time.Time{}, err
	}
	var t time.Time
	if err := t.UnmarshalBinary(v); err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (r *testRuntime) PutTime(key string, value time.Time) error {
	b, err := value.MarshalBinary()
	if err != nil {
		return err
	}
	return r.Put(key, b)
}

func (r *testRuntime) Write(e entry.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return nil
}

func (r *testRuntime) NegotiateTag(name string) (entry.EntryTag, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tags[name]; ok {
		return t, nil
	}
	r.nextTag++
	r.tags[name] = r.nextTag
	return r.nextTag, nil
}

func (r *testRuntime) entryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *testRuntime) snapshot() []entry.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]entry.Entry(nil), r.entries...)
}

func (r *testRuntime) tagFor(name string) (entry.EntryTag, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tags[name]
	return t, ok
}

func (r *testRuntime) Debug(string, ...rfc5424.SDParam)    {}
func (r *testRuntime) Info(string, ...rfc5424.SDParam)     {}
func (r *testRuntime) Warn(string, ...rfc5424.SDParam)     {}
func (r *testRuntime) Error(string, ...rfc5424.SDParam)    {}
func (r *testRuntime) Critical(string, ...rfc5424.SDParam) {}
