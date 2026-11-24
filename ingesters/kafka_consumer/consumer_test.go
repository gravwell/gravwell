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
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingest/processors/tags"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/stretchr/testify/require"
)

const testTopic = `testtopic`

var errTestWrite = errors.New("test write failure")

// fakeWriter collects entries written by the processor set and hands out tags.
// It satisfies both tags.TagResolver and the processor set writer interface.
type fakeWriter struct {
	mtx  sync.Mutex
	ents []*entry.Entry
	tags map[string]entry.EntryTag
	err  error
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

func (fw *fakeWriter) tag(name string) entry.EntryTag {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	return fw.tags[name]
}

func (fw *fakeWriter) WriteEntry(e *entry.Entry) error {
	return fw.WriteEntryContext(context.Background(), e)
}

func (fw *fakeWriter) WriteEntryContext(_ context.Context, e *entry.Entry) error {
	fw.mtx.Lock()
	defer fw.mtx.Unlock()
	if fw.err != nil {
		return fw.err
	}
	fw.ents = append(fw.ents, e)
	return nil
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

// fakeSession implements the parts of sarama.ConsumerGroupSession the consumer uses.
// Calling any other method panics on the nil embedded interface, which flags unexpected use.
type fakeSession struct {
	sarama.ConsumerGroupSession
	memberID string
	mtx      sync.Mutex
	marked   []*sarama.ConsumerMessage
}

func (fs *fakeSession) MemberID() string { return fs.memberID }

func (fs *fakeSession) MarkMessage(m *sarama.ConsumerMessage, _ string) {
	fs.mtx.Lock()
	fs.marked = append(fs.marked, m)
	fs.mtx.Unlock()
}

func (fs *fakeSession) markedMessages() []*sarama.ConsumerMessage {
	fs.mtx.Lock()
	defer fs.mtx.Unlock()
	return append([]*sarama.ConsumerMessage(nil), fs.marked...)
}

type fakeClaim struct {
	sarama.ConsumerGroupClaim
	topic string
	ch    chan *sarama.ConsumerMessage
}

func (fc *fakeClaim) Topic() string                            { return fc.topic }
func (fc *fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return fc.ch }

// claimWith returns a claim whose channel already holds msgs and is closed.
func claimWith(topic string, msgs ...*sarama.ConsumerMessage) *fakeClaim {
	ch := make(chan *sarama.ConsumerMessage, len(msgs))
	for _, m := range msgs {
		ch <- m
	}
	close(ch)
	return &fakeClaim{topic: topic, ch: ch}
}

type fakeGroup struct {
	sarama.ConsumerGroup
	calls   atomic.Int32
	consume func(call int32) error
}

func (fg *fakeGroup) Consume(_ context.Context, topics []string, _ sarama.ConsumerGroupHandler) error {
	return fg.consume(fg.calls.Add(1))
}

type fakeCloser struct {
	err    error
	closed atomic.Bool
}

func (fc *fakeCloser) Close() error {
	fc.closed.Store(true)
	return fc.err
}

// newUnstartedMuxer returns a muxer that was never started; it is enough to satisfy
// non-nil checks, and every network-dependent call on it fails immediately.
func newUnstartedMuxer(t *testing.T) *ingest.IngestMuxer {
	t.Helper()
	m, err := ingest.NewUniformMuxer(ingest.UniformMuxerConfig{
		Destinations: []string{`127.0.0.1:4023`},
		Tags:         []string{`default`},
		Auth:         `testing`,
		Logger:       log.NewDiscardLogger(),
	})
	require.NoError(t, err)
	return m
}

// newTestConsumer builds a consumer wired to a fakeWriter; mod can adjust the base config first.
func newTestConsumer(t *testing.T, mod func(*consumerCfg)) (*kafkaConsumer, *fakeWriter) {
	t.Helper()
	cfg := consumerCfg{
		TaggerConfig: tags.TaggerConfig{Tags: []string{`default`, `allowed*`}},
		defTag:       `default`,
		leader:       []string{`127.0.0.1:9092`},
		topic:        testTopic,
		group:        defaultConsumerGroup,
		batchSize:    defaultBatchSize,
		srcKey:       defaultSRCHeader,
		tagKey:       defaultTagHeader,
	}
	if mod != nil {
		mod(&cfg)
	}
	fw := newFakeWriter()
	tgr, err := tags.NewTagger(cfg.TaggerConfig, fw)
	require.NoError(t, err)
	defTag, err := tgr.Negotiate(cfg.defTag)
	require.NoError(t, err)

	kc, err := newKafkaConsumer(kafkaConsumerConfig{
		consumerCfg: cfg,
		name:        `test`,
		defaultTag:  defTag,
		igst:        newUnstartedMuxer(t),
		lg:          log.NewDiscardLogger(),
		pproc:       processors.NewProcessorSet(fw),
		tgr:         tgr,
	})
	require.NoError(t, err)
	t.Cleanup(func() { kc.Close() })
	return kc, fw
}

// requireUnlocked fails the test if kc.mtx is held. A leaked lock is released first so the
// Close registered by newTestConsumer does not hang the test run.
func requireUnlocked(t *testing.T, kc *kafkaConsumer) {
	t.Helper()
	if !kc.mtx.TryLock() {
		kc.mtx.Unlock()
		t.Fatal("kc.mtx is still held after Start returned")
	}
	kc.mtx.Unlock()
}

func msgAt(ts time.Time, val string, hdrs ...*sarama.RecordHeader) *sarama.ConsumerMessage {
	return &sarama.ConsumerMessage{
		Topic:     testTopic,
		Timestamp: ts,
		Value:     []byte(val),
		Headers:   hdrs,
	}
}

func hdr(k, v string) *sarama.RecordHeader {
	return &sarama.RecordHeader{Key: []byte(k), Value: []byte(v)}
}

// ---- closers ----

func TestClosersAddNil(t *testing.T) {
	c := newClosers()
	require.Nil(t, c.add(nil))
	require.Empty(t, c.set)
}

func TestClosersCloseAll(t *testing.T) {
	c := newClosers()
	a := &fakeCloser{}
	b := &fakeCloser{err: errors.New("b failed")}
	d := &fakeCloser{err: errors.New("d failed")}
	for _, v := range []*fakeCloser{a, b, d} {
		require.NotNil(t, c.add(v))
	}

	err := c.Close()
	require.Error(t, err)
	require.Contains(t, err.Error(), "b failed")
	require.Contains(t, err.Error(), "d failed")
	for _, v := range []*fakeCloser{a, b, d} {
		require.True(t, v.closed.Load(), "every closer must be closed even after an error")
	}
	require.Nil(t, c.set)
	require.NoError(t, c.Close(), "closing an empty set should be a no-op")
}

func TestClosersCloseWaitsForRoutines(t *testing.T) {
	c := newClosers()
	wg := c.add(&fakeCloser{})
	wg.Add(1)
	var done atomic.Bool
	go func() {
		time.Sleep(50 * time.Millisecond)
		done.Store(true)
		wg.Done()
	}()
	require.NoError(t, c.Close())
	require.True(t, done.Load(), "Close returned before the consumer routine finished")
}

func TestAppendError(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	require.NoError(t, appendError(nil, nil))
	require.Equal(t, a, appendError(a, nil))
	require.Equal(t, b, appendError(nil, b))
	require.EqualError(t, appendError(a, b), "a b")
}

// ---- construction and lifecycle ----

func TestNewKafkaConsumerValidation(t *testing.T) {
	_, err := newKafkaConsumer(kafkaConsumerConfig{lg: log.NewDiscardLogger()})
	require.Error(t, err, "nil ingest muxer should be rejected")

	_, err = newKafkaConsumer(kafkaConsumerConfig{igst: newUnstartedMuxer(t)})
	require.Error(t, err, "nil logger should be rejected")

	kc, err := newKafkaConsumer(kafkaConsumerConfig{igst: newUnstartedMuxer(t), lg: log.NewDiscardLogger()})
	require.NoError(t, err)
	require.NotNil(t, kc.ctx)
	require.NotNil(t, kc.cf)
	require.False(t, kc.started)
}

func TestCloseConsumer(t *testing.T) {
	var nilKC *kafkaConsumer
	require.Error(t, nilKC.Close())

	kc, _ := newTestConsumer(t, nil)
	require.NoError(t, kc.Close())
	require.Error(t, kc.ctx.Err(), "Close should cancel the consumer context")
	require.Error(t, kc.Close(), "second Close should report the consumer is already closed")
}

func TestStartRejectsDoubleStart(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	kc.started = true
	var wg sync.WaitGroup
	require.Error(t, kc.Start(&wg))
	requireUnlocked(t, kc)
}

func TestStartAfterClose(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	require.NoError(t, kc.Close())
	var wg sync.WaitGroup
	require.Error(t, kc.Start(&wg))
	requireUnlocked(t, kc)
}

func TestStartReleasesLockOnClientError(t *testing.T) {
	// main() calls Close after a failed Start, so the lock must be released on every error path
	// an unknown auth type fails in SetAuth before any network connection is attempted
	kc, _ := newTestConsumer(t, func(c *consumerCfg) {
		c.auth = KafkaAuthConfig{Auth_Type: `bogus`}
	})
	var wg sync.WaitGroup
	require.Error(t, kc.Start(&wg))
	requireUnlocked(t, kc)
}

func TestRoutineExitsOnConsumeError(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	fg := &fakeGroup{consume: func(int32) error { return errors.New("broker gone") }}
	var wg sync.WaitGroup
	wg.Add(1)
	kc.routine(fg, &wg)
	wg.Wait()
	require.EqualValues(t, 1, fg.calls.Load())
}

func TestRoutineRestartsUntilCancelled(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	// a nil return from Consume means a rebalance ended the session, so routine should consume again
	fg := &fakeGroup{consume: func(call int32) error {
		if call == 3 {
			kc.cf()
		}
		return nil
	}}
	var wg sync.WaitGroup
	wg.Add(1)
	kc.routine(fg, &wg)
	wg.Wait()
	require.EqualValues(t, 3, fg.calls.Load())
}

// ---- Setup and Cleanup ----

func TestSetupResetsStateAndReportsMuxerError(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	kc.count, kc.size = 10, 1000
	err := kc.Setup(&fakeSession{memberID: `member-1`})
	require.Error(t, err, "an unstarted muxer never becomes hot")
	require.Equal(t, `member-1`, kc.memberId)
	require.Zero(t, kc.count)
	require.Zero(t, kc.size)
}

func TestCleanupSwallowsSyncErrors(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	// the muxer has no connections, so the sync fails; sarama treats a Cleanup error as fatal,
	// so Cleanup must still return nil
	require.NoError(t, kc.Cleanup(&fakeSession{memberID: `member-1`}))
}

// ---- flush ----

func TestFlushDefaults(t *testing.T) {
	kc, fw := newTestConsumer(t, nil)
	kc.src = net.ParseIP(`10.1.2.3`)
	ts := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	sess := &fakeSession{}
	msgs := []*sarama.ConsumerMessage{msgAt(ts, `hello`), msgAt(ts, `world`)}

	require.NoError(t, kc.flush(sess, msgs))

	ents := fw.entries()
	require.Len(t, ents, 2)
	for i, e := range ents {
		require.Equal(t, msgs[i].Value, e.Data)
		require.Equal(t, entry.FromStandard(ts), e.TS, "kafka timestamp should be used by default")
		require.Equal(t, kc.defaultTag, e.Tag)
		require.True(t, kc.src.Equal(e.SRC))
	}
	require.Equal(t, msgs, sess.markedMessages())
	require.EqualValues(t, 2, kc.count)
	require.EqualValues(t, ents[0].Size()+ents[1].Size(), kc.size)
}

func TestFlushIgnoreTimestamps(t *testing.T) {
	kc, fw := newTestConsumer(t, func(c *consumerCfg) { c.ignoreTS = true })
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Now()
	require.NoError(t, kc.flush(&fakeSession{}, []*sarama.ConsumerMessage{msgAt(old, `x`)}))
	got := fw.entries()[0].TS.StandardTime()
	require.False(t, got.Before(before.Add(-time.Second)), "expected current time, got %v", got)
}

func TestFlushExtractTimestamps(t *testing.T) {
	tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{})
	require.NoError(t, err)
	kc, fw := newTestConsumer(t, func(c *consumerCfg) {
		c.extractTS = true
		c.tg = tg
	})
	kafkaTS := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	dataTS := time.Date(2019, 3, 1, 12, 30, 45, 0, time.UTC)
	msgs := []*sarama.ConsumerMessage{
		msgAt(kafkaTS, dataTS.Format(time.RFC3339)+` something happened`),
		msgAt(kafkaTS, `no timestamp in here`),
	}
	require.NoError(t, kc.flush(&fakeSession{}, msgs))
	ents := fw.entries()
	require.Len(t, ents, 2)
	require.True(t, dataTS.Equal(ents[0].TS.StandardTime()), "got %v", ents[0].TS.StandardTime())
	require.Equal(t, entry.FromStandard(kafkaTS), ents[1].TS, "should fall back to the kafka timestamp")
}

func TestFlushTagHeader(t *testing.T) {
	kc, fw := newTestConsumer(t, nil)
	ts := time.Now()
	msgs := []*sarama.ConsumerMessage{
		msgAt(ts, `a`, hdr(defaultTagHeader, `allowedfoo`)), // matches the allowed* glob
		msgAt(ts, `b`, hdr(defaultTagHeader, `forbidden`)),  // not in the tag list
		msgAt(ts, `c`, hdr(`OTHER`, `allowedbar`)),          // header key doesn't match Tag-Header
	}
	require.NoError(t, kc.flush(&fakeSession{}, msgs))
	ents := fw.entries()
	require.Len(t, ents, 3)
	require.Equal(t, fw.tag(`allowedfoo`), ents[0].Tag)
	require.NotEqual(t, kc.defaultTag, ents[0].Tag)
	require.Equal(t, kc.defaultTag, ents[1].Tag, "disallowed tags fall back to the default tag")
	require.Equal(t, kc.defaultTag, ents[2].Tag)
}

func TestFlushWriteErrorMarksNothing(t *testing.T) {
	kc, fw := newTestConsumer(t, nil)
	fw.err = errTestWrite
	sess := &fakeSession{}
	err := kc.flush(sess, []*sarama.ConsumerMessage{msgAt(time.Now(), `x`)})
	require.ErrorIs(t, err, errTestWrite)
	require.Empty(t, sess.markedMessages(), "messages must not be committed when the write fails")
	require.Zero(t, kc.count)
}

func TestFlushSyncErrorMarksNothing(t *testing.T) {
	kc, fw := newTestConsumer(t, func(c *consumerCfg) { c.sync = true })
	sess := &fakeSession{}
	// the unstarted muxer has no connections, so the synchronous sync fails
	require.Error(t, kc.flush(sess, []*sarama.ConsumerMessage{msgAt(time.Now(), `x`)}))
	require.Len(t, fw.entries(), 1)
	require.Empty(t, sess.markedMessages(), "messages must not be committed until the sync succeeds")
	require.Zero(t, kc.count)
}

// ---- source and tag resolution ----

func TestResolveSourceAndTagNilMessage(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	kc.src = net.ParseIP(`192.168.1.1`)
	tag, ip, err := kc.resolveSourceAndTag(nil)
	require.NoError(t, err)
	require.Equal(t, kc.defaultTag, tag)
	require.True(t, kc.src.Equal(ip))
}

func TestResolveSourceHeader(t *testing.T) {
	fallback := net.ParseIP(`192.168.1.1`)
	v4 := net.ParseIP(`10.0.0.1`).To4()
	v6 := net.ParseIP(`fe80::1`)
	tests := []struct {
		name   string
		srcBin bool
		value  []byte
		want   net.IP
	}{
		{`text ipv4`, false, []byte(`10.0.0.1`), v4},
		{`text ipv6`, false, []byte(`fe80::1`), v6},
		{`garbage falls back`, false, []byte(`not an ip`), fallback},
		{`binary not enabled`, false, []byte(v4), fallback},
		{`binary ipv4`, true, []byte(v4), v4},
		{`binary ipv6`, true, []byte(v6.To16()), v6},
		{`binary mode still parses text`, true, []byte(`10.0.0.1`), v4},
		{`binary bad length falls back`, true, []byte{1, 2, 3}, fallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kc, _ := newTestConsumer(t, func(c *consumerCfg) { c.srcBin = tt.srcBin })
			kc.src = fallback
			m := msgAt(time.Now(), `x`, &sarama.RecordHeader{Key: []byte(defaultSRCHeader), Value: tt.value})
			tag, ip, err := kc.resolveSourceAndTag(m)
			require.NoError(t, err)
			require.Equal(t, kc.defaultTag, tag)
			require.True(t, tt.want.Equal(ip), "got %v want %v", ip, tt.want)
		})
	}
}

func TestResolveCustomHeaderNames(t *testing.T) {
	kc, fw := newTestConsumer(t, func(c *consumerCfg) {
		c.srcKey = `origin`
		c.tagKey = `kind`
	})
	m := msgAt(time.Now(), `x`,
		hdr(`origin`, `172.16.0.9`),
		hdr(`kind`, `allowedbaz`),
		hdr(defaultSRCHeader, `1.1.1.1`), // default names are ignored once overridden
		hdr(defaultTagHeader, `allowedqux`),
	)
	tag, ip, err := kc.resolveSourceAndTag(m)
	require.NoError(t, err)
	require.Equal(t, fw.tag(`allowedbaz`), tag)
	require.True(t, net.ParseIP(`172.16.0.9`).Equal(ip))
}

// ---- ConsumeClaim ----

func TestConsumeClaimWrongTopic(t *testing.T) {
	kc, _ := newTestConsumer(t, nil)
	require.Error(t, kc.ConsumeClaim(&fakeSession{}, claimWith(`some-other-topic`)))
}

func TestConsumeClaimFlushesOnTimestampChange(t *testing.T) {
	kc, fw := newTestConsumer(t, nil)
	t1 := time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)
	t2 := t1.Add(time.Second)
	msgs := []*sarama.ConsumerMessage{msgAt(t1, `a`), nil, msgAt(t1, `b`), msgAt(t2, `c`)}
	sess := &fakeSession{}

	require.NoError(t, kc.ConsumeClaim(sess, claimWith(testTopic, msgs...)))

	// the second change in timestamp flushes the first two, nil messages are skipped,
	// and the partial batch left when the claim closes is not written or committed so Kafka redelivers it
	require.Len(t, fw.entries(), 2)
	require.Equal(t, []*sarama.ConsumerMessage{msgs[0], msgs[2]}, sess.markedMessages())
}

func TestConsumeClaimFlushesAtBatchSize(t *testing.T) {
	kc, fw := newTestConsumer(t, func(c *consumerCfg) { c.batchSize = 2 })
	ts := time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)
	msgs := []*sarama.ConsumerMessage{msgAt(ts, `a`), msgAt(ts, `b`), msgAt(ts, `c`), msgAt(ts, `d`), msgAt(ts, `e`)}
	sess := &fakeSession{}

	require.NoError(t, kc.ConsumeClaim(sess, claimWith(testTopic, msgs...)))

	require.Len(t, fw.entries(), 4)
	require.Equal(t, msgs[:4], sess.markedMessages())
}

func TestConsumeClaimFlushesOnTicker(t *testing.T) {
	kc, fw := newTestConsumer(t, nil)
	ch := make(chan *sarama.ConsumerMessage, 1)
	sess := &fakeSession{}
	errCh := make(chan error, 1)
	go func() { errCh <- kc.ConsumeClaim(sess, &fakeClaim{topic: testTopic, ch: ch}) }()

	m := msgAt(time.Now(), `lonely`)
	ch <- m
	require.Eventually(t, func() bool { return len(sess.markedMessages()) == 1 },
		5*time.Second, 20*time.Millisecond, "a lone message should be flushed by the one second ticker")
	close(ch)
	require.NoError(t, <-errCh)
	require.Len(t, fw.entries(), 1)
	require.Equal(t, m, sess.markedMessages()[0])
}

func TestConsumeClaimStopsOnWriteError(t *testing.T) {
	kc, fw := newTestConsumer(t, func(c *consumerCfg) { c.batchSize = 1 })
	fw.err = errTestWrite
	ts := time.Now()
	sess := &fakeSession{}
	err := kc.ConsumeClaim(sess, claimWith(testTopic, msgAt(ts, `a`), msgAt(ts, `b`)))
	require.ErrorIs(t, err, errTestWrite)
	require.Empty(t, sess.markedMessages())
}
