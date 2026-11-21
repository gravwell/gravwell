/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mqtt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/stretchr/testify/require"
)

// errRuntime wraps hosted.Mock to inject a specific write error. Mock can only
// fail with a generic error, and the retry logic turns on telling a permanent
// failure from a transient one.
type errRuntime struct {
	*hosted.Mock
	mu     sync.Mutex
	err    error
	failN  int
	writes int
}

func (e *errRuntime) Write(ent entry.Entry) error {
	e.mu.Lock()
	e.writes++
	fail := e.failN > 0
	if fail {
		e.failN--
	}
	err := e.err
	e.mu.Unlock()
	if fail {
		return errors.New("transient write failure")
	}
	if err != nil {
		return err
	}
	return e.Mock.Write(ent)
}

func (e *errRuntime) writeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.writes
}

// fakeAcker records Ack calls.
type fakeAcker struct {
	mu   sync.Mutex
	acks []*paho.Publish
}

func (fa *fakeAcker) Ack(p *paho.Publish) error {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	fa.acks = append(fa.acks, p)
	return nil
}

func (fa *fakeAcker) count() int {
	fa.mu.Lock()
	defer fa.mu.Unlock()
	return len(fa.acks)
}

type harness struct {
	sub *subscriber
	rt  *errRuntime
	fa  *fakeAcker
	cf  context.CancelFunc
}

// newHarness builds a subscriber over hosted.NewMock. routes are raw
// Topic-Route strings so the real parsing and negotiation path is exercised.
func newHarness(t *testing.T, mutate func(*Config), routes ...string) *harness {
	t.Helper()
	conf := Config{
		Broker:      []string{`mqtt://127.0.0.1:1883`},
		Topic:       []string{`a/#`},
		Topic_Route: routes,
	}
	conf.Tag_Name = `default`
	conf.Ingester_UUID = `7db1995f-ffa3-4f0b-b5f8-1516459d100a`
	if mutate != nil {
		mutate(&conf)
	}
	require.NoError(t, conf.Verify())

	ctx, cf := context.WithCancel(context.Background())
	t.Cleanup(cf)
	rt := &errRuntime{Mock: hosted.NewMock(ctx)}

	ing, err := New(conf, `test`, rt)
	require.NoError(t, err)

	tg, err := conf.newTimeGrinder()
	require.NoError(t, err)

	return &harness{
		sub: &subscriber{Ingester: ing, ctx: ctx, rt: rt, tg: tg},
		rt:  rt,
		fa:  &fakeAcker{},
		cf:  cf,
	}
}

func pub(topic, payload string, qos byte) *paho.Publish {
	return &paho.Publish{Topic: topic, Payload: []byte(payload), QoS: qos}
}

func TestHandlePublishWritesAndAcks(t *testing.T) {
	h := newHarness(t, nil)
	handled, err := h.sub.handlePublish(pub(`a/b`, `{"temp":22}`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)

	ents := h.rt.Entries()
	require.Len(t, ents, 1)
	require.Equal(t, []byte(`{"temp":22}`), ents[0].Data)
	require.Equal(t, 1, h.fa.count())
}

// A transient write failure must be retried, not returned. Returning would
// leave the packet unacknowledged, and paho acks in arrival order, so it would
// block every PUBACK behind it and stall the subscription.
func TestHandlePublishRetriesTransientFailure(t *testing.T) {
	h := newHarness(t, nil)
	h.rt.failN = 2

	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, 3, h.rt.writeCount(), "two failures then a success")
	require.Len(t, h.rt.Entries(), 1)
	require.Equal(t, 1, h.fa.count())
}

// An entry that can never be written is dropped and acked, otherwise a single
// poison message wedges the subscription forever.
func TestHandlePublishDropsPermanentFailure(t *testing.T) {
	for _, permErr := range []error{ingest.ErrOversizedEntry, ingest.ErrUnknownTag} {
		t.Run(permErr.Error(), func(t *testing.T) {
			h := newHarness(t, nil)
			h.rt.err = permErr

			handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
			require.True(t, handled)
			require.NoError(t, err)
			require.Empty(t, h.rt.Entries())
			require.Equal(t, 1, h.rt.writeCount(), "a permanent failure is not retried")
			require.Equal(t, 1, h.fa.count(), "dropped so the subscription keeps moving")
		})
	}
}

// A write still failing at shutdown must NOT be acked, so the broker redelivers.
func TestHandlePublishNoAckOnShutdown(t *testing.T) {
	h := newHarness(t, nil)
	h.rt.failN = 1 << 30 // never succeeds

	done := make(chan error, 1)
	go func() {
		_, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
		done <- err
	}()

	time.Sleep(250 * time.Millisecond)
	h.cf()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not unwind on shutdown")
	}
	require.Empty(t, h.rt.Entries())
	require.Zero(t, h.fa.count(), "an unwritten message must not be acknowledged")
}

func TestHandlePublishQoS0NotAcked(t *testing.T) {
	h := newHarness(t, nil)
	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 0), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Len(t, h.rt.Entries(), 1)
	require.Zero(t, h.fa.count(), "QoS 0 carries no acknowledgement")
}

func TestMetadataEnumeratedValues(t *testing.T) {
	h := newHarness(t, nil)
	p := pub(`a/sensor/temp`, `payload`, 2)
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
	ents := h.rt.Entries()
	require.Len(t, ents, 1)

	v, ok := ents[0].GetEnumeratedValue(evTopic)
	require.True(t, ok)
	require.Equal(t, `a/sensor/temp`, v, "a user property must not shadow the real topic")

	v, ok = ents[0].GetEnumeratedValue(evRetain)
	require.True(t, ok)
	require.Equal(t, true, v)

	v, ok = ents[0].GetEnumeratedValue(evContentType)
	require.True(t, ok)
	require.Equal(t, `application/json`, v)

	v, ok = ents[0].GetEnumeratedValue(`device`)
	require.True(t, ok)
	require.Equal(t, `sensor-7`, v)
}

func TestMetadataDisabled(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Disable_Metadata = true })
	_, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.NoError(t, err)
	ents := h.rt.Entries()
	require.Len(t, ents, 1)
	require.Empty(t, ents[0].EnumeratedValues())
}

func TestTagRouting(t *testing.T) {
	h := newHarness(t, nil,
		`a/line1/*:mqttline1`,
		`a/*/heartbeat:`,
	)
	// tags were negotiated through the Mock, resolve what they became
	line1, err := h.rt.NegotiateTag(`mqttline1`)
	require.NoError(t, err)
	def, err := h.rt.NegotiateTag(`default`)
	require.NoError(t, err)

	tests := []struct {
		topic string
		tag   entry.EntryTag
		drop  bool
	}{
		{topic: `a/line1/temp`, tag: line1},
		{topic: `a/line9/heartbeat`, drop: true},
		{topic: `a/line2/temp`, tag: def},
		// "*" must not cross a topic level, so this falls through to default
		{topic: `a/line1/sub/temp`, tag: def},
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
	h := newHarness(t, nil, `a/*:`)
	handled, err := h.sub.handlePublish(pub(`a/b`, `data`, 1), h.fa)
	require.True(t, handled)
	require.NoError(t, err)
	require.Empty(t, h.rt.Entries(), "dropped messages must not be written")
	require.Equal(t, 1, h.fa.count(), "dropped messages are still acknowledged")
}

func TestTimestampExtraction(t *testing.T) {
	payload := `2021-03-04T05:06:07Z some log line`

	t.Run("default is arrival time", func(t *testing.T) {
		h := newHarness(t, nil)
		before := entry.Now()
		_, err := h.sub.handlePublish(pub(`a/b`, payload, 1), h.fa)
		require.NoError(t, err)
		ents := h.rt.Entries()
		require.Len(t, ents, 1)
		require.False(t, ents[0].TS.Before(before))
	})

	t.Run("extract pulls from the payload", func(t *testing.T) {
		h := newHarness(t, func(c *Config) { c.Extract_Timestamps = true })
		_, err := h.sub.handlePublish(pub(`a/b`, payload, 1), h.fa)
		require.NoError(t, err)
		ents := h.rt.Entries()
		require.Len(t, ents, 1)
		want := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
		require.Equal(t, want, ents[0].TS.StandardTime().UTC())
	})
}

// A panic on a paho-owned goroutine would take down the hosted runner and
// every other plugin sharing the process, so the handler has to contain it.
func TestHandlePanicIsContained(t *testing.T) {
	h := newHarness(t, nil)
	require.NotPanics(t, func() {
		h.sub.guard("test", func() { panic("boom") })
	})
	require.NotPanics(t, func() {
		_, _ = h.sub.handle(paho.PublishReceived{Packet: pub(`a/b`, `data`, 1)})
	})
	require.Len(t, h.rt.Entries(), 1, "a normal message still goes through the guard")
}

// panicRuntime panics on the first write, to drive the handler's recover.
type panicRuntime struct {
	*hosted.Mock
	panicked bool
}

func (p *panicRuntime) Write(e entry.Entry) error {
	if !p.panicked {
		p.panicked = true
		panic("boom from the write path")
	}
	return p.Mock.Write(e)
}

// A recovered panic MUST still acknowledge the message. Unwinding without an
// ack leaves the packet at the head of paho's ack tracker, which blocks every
// acknowledgement behind it until the subscription stalls - the exact failure
// the retry logic exists to avoid.
func TestHandlePanicStillAcknowledges(t *testing.T) {
	conf := Config{
		Broker: []string{`mqtt://127.0.0.1:1883`},
		Topic:  []string{`a/#`},
	}
	conf.Tag_Name = `default`
	conf.Ingester_UUID = testUUID
	require.NoError(t, conf.Verify())

	ctx, cf := context.WithCancel(context.Background())
	t.Cleanup(cf)
	rt := &panicRuntime{Mock: hosted.NewMock(ctx)}
	ing, err := New(conf, `test`, rt)
	require.NoError(t, err)
	sub := &subscriber{Ingester: ing, ctx: ctx, rt: rt}
	fa := &fakeAcker{}

	var handled bool
	require.NotPanics(t, func() {
		handled, err = sub.handleGuarded(pub(`a/b`, `data`, 1), fa)
	})
	require.True(t, handled)
	require.NoError(t, err)
	require.True(t, rt.panicked, "the write path should have panicked")
	require.Equal(t, 1, fa.count(), "a panicking message must still be acked or the subscription wedges")
}

func TestNewNegotiatesEveryRouteTag(t *testing.T) {
	h := newHarness(t, nil, `a/x/*:routed1`, `a/y/*:routed2`, `a/z/*:`)
	require.Len(t, h.sub.routes, 3)
	require.False(t, h.sub.routes[0].drop)
	require.NotZero(t, h.sub.routes[0].tag)
	require.True(t, h.sub.routes[2].drop)
}

func TestNewFailsOnTagNegotiationError(t *testing.T) {
	conf := Config{
		Broker: []string{`mqtt://127.0.0.1:1883`},
		Topic:  []string{`a/#`},
	}
	conf.Tag_Name = `default`
	conf.Ingester_UUID = `7db1995f-ffa3-4f0b-b5f8-1516459d100a`
	require.NoError(t, conf.Verify())

	rt := hosted.NewMock(context.Background())
	rt.NegotiateErr = errors.New("nope")
	_, err := New(conf, `test`, rt)
	require.Error(t, err)
}

func TestInspectSuback(t *testing.T) {
	topics := []string{`a/#`, `b/#`, `c/#`}
	tests := []struct {
		name      string
		sa        *paho.Suback
		granted   int
		rejected  int
		permanent bool
		contains  string
	}{
		{name: `nil suback`, sa: nil},
		{name: `all granted`, sa: &paho.Suback{Reasons: []byte{0x00, 0x01, 0x02}}, granted: 3},
		{
			name:    `one denied, rest granted`,
			sa:      &paho.Suback{Reasons: []byte{0x00, 0x87, 0x01}},
			granted: 2, rejected: 1, permanent: true,
			contains: `b/#(0x87 not authorized)`,
		},
		{name: `all denied permanently`, sa: &paho.Suback{Reasons: []byte{0x87, 0x8f, 0xa2}}, rejected: 3, permanent: true},
		{name: `quota exceeded is transient`, sa: &paho.Suback{Reasons: []byte{0x97}}, rejected: 1},
		{name: `mixed is not permanent`, sa: &paho.Suback{Reasons: []byte{0x87, 0x97}}, rejected: 2},
		{
			name:     `more reasons than topics does not panic`,
			sa:       &paho.Suback{Reasons: []byte{0x87, 0x87, 0x87, 0x87}},
			rejected: 4, permanent: true, contains: `?(0x87 not authorized)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			granted, rejected, permanent := inspectSuback(tt.sa, topics)
			require.Equal(t, tt.granted, granted)
			require.Len(t, rejected, tt.rejected)
			require.Equal(t, tt.permanent, permanent)
			if tt.contains != `` {
				require.Contains(t, rejected, tt.contains)
			}
		})
	}
}
