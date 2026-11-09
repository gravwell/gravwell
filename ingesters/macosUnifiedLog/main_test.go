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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
)

// newTestMuxer returns an IngestMuxer that knows about tags, closing it on
// test cleanup. It's never started, so GetTag resolves without dialing out.
func newTestMuxer(t *testing.T, tags ...string) *ingest.IngestMuxer {
	t.Helper()
	muxer, err := ingest.NewUniformMuxer(ingest.UniformMuxerConfig{
		Destinations: []string{"127.0.0.1:0"},
		Tags:         tags,
		Auth:         "testing",
		Logger:       log.NewDiscardLogger(),
	})
	if err != nil {
		t.Fatalf("failed to build test muxer: %v", err)
	}
	t.Cleanup(func() {
		if err := muxer.Close(); err != nil {
			t.Errorf("failed to close test muxer: %v", err)
		}
	})
	return muxer
}

// stubProcCloser is a processorCloser that does nothing but count its Close
// calls, so tests can observe runStreams's processor-set lifecycle without
// building real preprocessors.
type stubProcCloser struct {
	mu     sync.Mutex
	closes int
}

func (p *stubProcCloser) ProcessContext(*entry.Entry, context.Context) error { return nil }

func (p *stubProcCloser) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return nil
}

func (p *stubProcCloser) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}

// blockingProcCloser is a processorCloser whose ProcessContext ignores ctx and
// blocks until unblock is closed, simulating a preprocessor that doesn't honor
// cancellation.
type blockingProcCloser struct {
	unblock chan struct{}

	mu     sync.Mutex
	closes int
}

func (p *blockingProcCloser) ProcessContext(*entry.Entry, context.Context) error {
	<-p.unblock
	return nil
}

func (p *blockingProcCloser) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return nil
}

func (p *blockingProcCloser) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}

// withStubBuildProcessorSet overrides buildProcessorSet for the duration of a
// test, restoring the original on cleanup.
func withStubBuildProcessorSet(t *testing.T, fn func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error)) {
	t.Helper()
	orig := buildProcessorSet
	buildProcessorSet = fn
	t.Cleanup(func() { buildProcessorSet = orig })
}

// writeFakeLog writes a standalone executable at a temp path that prints a
// single JSON event, ignoring whatever arguments runStreams hands it, then
// blocks. It stands in for `log` so runStreams's streaming loop can actually
// start a process without macOS's real `log` binary.
func writeFakeLog(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-log")
	body := "#!/bin/sh\nprintf '[{\"eventMessage\":\"hi\"}]\\n'\nexec sleep 60\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return script
}

// TestRunStreamsPartialStartupFailure is a regression test: if any stream
// fails to resolve its tag, no other stream's `log` process should ever be
// started, and runStreams should report the failure as an error rather than
// exiting the process out from under an already-running stream.
func TestRunStreamsPartialStartupFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh available")
	}

	// stands in for `log`: if ever started, it touches a sentinel file and then
	// blocks, so a wrongly-started process would be easy to catch mid-run
	sentinel := filepath.Join(t.TempDir(), "started")
	script := filepath.Join(t.TempDir(), "fake-log")
	body := fmt.Sprintf("#!/bin/sh\ntouch %q\nexec sleep 60\n", sentinel)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	orig := logCommand
	logCommand = script
	t.Cleanup(func() { logCommand = orig })

	// the muxer only knows about "ok", so GetTag fails for stream "b" without
	// ever dialing out
	muxer := newTestMuxer(t, "ok")

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			"a": {Tag_Name: "ok"},
			"b": {Tag_Name: "missing"},
		},
	}

	out := &syncBuffer{}
	lg := log.New(out)

	err := runStreams(context.Background(), cfg, muxer, "", lg)
	if err == nil {
		t.Fatal("expected an error from runStreams, got nil")
	}
	if !strings.Contains(err.Error(), `"b"`) || !strings.Contains(err.Error(), `"missing"`) {
		t.Fatalf("error %q does not name the failing stream %q and tag %q", err, "b", "missing")
	}

	// give a wrongly-started process a moment to touch the sentinel
	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatalf("stream %q's log process was started despite stream %q failing to resolve its tag", "a", "b")
	}
}

// TestRunStreamsPartialStartupFailureClosesPriorProcessors is a regression
// test for the ProcessorSet leak the original partial-startup-failure fix was
// meant to prevent: if stream "a" is set up (tag resolved, ProcessorSet built)
// before stream "b" fails to resolve its tag, "a"'s ProcessorSet must be
// closed, not leaked. cfg.Stream is a map, so which stream the setup loop
// visits first is not under the test's control; it runs the scenario several
// times and checks the invariant every time, requiring that the interesting
// ordering (a before b) actually occurred at least once.
func TestRunStreamsPartialStartupFailureClosesPriorProcessors(t *testing.T) {
	muxer := newTestMuxer(t, "ok")

	sawPriorClosed := false
	for i := 0; i < 25; i++ {
		proc := &stubProcCloser{}
		withStubBuildProcessorSet(t, func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error) {
			return proc, nil
		})

		cfg := &cfgType{
			Stream: map[string]*streamConfig{
				"a": {Tag_Name: "ok"},
				"b": {Tag_Name: "missing"},
			},
		}
		out := &syncBuffer{}
		lg := log.New(out)

		err := runStreams(context.Background(), cfg, muxer, "", lg)
		if err == nil {
			t.Fatal("expected an error from runStreams, got nil")
		}

		switch n := proc.closeCount(); n {
		case 0:
			// "b" was visited before "a" in this run's map iteration order, so
			// "a" never got far enough to build a ProcessorSet; nothing to leak
		case 1:
			sawPriorClosed = true
		default:
			t.Fatalf("iteration %d: ProcessorSet.Close() called %d times, want 0 or 1", i, n)
		}
	}
	if !sawPriorClosed {
		t.Fatal("never observed stream \"a\"'s ProcessorSet built before stream \"b\" failed; rerun or increase iterations")
	}
}

// TestRunStreamsPreprocessorConstructionFailure checks the preprocessor
// construction branch of the setup loop: a stream whose preprocessor list
// names an undefined preprocessor fails runStreams with an error naming the
// stream, without ever starting a `log` process.
func TestRunStreamsPreprocessorConstructionFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh available")
	}
	sentinel := filepath.Join(t.TempDir(), "started")
	script := filepath.Join(t.TempDir(), "fake-log")
	body := fmt.Sprintf("#!/bin/sh\ntouch %q\nexec sleep 60\n", sentinel)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	orig := logCommand
	logCommand = script
	t.Cleanup(func() { logCommand = orig })

	muxer := newTestMuxer(t, "ok")

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			// bypasses cfg.Verify()'s CheckProcessors, same as the tag-failure
			// test bypasses GetTag validation, to isolate this branch
			"b": {Tag_Name: "ok", Preprocessor: []string{"missing-preprocessor"}},
		},
		// must be non-nil: ProcessorConfig.ProcessorSet treats a nil map as "no
		// preprocessors configured" and skips validating names entirely, which
		// would make this stream's construction succeed instead of failing
		Preprocessor: processors.ProcessorConfig{},
	}
	out := &syncBuffer{}
	lg := log.New(out)

	err := runStreams(context.Background(), cfg, muxer, "", lg)
	if err == nil {
		t.Fatal("expected an error from runStreams, got nil")
	}
	if !strings.Contains(err.Error(), `"b"`) || !strings.Contains(err.Error(), "missing-preprocessor") {
		t.Fatalf("error %q does not name the failing stream %q and preprocessor %q", err, "b", "missing-preprocessor")
	}

	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(sentinel); !os.IsNotExist(statErr) {
		t.Fatal("a log process was started despite a stream failing preprocessor construction")
	}
}

// TestRunStreamsPreprocessorConstructionFailureClosesPartialSet is a
// regression test: ProcessorConfig.ProcessorSet can return a non-nil,
// partially-built set alongside its error (e.g. the second of two
// preprocessors failed to construct), and that partial set must be closed
// rather than leaked.
func TestRunStreamsPreprocessorConstructionFailureClosesPartialSet(t *testing.T) {
	muxer := newTestMuxer(t, "ok")

	proc := &stubProcCloser{}
	withStubBuildProcessorSet(t, func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error) {
		return proc, errors.New("simulated preprocessor construction failure")
	})

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			"b": {Tag_Name: "ok", Preprocessor: []string{"whatever"}},
		},
	}
	out := &syncBuffer{}
	lg := log.New(out)

	err := runStreams(context.Background(), cfg, muxer, "", lg)
	if err == nil {
		t.Fatal("expected an error from runStreams, got nil")
	}
	if !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("error %q does not name the failing stream %q", err, "b")
	}
	if n := proc.closeCount(); n != 1 {
		t.Fatalf("partially-built ProcessorSet.Close() called %d times, want 1 (leaked otherwise)", n)
	}
}

// TestRunStreamsHappyPathShutdown covers runStreams's main shutdown path:
// canceling ctx stops every streamer goroutine, runStreams returns nil, and
// every ProcessorSet it built is closed exactly once.
func TestRunStreamsHappyPathShutdown(t *testing.T) {
	logCommandOrig := logCommand
	logCommand = writeFakeLog(t)
	t.Cleanup(func() { logCommand = logCommandOrig })

	muxer := newTestMuxer(t, "ok")

	proc := &stubProcCloser{}
	withStubBuildProcessorSet(t, func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error) {
		return proc, nil
	})

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			"a": {Tag_Name: "ok"},
		},
	}
	out := &syncBuffer{}
	lg := log.New(out)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runStreams(ctx, cfg, muxer, "", lg) }()

	// give the streamer a moment to start the fake log process
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runStreams returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runStreams did not return after ctx was canceled")
	}

	if n := proc.closeCount(); n != 1 {
		t.Fatalf("ProcessorSet.Close() called %d times, want exactly 1", n)
	}
}

// TestRunStreamsTimeoutDoesNotCloseProcessors is a regression test for the
// shutdown-path race between the wg.Wait() timeout and ProcessorSet.Close():
// if a streamer goroutine is still live (stuck inside a preprocessor call)
// when wgWaitTimeout fires, runStreams must return promptly, logging the
// timeout, without calling Close() on a ProcessorSet a goroutine may still be
// using.
func TestRunStreamsTimeoutDoesNotCloseProcessors(t *testing.T) {
	logCommandOrig := logCommand
	logCommand = writeFakeLog(t)
	t.Cleanup(func() { logCommand = logCommandOrig })

	timeoutOrig := wgWaitTimeout
	wgWaitTimeout = 100 * time.Millisecond
	t.Cleanup(func() { wgWaitTimeout = timeoutOrig })

	muxer := newTestMuxer(t, "ok")

	proc := &blockingProcCloser{unblock: make(chan struct{})}
	t.Cleanup(func() { close(proc.unblock) }) // let the blocked goroutine finish so it doesn't leak past the test
	withStubBuildProcessorSet(t, func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error) {
		return proc, nil
	})

	cfg := &cfgType{
		Stream: map[string]*streamConfig{
			"a": {Tag_Name: "ok"},
		},
	}
	out := &syncBuffer{}
	lg := log.New(out)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- runStreams(ctx, cfg, muxer, "", lg) }()

	// give the streamer a moment to start consuming the fake log's one event,
	// which blocks inside proc.ProcessContext
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runStreams returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runStreams did not return within the timeout window; it waited for the blocked goroutine instead")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("runStreams took %v to return after cancel, want roughly wgWaitTimeout (%v)", elapsed, wgWaitTimeout)
	}

	if !strings.Contains(out.String(), "timed out waiting for streamers to stop") {
		t.Errorf("timeout was not logged:\n%s", out.String())
	}
	if n := proc.closeCount(); n != 0 {
		t.Fatalf("ProcessorSet.Close() called %d times while a streamer goroutine is still live, want 0", n)
	}
}
