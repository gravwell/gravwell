package ingest

import (
	"bytes"
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/attach"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type acknowledgedAttachConfig struct {
	Attach attach.AttachConfig
}

func TestWriteEntryAcknowledgedContextPreservesAttachmentsAndAccounting(t *testing.T) {
	var cfg acknowledgedAttachConfig
	if err := config.LoadConfigBytes(&cfg, []byte("[Attach]\nreview=value\n")); err != nil {
		t.Fatal(err)
	}
	attacher, err := attach.NewAttacher(cfg.Attach, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	delivered := make(chan dittoBlock)
	muxer := &IngestMuxer{
		mtx:          &sync.RWMutex{},
		dittoChan:    delivered,
		writeBarrier: make(chan bool),
		maxEntrySize: MAX_ENTRY_SIZE,
		state:        running,
		attacher:     attacher,
		attachActive: attacher.Active(),
	}
	ent := entry.Entry{Tag: entry.GravwellTagId, Data: []byte("test")}
	originalSize := ent.Size()
	result := make(chan error, 1)
	go func() {
		result <- muxer.WriteEntryAcknowledgedContext(context.Background(), &ent)
	}()
	block := <-delivered
	if got, ok := block.ents[0].GetEnumeratedValue("review"); !ok || got != "value" {
		t.Fatalf("delivered attachment=(%v,%v) want=(value,true)", got, ok)
	}
	if got, ok := ent.GetEnumeratedValue("review"); !ok || got != "value" {
		t.Fatalf("caller attachment=(%v,%v) want=(value,true)", got, ok)
	}
	if ent.Size() <= originalSize {
		t.Fatalf("attached size=%d want greater than original=%d", ent.Size(), originalSize)
	}
	block.cb(nil)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if got := muxer.entryCount.Load(); got != 1 {
		t.Fatalf("entry count=%d want=1", got)
	}
	if got, want := muxer.byteCount.Load(), uint64(ent.Size()); got != want {
		t.Fatalf("byte count=%d want attached size=%d", got, want)
	}
}

func TestWriteEntryAcknowledgedContextWithoutAttachments(t *testing.T) {
	delivered := make(chan dittoBlock)
	muxer := &IngestMuxer{
		mtx:          &sync.RWMutex{},
		dittoChan:    delivered,
		writeBarrier: make(chan bool),
		maxEntrySize: MAX_ENTRY_SIZE,
		state:        running,
	}
	ent := entry.Entry{Tag: entry.GravwellTagId, Data: []byte("test")}
	result := make(chan error, 1)
	go func() {
		result <- muxer.WriteEntryAcknowledgedContext(context.Background(), &ent)
	}()
	block := <-delivered
	if got := len(block.ents[0].EnumeratedValues()); got != 0 {
		t.Fatalf("enumerated values=%d want=0", got)
	}
	block.cb(nil)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestWriteEntryAcknowledgedContextLifecycleStateIsSynchronized(t *testing.T) {
	muxer := &IngestMuxer{
		mtx:          &sync.RWMutex{},
		dittoChan:    make(chan dittoBlock),
		writeBarrier: make(chan bool),
		maxEntrySize: MAX_ENTRY_SIZE,
		state:        running,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < 10000; i++ {
			muxer.mtx.Lock()
			if i%2 == 0 {
				muxer.state = closed
			} else {
				muxer.state = running
			}
			muxer.mtx.Unlock()
			runtime.Gosched()
		}
		close(done)
	}()
	close(start)
	for i := 0; i < 10000; i++ {
		err := muxer.WriteEntryAcknowledgedContext(ctx, &entry.Entry{Tag: entry.GravwellTagId, Data: []byte("test")})
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrNotRunning) {
			t.Fatalf("error=%v want cancellation or ErrNotRunning", err)
		}
		runtime.Gosched()
	}
	<-done
}

func TestDittoWriteContextReturnsCancellationAfterRelayAcceptsBlock(t *testing.T) {
	delivered := make(chan dittoBlock)
	barrier := make(chan bool)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: barrier, maxEntrySize: MAX_ENTRY_SIZE}
	accepted := make(chan struct{})
	blockResult := make(chan dittoBlock, 1)
	go func() {
		block := <-delivered
		close(accepted)
		blockResult <- block
	}()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- muxer.DittoWriteContext(ctx, []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}})
	}()
	<-accepted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context cancellation", err)
	}
	// A late callback must not block the relay after the caller returns.
	block := <-blockResult
	callbackDone := make(chan struct{})
	go func() {
		block.cb(nil)
		close(callbackDone)
	}()
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("late acknowledgement callback blocked")
	}
}

func TestDittoWriteContextOwnsEntriesAfterPostHandoffReturn(t *testing.T) {
	delivered := make(chan dittoBlock)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: make(chan bool), maxEntrySize: MAX_ENTRY_SIZE}
	ent := entry.Entry{
		Tag:  entry.GravwellTagId,
		SRC:  net.IP{192, 0, 2, 10},
		Data: []byte("original-data"),
	}
	if err := ent.AddEnumeratedValueEx("original", "value"); err != nil {
		t.Fatal(err)
	}
	batch := []entry.Entry{ent}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- muxer.DittoWriteContext(ctx, batch)
	}()

	block := <-delivered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context cancellation", err)
	}

	copy(batch[0].Data, []byte("mutated-data!"))
	copy(batch[0].SRC, net.IP{203, 0, 113, 20})
	batch[0].Tag = 42
	batch[0].ClearEnumeratedValues()

	if len(block.ents) != 1 {
		t.Fatalf("delivered entries=%d want=1", len(block.ents))
	}
	got := &block.ents[0]
	if got.Tag != entry.GravwellTagId || !bytes.Equal(got.Data, []byte("original-data")) || !got.SRC.Equal(net.IP{192, 0, 2, 10}) {
		t.Fatalf("relay-owned entry mutated: tag=%d data=%q src=%v", got.Tag, got.Data, got.SRC)
	}
	if value, ok := got.GetEnumeratedValue("original"); !ok || value != "value" {
		t.Fatalf("relay-owned attachment=(%v,%v) want=(value,true)", value, ok)
	}
	block.cb(nil)
}

func TestDittoWriteContextReturnsAfterBarrierClosesPostHandoff(t *testing.T) {
	delivered := make(chan dittoBlock)
	barrier := make(chan bool)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: barrier, maxEntrySize: MAX_ENTRY_SIZE}
	accepted := make(chan struct{})
	blockResult := make(chan dittoBlock, 1)
	go func() {
		block := <-delivered
		close(accepted)
		blockResult <- block
	}()
	result := make(chan error, 1)
	go func() {
		result <- muxer.DittoWriteContext(context.Background(), []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}})
	}()
	<-accepted
	close(barrier)
	if err := <-result; !errors.Is(err, ErrNotRunning) {
		t.Fatalf("error=%v want ErrNotRunning", err)
	}
	(<-blockResult).cb(nil)
}

func TestDittoWriteContextCancellationBeforeRelayHandoff(t *testing.T) {
	delivered := make(chan dittoBlock, 1)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: make(chan bool), maxEntrySize: MAX_ENTRY_SIZE}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := muxer.DittoWriteContext(ctx, []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context cancellation", err)
	}
	if len(delivered) != 0 {
		t.Fatal("pre-canceled block was handed to ready relay")
	}
}

func TestDittoWriteContextBarrierBeforeRelayHandoff(t *testing.T) {
	barrier := make(chan bool)
	close(barrier)
	delivered := make(chan dittoBlock, 1)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: barrier, maxEntrySize: MAX_ENTRY_SIZE}
	if err := muxer.DittoWriteContext(context.Background(), []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("error=%v want ErrNotRunning", err)
	}
	if len(delivered) != 0 {
		t.Fatal("block was handed to ready relay after barrier closed")
	}
}

func TestDittoWriteContextValidatesBatchBeforeRelayHandoff(t *testing.T) {
	delivered := make(chan dittoBlock, 1)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: make(chan bool), maxEntrySize: 4}
	if err := muxer.DittoWriteContext(context.Background(), nil); err != nil {
		t.Fatalf("empty batch error=%v", err)
	}
	if err := muxer.DittoWriteContext(context.Background(), []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("12345")}}); !errors.Is(err, ErrOversizedEntry) {
		t.Fatalf("error=%v want ErrOversizedEntry", err)
	}
	if len(delivered) != 0 {
		t.Fatal("invalid block was handed to relay")
	}
}

func TestDittoWriteContextReturnsAuthoritativeAcknowledgementError(t *testing.T) {
	delivered := make(chan dittoBlock)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: make(chan bool), maxEntrySize: MAX_ENTRY_SIZE}
	want := errors.New("synthetic disk acknowledgement failure")
	go func() {
		block := <-delivered
		block.cb(want)
	}()
	err := muxer.DittoWriteContext(context.Background(), []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want=%v", err, want)
	}
}

func TestDittoWriteContextPrefersReadyAcknowledgementOverCancellation(t *testing.T) {
	delivered := make(chan dittoBlock)
	muxer := &IngestMuxer{dittoChan: delivered, writeBarrier: make(chan bool), maxEntrySize: MAX_ENTRY_SIZE}
	want := errors.New("synthetic disk acknowledgement failure")
	accepted := make(chan struct{})
	release := make(chan struct{})
	acknowledgementSent := make(chan struct{})
	go func() {
		block := <-delivered
		close(accepted)
		<-release
		block.cb(want)
		close(acknowledgementSent)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- muxer.DittoWriteContext(ctx, []entry.Entry{{Tag: entry.GravwellTagId, Data: []byte("test")}})
	}()
	<-accepted
	close(release)
	<-acknowledgementSent
	cancel()
	if err := <-result; !errors.Is(err, want) {
		t.Fatalf("error=%v want authoritative acknowledgement error %v", err, want)
	}
}
