/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

type captureHandler struct {
	sync.Mutex
	ents    []*entry.Entry
	onEntry func(total int)
}

func (h *captureHandler) ProcessContext(ent *entry.Entry, ctx context.Context) error {
	h.Lock()
	h.ents = append(h.ents, ent)
	total := len(h.ents)
	h.Unlock()
	if h.onEntry != nil {
		h.onEntry(total)
	}
	return nil
}

func (h *captureHandler) count() int {
	h.Lock()
	defer h.Unlock()
	return len(h.ents)
}

type syncBuffer struct {
	sync.Mutex
	b bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.Lock()
	defer s.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) Close() error { return nil }

func (s *syncBuffer) String() string {
	s.Lock()
	defer s.Unlock()
	return s.b.String()
}

// shellStreamer returns a streamer that runs script with /bin/sh in place of log,
// with the script's $0 set to arg0 so tests can hand it canned output.
func shellStreamer(t *testing.T, script, arg0 string) (*streamer, *captureHandler, *syncBuffer) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh available")
	}
	orig := logCommand
	logCommand = "/bin/sh"
	t.Cleanup(func() { logCommand = orig })

	h := &captureHandler{}
	out := &syncBuffer{}
	s := &streamer{
		name:     "test",
		args:     []string{"-c", script, arg0},
		tag:      7,
		src:      net.ParseIP("10.1.2.3"),
		proc:     h,
		lg:       log.NewLoggerWithKV(log.New(out), log.KV("stream", "test")),
		minDelay: 10 * time.Millisecond,
		maxDelay: 20 * time.Millisecond,
	}
	return s, h, out
}

func TestRunOnceExitStatus(t *testing.T) {
	s, h, out := shellStreamer(t, `printf '%s' "$0"; echo "log: Must be admin to run 'stream' command" >&2; exit 3`, sampleStream)
	n, err := s.runOnce(context.Background())
	if n != 2 || h.count() != 2 {
		t.Fatalf("ingested %d events (%d handled), want 2", n, h.count())
	}
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("expected the exit status in the error, got %v", err)
	}
	if !strings.Contains(out.String(), "Must be admin") {
		t.Errorf("stderr was not relayed to the log:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Filtering the log data") {
		t.Errorf("preamble was not relayed to the log:\n%s", out.String())
	}

	for i, ent := range h.ents {
		if string(ent.Data) != sampleEvents[i] {
			t.Errorf("event %d data mismatch: %s", i, ent.Data)
		}
		if ent.Tag != 7 || !ent.SRC.Equal(net.ParseIP("10.1.2.3")) {
			t.Errorf("event %d has tag %d src %v", i, ent.Tag, ent.SRC)
		}
	}
	want := time.Date(2026, 9, 30, 16, 2, 12, 345678000, time.UTC)
	if got := h.ents[0].TS.StandardTime(); !got.Equal(want) {
		t.Errorf("timestamp %v, want %v", got, want)
	}
}

func TestRunOnceKillsOnDecodeError(t *testing.T) {
	// exec so the shell is replaced by sleep, which then holds the pipes open until killed
	s, h, _ := shellStreamer(t, `printf '[{"a":1},oops'; exec sleep 60`, "sh")
	start := time.Now()
	n, err := s.runOnce(context.Background())
	var serr *json.SyntaxError
	if !errors.As(err, &serr) {
		t.Fatalf("expected a JSON syntax error, got %v", err)
	}
	if n != 1 || h.count() != 1 {
		t.Fatalf("ingested %d events, want 1", n)
	}
	if elapsed := time.Since(start); elapsed >= exitWaitDelay {
		t.Fatalf("took %v to stop a process after a decode error", elapsed)
	}
}

func TestRunOnceCancel(t *testing.T) {
	stream := strings.TrimSuffix(sampleStream, "]\n")
	s, h, _ := shellStreamer(t, `printf '%s' "$0"; exec sleep 60`, stream)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onEntry = func(total int) {
		if total == 2 {
			cancel()
		}
	}
	start := time.Now()
	n, err := s.runOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if n != 2 {
		t.Fatalf("ingested %d events, want 2", n)
	}
	if elapsed := time.Since(start); elapsed >= exitWaitDelay {
		t.Fatalf("took %v to stop after cancel", elapsed)
	}
}

func TestRunRestarts(t *testing.T) {
	s, h, out := shellStreamer(t, `printf '%s' "$0"`, sampleEvents[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onEntry = func(total int) {
		if total == 3 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("stream did not restart the process")
	}
	if h.count() < 3 {
		t.Fatalf("got %d events, want at least 3", h.count())
	}
	if !strings.Contains(out.String(), "restarting") {
		t.Errorf("restarts were not logged:\n%s", out.String())
	}
}

func TestNewEntryTimestamps(t *testing.T) {
	s, _, out := shellStreamer(t, ``, ``)

	before := time.Now()
	s.ignoreTS = true
	if ts := s.newEntry([]byte(sampleEvents[0])).TS.StandardTime(); ts.Before(before) {
		t.Errorf("Ignore-Timestamps should use the current time, got %v", ts)
	}

	s.ignoreTS = false
	s.newEntry([]byte(`{"timestamp":"not a time"}`))
	ent := s.newEntry([]byte(`{"eventMessage":"no time at all"}`))
	if ts := ent.TS.StandardTime(); ts.Before(before) {
		t.Errorf("an unparseable timestamp should fall back to the current time, got %v", ts)
	}
	if c := strings.Count(out.String(), "failed to read event timestamp"); c != 1 {
		t.Errorf("timestamp failure logged %d times, want 1", c)
	}
}

func TestStderrRelay(t *testing.T) {
	out := &syncBuffer{}
	w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
	w.Write([]byte("first li"))
	w.Write([]byte("ne\nsecond line\n\nthird"))
	if s := out.String(); !strings.Contains(s, "first line") || !strings.Contains(s, "second line") || strings.Contains(s, "third") {
		t.Fatalf("unexpected relay output:\n%s", s)
	}
	w.flush()
	if !strings.Contains(out.String(), "third") {
		t.Fatalf("flush did not emit the partial line:\n%s", out.String())
	}
	if c := strings.Count(out.String(), "log stream stderr"); c != 3 {
		t.Errorf("emitted %d lines, want 3:\n%s", c, out.String())
	}

	// a line with no newline is not buffered without bound
	w.Write(bytes.Repeat([]byte("x"), maxStderrLine*2))
	if len(w.buf) != 0 {
		t.Errorf("stderr relay is holding %d bytes", len(w.buf))
	}
}
