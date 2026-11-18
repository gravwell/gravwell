/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/stretchr/testify/require"
)

var errTestWrite = errors.New("test write failure")

// fakeWriter collects entries written by the processor set and hands out tags.
type fakeWriter struct {
	mtx    sync.Mutex
	ents   []*entry.Entry
	tags   map[string]entry.EntryTag
	err    error
	failN  int // fail this many writes, then start succeeding
	writes int
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{tags: map[string]entry.EntryTag{}}
}

func (fw *fakeWriter) NegotiateTag(name string) (entry.EntryTag, error) {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	if tg, ok := fw.tags[name]; ok {
		return tg, nil
	}
	tg := entry.EntryTag(len(fw.tags) + 1)
	fw.tags[name] = tg
	return tg, nil
}

func (fw *fakeWriter) WriteEntry(e *entry.Entry) error {
	return fw.WriteEntryContext(context.Background(), e)
}

func (fw *fakeWriter) WriteEntryContext(_ context.Context, e *entry.Entry) error {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	fw.writes++
	if fw.failN > 0 {
		fw.failN--
		return errTestWrite
	}
	if fw.err != nil {
		return fw.err
	}
	fw.ents = append(fw.ents, e)
	return nil
}

func (fw *fakeWriter) writeCount() int {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	return fw.writes
}

func (fw *fakeWriter) WriteBatch(ents []*entry.Entry) error {
	return fw.WriteBatchContext(context.Background(), ents)
}

func (fw *fakeWriter) WriteBatchContext(ctx context.Context, ents []*entry.Entry) error {
	for _, e := range ents {
		if err := fw.WriteEntryContext(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (fw *fakeWriter) entries() []*entry.Entry {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	return append([]*entry.Entry(nil), fw.ents...)
}

// fakeAcker records Ack calls. The embedded interface means any method we did
// not expect to be called panics instead of silently succeeding.
type fakeAcker struct {
	mtx  sync.Mutex
	acks []*paho.Publish
	err  error
}

func (fa *fakeAcker) Ack(p *paho.Publish) error {
	fa.mtx.Lock()
	defer fa.mtx.Unlock()
	if fa.err != nil {
		return fa.err
	}
	fa.acks = append(fa.acks, p)
	return nil
}

func (fa *fakeAcker) count() int {
	fa.mtx.Lock()
	defer fa.mtx.Unlock()
	return len(fa.acks)
}

// fakeSyncer records SyncContext calls.
type fakeSyncer struct {
	calls int
	err   error
	failN int
}

func (fs *fakeSyncer) SyncContext(ctx context.Context, to time.Duration) error {
	fs.calls++
	if fs.failN > 0 {
		fs.failN--
		return errors.New("transient sync failure")
	}
	return fs.err
}

type testHarness struct {
	sub *mqttSubscriber
	fw  *fakeWriter
	fa  *fakeAcker
	fs  *fakeSyncer
}

func newTestHarness(t *testing.T, sc *subCfg, routes ...string) *testHarness {
	t.Helper()
	if sc == nil {
		sc = &subCfg{name: `test`, attachMeta: true}
	}
	fw := newFakeWriter()
	proc := processors.NewProcessorSet(fw)

	defTag, err := fw.NegotiateTag(`default`)
	require.NoError(t, err)

	var rts []runtimeRoute
	for _, r := range routes {
		cs := ConfigSubscription{Topic_Route: []string{r}}
		parsed, err := cs.topicRoutes()
		require.NoError(t, err)
		require.Len(t, parsed, 1)
		rt := runtimeRoute{pattern: parsed[0].pattern, glob: parsed[0].glob, drop: parsed[0].drop}
		if !rt.drop {
			rt.tag, err = fw.NegotiateTag(parsed[0].tag)
			require.NoError(t, err)
		}
		rts = append(rts, rt)
	}

	fs := &fakeSyncer{}
	sub, err := newSubscriber(subscriberConfig{
		cfg:    sc,
		igst:   fs,
		proc:   proc,
		defTag: defTag,
		routes: rts,
		src:    net.ParseIP(`10.0.0.1`),
		lg:     log.NewDiscardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { sub.cf() })
	return &testHarness{sub: sub, fw: fw, fa: &fakeAcker{}, fs: fs}
}

func pub(topic string, payload string, qos byte) *paho.Publish {
	return &paho.Publish{Topic: topic, Payload: []byte(payload), QoS: qos}
}

func evOf(t *testing.T, ent *entry.Entry, name string) (interface{}, bool) {
	t.Helper()
	return ent.GetEnumeratedValue(name)
}

func TestHandlePublishWritesAndAcks(t *testing.T) {
	h := newTestHarness(t, nil)
	p := pub(`factory/line1/temp`, `{"temp":22}`, 1)

	handled, err := h.sub.handlePublish(p, h.fa)
	require.True(t, handled)
	require.NoError(t, err)

	ents := h.fw.entries()
	require.Len(t, ents, 1)
	// the payload must survive byte for byte
	require.Equal(t, []byte(`{"temp":22}`), ents[0].Data)
	require.Equal(t, net.ParseIP(`10.0.0.1`), net.IP(ents[0].SRC))
	require.Equal(t, 1, h.fa.count(), "exactly one ack expected")
}

// A transient write failure must be retried rather than returned. Returning
// would leave the packet unacknowledged, and paho acks strictly in arrival
// order, so it would block every PUBACK behind it and stall the subscription.
func TestHandlePublishRetriesTransientWriteFailure(t *testing.T) {
	h := newTestHarness(t, nil)
	h.fw.failN = 2

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, 3, h.fw.writeCount(), "two failures then a success")
	require.Len(t, h.fw.entries(), 1)
	require.Equal(t, 1, h.fa.count(), "the message is acked once it lands")
}

// An entry that can never be written is dropped and acked. Retrying it forever
// would wedge the subscription on a single poison message.
func TestHandlePublishDropsPermanentFailure(t *testing.T) {
	h := newTestHarness(t, nil)
	h.fw.err = ingest.ErrOversizedEntry

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Empty(t, h.fw.entries())
	require.Equal(t, 1, h.fa.count(), "a poison entry is acked so the subscription keeps moving")
}

// A write that is still failing when we shut down must NOT be acked, so the
// broker redelivers it.
func TestHandlePublishNoAckOnShutdown(t *testing.T) {
	h := newTestHarness(t, nil)
	h.fw.err = errTestWrite

	done := make(chan error, 1)
	go func() {
		_, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
		done <- err
	}()

	time.Sleep(250 * time.Millisecond) // let it fail and enter the retry loop
	h.sub.cf()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not unwind on shutdown")
	}
	require.Empty(t, h.fw.entries())
	require.Zero(t, h.fa.count(), "an unwritten message must not be acknowledged")
}

func TestHandlePublishRetriesSyncFailure(t *testing.T) {
	sc := &subCfg{name: `test`, attachMeta: true, sync: true}
	h := newTestHarness(t, sc)
	h.fs.failN = 2

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, 3, h.fs.calls)
	require.Len(t, h.fw.entries(), 1, "the entry is written once, only the sync is retried")
	require.Equal(t, 1, h.fa.count())
}

func TestHandlePublishQoS0NotAcked(t *testing.T) {
	h := newTestHarness(t, nil)
	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 0), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Len(t, h.fw.entries(), 1)
	require.Zero(t, h.fa.count(), "QoS 0 carries no acknowledgement")
}

func TestHandlePublishDeadContext(t *testing.T) {
	h := newTestHarness(t, nil)
	h.sub.cf() // cancel

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, h.fw.entries())
	require.Zero(t, h.fa.count())
}

func TestHandlePublishNilPacket(t *testing.T) {
	h := newTestHarness(t, nil)
	handled, err := h.sub.handlePublish(nil, h.fa)
	require.False(t, handled)
	require.NoError(t, err)
}

func TestMetadataEnumeratedValues(t *testing.T) {
	h := newTestHarness(t, nil)
	p := pub(`factory/line1/temp`, `payload`, 2)
	p.Retain = true
	p.Properties = &paho.PublishProperties{
		ContentType: `application/json`,
		User: paho.UserProperties{
			{Key: `device`, Value: `sensor-7`},
			{Key: `topic`, Value: `spoofed`}, // reserved, must be skipped
		},
	}

	_, err := h.sub.handlePublish(p, h.fa)
	require.NoError(t, err)
	ents := h.fw.entries()
	require.Len(t, ents, 1)

	v, ok := evOf(t, ents[0], evTopic)
	require.True(t, ok)
	require.Equal(t, `factory/line1/temp`, v, "a user property must not shadow the real topic")

	v, ok = evOf(t, ents[0], evRetain)
	require.True(t, ok)
	require.Equal(t, true, v)

	v, ok = evOf(t, ents[0], evContentType)
	require.True(t, ok)
	require.Equal(t, `application/json`, v)

	v, ok = evOf(t, ents[0], `device`)
	require.True(t, ok)
	require.Equal(t, `sensor-7`, v)

	_, ok = evOf(t, ents[0], evQoS)
	require.True(t, ok)
}

func TestMetadataDisabled(t *testing.T) {
	h := newTestHarness(t, &subCfg{name: `test`, attachMeta: false})
	_, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.NoError(t, err)
	ents := h.fw.entries()
	require.Len(t, ents, 1)
	_, ok := evOf(t, ents[0], evTopic)
	require.False(t, ok)
	require.Empty(t, ents[0].EnumeratedValues())
}

func TestResolveTagRouting(t *testing.T) {
	h := newTestHarness(t, nil,
		`factory/line1/*:mqttline1`,
		`factory/*/heartbeat:`,
	)
	line1 := h.fw.tags[`mqttline1`]
	def := h.fw.tags[`default`]

	tests := []struct {
		topic string
		tag   entry.EntryTag
		drop  bool
	}{
		{topic: `factory/line1/temp`, tag: line1},
		{topic: `factory/line9/heartbeat`, drop: true},
		{topic: `factory/line2/temp`, tag: def},
		// "*" must not cross a topic level, so this falls through to default
		{topic: `factory/line1/sub/temp`, tag: def},
	}
	for _, tt := range tests {
		tag, drop := h.sub.resolveTag(tt.topic)
		require.Equal(t, tt.drop, drop, tt.topic)
		if !tt.drop {
			require.Equal(t, tt.tag, tag, tt.topic)
		}
	}
}

func TestDroppedMessageIsAckedNotWritten(t *testing.T) {
	h := newTestHarness(t, nil, `a/*:`)

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Empty(t, h.fw.entries(), "dropped messages must not be written")
	require.Equal(t, 1, h.fa.count(), "dropped messages are still acknowledged")
}

func TestTimestampHandling(t *testing.T) {
	payload := `2021-03-04T05:06:07Z some log line`

	t.Run("default is arrival time", func(t *testing.T) {
		h := newTestHarness(t, nil)
		before := entry.Now()
		_, err := h.sub.handlePublish(pub(`a/b`, payload, 1), h.fa)
		require.NoError(t, err)
		ents := h.fw.entries()
		require.Len(t, ents, 1)
		require.False(t, ents[0].TS.Before(before), "entry should be stamped on arrival")
	})

	t.Run("extract pulls from the payload", func(t *testing.T) {
		tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{EnableLeftMostSeed: true})
		require.NoError(t, err)
		sc := &subCfg{name: `test`, attachMeta: true, extractTS: true, tg: tg}
		h := newTestHarness(t, sc)

		_, err = h.sub.handlePublish(pub(`a/b`, payload, 1), h.fa)
		require.NoError(t, err)
		ents := h.fw.entries()
		require.Len(t, ents, 1)
		want := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
		require.Equal(t, want, ents[0].TS.StandardTime().UTC())
	})

	t.Run("ignore beats extract", func(t *testing.T) {
		tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{EnableLeftMostSeed: true})
		require.NoError(t, err)
		sc := &subCfg{name: `test`, attachMeta: true, extractTS: true, ignoreTS: true, tg: tg}
		h := newTestHarness(t, sc)

		before := entry.Now()
		_, err = h.sub.handlePublish(pub(`a/b`, payload, 1), h.fa)
		require.NoError(t, err)
		ents := h.fw.entries()
		require.Len(t, ents, 1)
		require.False(t, ents[0].TS.Before(before))
	})
}

func TestHandleNilClientDoesNotPanic(t *testing.T) {
	h := newTestHarness(t, nil)
	// paho hands us a PublishReceived with a nil Client if the connection died
	handled, err := h.sub.handle(paho.PublishReceived{Packet: pub(`a/b`, `data`, 1)})
	require.True(t, handled)
	require.NoError(t, err)
	require.Len(t, h.fw.entries(), 1)
}
