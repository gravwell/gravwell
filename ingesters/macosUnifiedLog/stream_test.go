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
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewjam/rfc5424"
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

// failNthHandler succeeds for every entry except the nth (1-indexed), which it fails once.
type failNthHandler struct {
	sync.Mutex
	n       int
	attempt int
	ents    []*entry.Entry
}

func (h *failNthHandler) ProcessContext(ent *entry.Entry, ctx context.Context) error {
	h.Lock()
	defer h.Unlock()
	h.attempt++
	if h.attempt == h.n {
		return errors.New("simulated entry failure")
	}
	h.ents = append(h.ents, ent)
	return nil
}

func (h *failNthHandler) count() int {
	h.Lock()
	defer h.Unlock()
	return len(h.ents)
}

// cancelingHandler cancels ctx, then fails, simulating a send that races shutdown.
type cancelingHandler struct {
	cancel context.CancelFunc
	calls  int
}

func (h *cancelingHandler) ProcessContext(ent *entry.Entry, ctx context.Context) error {
	h.calls++
	h.cancel()
	return errors.New("simulated send failure")
}

// parseLogLines parses structured log output in s. It splits on '\n', so a
// value containing a newline will misparse.
func parseLogLines(t *testing.T, s string) []rfc5424.Message {
	t.Helper()
	var msgs []rfc5424.Message
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m rfc5424.Message
		if err := m.UnmarshalBinary([]byte(line)); err != nil {
			t.Fatalf("failed to parse log line %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// sdValue returns the value of the named structured-data parameter on m, if any.
func sdValue(m rfc5424.Message, name string) (string, bool) {
	for _, sd := range m.StructuredData {
		for _, p := range sd.Parameters {
			if p.Name == name {
				return p.Value, true
			}
		}
	}
	return "", false
}

// splitIntoChunks splits s into pieces of the given sizes, repeating the list as needed.
func splitIntoChunks(s string, sizes ...int) []string {
	var chunks []string
	for i := 0; len(s) > 0; i = (i + 1) % len(sizes) {
		n := min(sizes[i], len(s))
		chunks = append(chunks, s[:n])
		s = s[n:]
	}
	return chunks
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
		// large enough that a test script's near-instant exit never counts as a
		// healthy run by accident; tests that exercise the backoff reset set their own
		minHealthy: time.Hour,
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

// TestRunOnceCleanExit checks the branch where the log process produces
// output and then exits cleanly (status 0): the error is exactly "log
// exited", not wrapped around a nil *exec.ExitError.
func TestRunOnceCleanExit(t *testing.T) {
	s, h, _ := shellStreamer(t, `printf '%s' "$0"`, sampleStream)
	n, err := s.runOnce(context.Background())
	if n != 2 || h.count() != 2 {
		t.Fatalf("ingested %d events (%d handled), want 2", n, h.count())
	}
	if err == nil || err.Error() != "log exited" {
		t.Fatalf("got %v, want exactly \"log exited\"", err)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("error unexpectedly wraps an *exec.ExitError: %v", err)
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

// TestRunAlreadyCanceled checks that run never spawns a log process at all
// when handed a context that is already canceled.
func TestRunAlreadyCanceled(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "touched")
	s, h, _ := shellStreamer(t, fmt.Sprintf(`touch %q`, sentinel), "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return promptly for an already-canceled context")
	}

	if h.count() != 0 {
		t.Fatalf("ingested %d events, want 0", h.count())
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("a log process was started despite the context already being canceled")
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

	// a write with no newline still gets chunked and fully emitted, not just truncated
	w.Write(bytes.Repeat([]byte("x"), maxStderrLine*2))
	if len(w.buf) != 0 {
		t.Errorf("stderr relay is holding %d bytes", len(w.buf))
	}
	if c := strings.Count(out.String(), strings.Repeat("x", maxStderrLine)); c != 2 {
		t.Errorf("expected 2 full %d-byte chunks relayed, found %d:\n%s", maxStderrLine, c, out.String())
	}
}

// TestStderrRelayLongLine checks a long newline-terminated line is fully
// relayed, chunked into maxStderrLine-sized records.
func TestStderrRelayLongLine(t *testing.T) {
	out := &syncBuffer{}
	w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
	line := bytes.Repeat([]byte("y"), 6000)
	w.Write(append(append([]byte{}, line...), '\n'))

	var got strings.Builder
	for _, m := range parseLogLines(t, out.String()) {
		if v, ok := sdValue(m, "output"); ok {
			got.WriteString(v)
		}
	}
	if got.String() != string(line) {
		t.Fatalf("relayed %d of %d bytes from a single long line", got.Len(), len(line))
	}
}

// TestStderrRelayNoNewlineChunking checks tail-chunking at the maxStderrLine
// boundary, then flush, for writes that never see a newline.
func TestStderrRelayNoNewlineChunking(t *testing.T) {
	tests := []struct {
		size        int
		wantRecords int
		wantBufLen  int
	}{
		{maxStderrLine - 1, 0, maxStderrLine - 1},
		{maxStderrLine, 1, 0},
		{maxStderrLine + 1, 1, 1},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("size=%d", tt.size), func(t *testing.T) {
			out := &syncBuffer{}
			w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
			w.Write(bytes.Repeat([]byte("z"), tt.size))
			if len(w.buf) != tt.wantBufLen {
				t.Errorf("buf len = %d, want %d", len(w.buf), tt.wantBufLen)
			}
			if n := strings.Count(out.String(), "log stream stderr"); n != tt.wantRecords {
				t.Errorf("emitted %d records, want %d", n, tt.wantRecords)
			}

			w.flush()
			var got strings.Builder
			for _, m := range parseLogLines(t, out.String()) {
				if v, ok := sdValue(m, "output"); ok {
					got.WriteString(v)
				}
			}
			if want := strings.Repeat("z", tt.size); got.String() != want {
				t.Errorf("after flush, relayed %d of %d bytes", got.Len(), tt.size)
			}
		})
	}
}

// TestStderrRelayNewlineChunkBoundary checks a line of exactly 2*maxStderrLine
// bytes emits exactly 2 records, not a phantom empty one.
func TestStderrRelayNewlineChunkBoundary(t *testing.T) {
	out := &syncBuffer{}
	w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
	line := bytes.Repeat([]byte("q"), 2*maxStderrLine)
	w.Write(append(append([]byte{}, line...), '\n'))

	var got strings.Builder
	msgs := parseLogLines(t, out.String())
	for _, m := range msgs {
		if v, ok := sdValue(m, "output"); ok {
			got.WriteString(v)
		}
	}
	if len(msgs) != 2 {
		t.Fatalf("emitted %d records for a %d-byte line, want 2", len(msgs), len(line))
	}
	if got.String() != string(line) {
		t.Fatalf("relayed %d of %d bytes", got.Len(), len(line))
	}
}

// TestStderrRelayChunkBoundaryWhitespace checks a space landing exactly on a
// chunk boundary survives.
func TestStderrRelayChunkBoundaryWhitespace(t *testing.T) {
	line := bytes.Repeat([]byte("a"), 2*maxStderrLine+10)
	line[maxStderrLine] = ' '

	out := &syncBuffer{}
	w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
	w.Write(append(append([]byte{}, line...), '\n'))

	var got strings.Builder
	for _, m := range parseLogLines(t, out.String()) {
		if v, ok := sdValue(m, "output"); ok {
			got.WriteString(v)
		}
	}
	if got.String() != string(line) {
		t.Fatalf("relayed content lost the boundary whitespace:\n got %q\nwant %q", got.String(), line)
	}
}

// TestStderrRelayPreservesOrder checks relayed content and ordering, not
// just a substring count.
func TestStderrRelayPreservesOrder(t *testing.T) {
	var sb strings.Builder
	lineLens := []int{10, maxStderrLine, maxStderrLine + 1, 2*maxStderrLine + 37, 1}
	for i, n := range lineLens {
		for j := 0; j < n; j++ {
			sb.WriteByte(byte('a' + (i+j)%26))
		}
		if i < len(lineLens)-1 {
			sb.WriteByte('\n')
		}
	}
	input := sb.String()

	out := &syncBuffer{}
	w := &stderrRelay{lg: log.NewLoggerWithKV(log.New(out))}
	for _, chunk := range splitIntoChunks(input, 37, 4096, 1, 9001) {
		w.Write([]byte(chunk))
	}
	w.flush()

	var got strings.Builder
	for _, m := range parseLogLines(t, out.String()) {
		if v, ok := sdValue(m, "output"); ok {
			got.WriteString(v)
		}
	}
	want := strings.ReplaceAll(input, "\n", "")
	if got.String() != want {
		t.Fatalf("relayed content mismatch: got %d bytes, want %d bytes", got.Len(), len(want))
	}
}

func TestConsumeEntryError(t *testing.T) {
	const failAt = 2 // 1-indexed: fail the second entry only
	h := &failNthHandler{n: failAt}
	out := &syncBuffer{}
	s := &streamer{
		tag:  7,
		proc: h,
		lg:   log.NewLoggerWithKV(log.New(out), log.KV("stream", "test")),
	}
	input := sampleEvents[0] + "\n" + sampleEvents[1] + "\n" + sampleEvents[0] + "\n"
	n, err := s.consume(context.Background(), strings.NewReader(input))
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 2 {
		t.Fatalf("got %d successful entries, want 2", n)
	}
	if got := h.count(); got != 2 {
		t.Fatalf("handler recorded %d entries, want 2 (the failed one should not be recorded)", got)
	}
	if c := strings.Count(out.String(), "failed to send entry"); c != 1 {
		t.Fatalf("logged %d entry failures, want 1:\n%s", c, out.String())
	}
}

// TestConsumeContextCanceled checks consume treats a cancellation-triggered
// error as shutdown, not a per-entry failure.
func TestConsumeContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &cancelingHandler{cancel: cancel}
	out := &syncBuffer{}
	s := &streamer{
		tag:  7,
		proc: h,
		lg:   log.NewLoggerWithKV(log.New(out), log.KV("stream", "test")),
	}
	input := sampleEvents[0] + "\n" + sampleEvents[1] + "\n"
	n, err := s.consume(ctx, strings.NewReader(input))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if n != 0 {
		t.Fatalf("got %d successful entries, want 0", n)
	}
	if h.calls != 1 {
		t.Fatalf("handler called %d times, want 1 (consume should stop as soon as the context is canceled)", h.calls)
	}
	if strings.Contains(out.String(), "failed to send entry") {
		t.Errorf("a context cancellation should not be logged like a per-entry failure:\n%s", out.String())
	}
}

func TestRunOnceEventTooLarge(t *testing.T) {
	// generates an unterminated event over maxEventSize; the shell blocks until killed
	script := fmt.Sprintf(`printf '[{"eventMessage":"'; head -c %d /dev/zero | tr '\0' A`, maxEventSize+100)
	s, _, _ := shellStreamer(t, script, "sh")
	start := time.Now()
	n, err := s.runOnce(context.Background())
	if !errors.Is(err, errEventTooLarge) {
		t.Fatalf("expected errEventTooLarge, got %v", err)
	}
	if want := fmt.Sprintf("(%d bytes)", maxEventSize); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not include %q", err, want)
	}
	if n != 0 {
		t.Errorf("ingested %d events, want 0", n)
	}
	if elapsed := time.Since(start); elapsed >= exitWaitDelay {
		t.Fatalf("took %v to stop a process after an oversized event", elapsed)
	}
}

func TestRunOnceStartFailure(t *testing.T) {
	orig := logCommand
	logCommand = filepath.Join(t.TempDir(), "does-not-exist")
	t.Cleanup(func() { logCommand = orig })

	s := &streamer{lg: log.NewLoggerWithKV(log.New(&syncBuffer{}))}
	if _, err := s.runOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "failed to start") {
		t.Fatalf("expected a failed-to-start error, got %v", err)
	}
}

// TestNewStreamer checks newStreamer wires every constructor field,
// including source resolution.
func TestNewStreamer(t *testing.T) {
	tag := entry.EntryTag(9)
	lg := log.New(&syncBuffer{})

	sc := &streamConfig{
		Tag_Name:          "macos-auth",
		Predicate:         "process == \"sudo\"",
		Source_Override:   "10.1.2.3",
		Ignore_Timestamps: true,
	}
	if err := sc.validate(); err != nil {
		t.Fatal(err)
	}
	// a global distinct from the override, so the assertion can't pass vacuously
	s := newStreamer("auth", sc, tag, net.ParseIP("10.9.9.9"), "", nil, lg)
	if s.name != "auth" {
		t.Errorf("name = %q, want %q", s.name, "auth")
	}
	if !slices.Equal(s.args, sc.args()) {
		t.Errorf("args = %q, want %q", s.args, sc.args())
	}
	if s.tag != tag {
		t.Errorf("tag = %d, want %d", s.tag, tag)
	}
	if s.src == nil || !s.src.Equal(net.ParseIP("10.1.2.3")) {
		t.Errorf("src = %v, want the per-stream override 10.1.2.3", s.src)
	}
	if !s.ignoreTS {
		t.Errorf("ignoreTS not wired through")
	}
	if s.minDelay != minRestartDelay || s.maxDelay != maxRestartDelay {
		t.Errorf("minDelay = %v, maxDelay = %v, want %v and %v (these bound the restart backoff, so a missed wiring would hot-loop a production streamer)", s.minDelay, s.maxDelay, minRestartDelay, maxRestartDelay)
	}
	if s.minHealthy != minHealthyRuntime {
		t.Errorf("minHealthy = %v, want %v", s.minHealthy, minHealthyRuntime)
	}
	if s.lg == nil {
		t.Errorf("lg not wired through")
	}

	// a nil per-stream override falls back to the global value it is handed
	global := net.ParseIP("10.9.9.9")
	sc2 := &streamConfig{}
	if err := sc2.validate(); err != nil {
		t.Fatal(err)
	}
	s2 := newStreamer("default", sc2, tag, global, "", nil, lg)
	if s2.src == nil || !s2.src.Equal(global) {
		t.Errorf("src = %v, want the global fallback %v", s2.src, global)
	}
}

// TestNewEntryHost checks that a non-empty host is stamped as a "host"
// enumerated value, and that an empty host (Disable-Hostname-Attach, or a
// failed os.Hostname lookup) leaves the entry without one.
func TestNewEntryHost(t *testing.T) {
	sc := &streamConfig{}
	if err := sc.validate(); err != nil {
		t.Fatal(err)
	}
	lg := log.New(&syncBuffer{})

	s := newStreamer("system", sc, entry.EntryTag(1), nil, "mac.local", nil, lg)
	ent := s.newEntry([]byte(`{"timestamp":"2026-09-30 10:02:12.345678-0600"}`))
	ev, ok := ent.EVB.Get("host")
	if !ok || ev.Value.String() != "mac.local" {
		t.Fatalf("host enumerated value not wired through, got %v (ok=%v)", ev, ok)
	}

	s2 := newStreamer("system", sc, entry.EntryTag(1), nil, "", nil, lg)
	ent2 := s2.newEntry([]byte(`{"timestamp":"2026-09-30 10:02:12.345678-0600"}`))
	if ent2.EVB.Populated() {
		t.Fatalf("expected no enumerated values with an empty host, got %v", ent2.EVB.Values())
	}
}

// TestRunBackoff checks the restart delay doubles, caps at maxDelay, and
// resets after a run that both ingests an event and runs long enough to
// count as healthy. It reads the logged delay value rather than timing
// wall-clock, since the delay itself is what matters.
func TestRunBackoff(t *testing.T) {
	ctr := filepath.Join(t.TempDir(), "n")
	// first 4 runs produce nothing and exit immediately, the 5th+ sleeps long enough
	// to count as a healthy run before producing one event; kept on one line so it
	// doesn't itself introduce newlines that break the log parsing below
	script := fmt.Sprintf(`n=$(cat %[1]q 2>/dev/null || echo 0); n=$((n+1)); echo "$n" >%[1]q; if [ "$n" -gt 4 ]; then sleep 0.1; printf '%%s' "$0"; fi`, ctr)
	s, h, out := shellStreamer(t, script, sampleEvents[0])
	s.minDelay = 2 * time.Millisecond
	s.maxDelay = 8 * time.Millisecond
	s.minHealthy = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onEntry = func(total int) {
		if total == 2 {
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
		t.Fatal("run did not stop")
	}

	var restarts []rfc5424.Message
	for _, m := range parseLogLines(t, out.String()) {
		if string(m.Message) == "log stream stopped, restarting" {
			restarts = append(restarts, m)
		}
	}
	want := []struct{ events, delay string }{
		{"0", "2ms"}, // minDelay
		{"0", "4ms"}, // doubled
		{"0", "8ms"}, // doubled again, now at maxDelay
		{"0", "8ms"}, // capped: would be 16ms uncapped
		{"1", "2ms"}, // reset to minDelay after a run that ingested an event and ran long enough to count as healthy
	}
	if len(restarts) < len(want) {
		t.Fatalf("got %d restarts, want at least %d:\n%s", len(restarts), len(want), out.String())
	}
	for i, w := range want {
		events, _ := sdValue(restarts[i], "events")
		delay, _ := sdValue(restarts[i], "delay")
		if events != w.events || delay != w.delay {
			t.Errorf("restart %d: events=%s delay=%s, want events=%s delay=%s", i, events, delay, w.events, w.delay)
		}
	}
}

// TestRunBackoffPoisonEvent is a regression test: a process that ingests an
// event every run but keeps exiting again almost immediately (e.g. a poison
// event the preprocessor keeps rejecting) must keep backing off instead of
// resetting to minDelay on every cycle, the way gating the reset on "ingested
// >=1 event" used to allow.
func TestRunBackoffPoisonEvent(t *testing.T) {
	s, h, out := shellStreamer(t, `printf '%s' "$0"`, sampleEvents[0])
	s.minDelay = 2 * time.Millisecond
	s.maxDelay = 8 * time.Millisecond
	s.minHealthy = time.Second // every run below finishes in well under this

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onEntry = func(total int) {
		// cancel during the 5th event so its restart message (which would
		// otherwise be suppressed by the cancellation anyway) is never logged,
		// leaving exactly the 4 restarts asserted below
		if total == 5 {
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
		t.Fatal("run did not stop")
	}

	var restarts []rfc5424.Message
	for _, m := range parseLogLines(t, out.String()) {
		if string(m.Message) == "log stream stopped, restarting" {
			restarts = append(restarts, m)
		}
	}
	if len(restarts) < 4 {
		t.Fatalf("got %d restarts, want at least 4:\n%s", len(restarts), out.String())
	}
	want := []string{"2ms", "4ms", "8ms", "8ms"} // never resets, even though every run ingests an event
	for i, w := range want {
		events, _ := sdValue(restarts[i], "events")
		delay, _ := sdValue(restarts[i], "delay")
		if events != "1" || delay != w {
			t.Errorf("restart %d: events=%s delay=%s, want events=1 delay=%s", i, events, delay, w)
		}
	}
}

// TestRunBackoffCancel checks canceling during the restart backoff sleep
// returns immediately, not after the full delay.
func TestRunBackoffCancel(t *testing.T) {
	s, _, out := shellStreamer(t, ``, ``)
	s.minDelay = 5 * time.Second
	s.maxDelay = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()

	waitUntil := time.After(2 * time.Second)
	for !strings.Contains(out.String(), "restarting") {
		select {
		case <-waitUntil:
			t.Fatal("run never logged a restart")
		case <-time.After(5 * time.Millisecond):
		}
	}

	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not return promptly after cancel during the backoff sleep")
	}
	if elapsed := time.Since(start); elapsed >= s.minDelay {
		t.Fatalf("run took %v to return after cancel, want well under minDelay %v", elapsed, s.minDelay)
	}
}
