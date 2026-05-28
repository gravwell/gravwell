package hosted

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContinueNow(t *testing.T) {
	c := ContinueNow()
	if c == nil {
		t.Fatal("ContinueNow returned nil")
	}
	if c.Delay != 0 {
		t.Errorf("expected zero delay, got %v", c.Delay)
	}
}

func TestContinueAfter(t *testing.T) {
	d := 5 * time.Second
	c := ContinueAfter(d)
	if c == nil {
		t.Fatal("ContinueAfter returned nil")
	}
	if c.Delay != d {
		t.Errorf("expected delay %v, got %v", d, c.Delay)
	}
}

func TestStop(t *testing.T) {
	if Stop() != nil {
		t.Error("Stop should return nil")
	}
}

// TestWrapJob_StopsOnNilContinuation verifies that the adapter exits Run cleanly
// when Handle returns a nil continuation without requiring context cancellation.
func TestWrapJob_StopsOnNilContinuation(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	job := &testJob{
		results: []handleResult{
			{cont: Stop()},
		},
	}

	done := make(chan error, 1)
	go func() { done <- WrapJob(job).Run(ctx, rt) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not stop within timeout")
	}

	if job.callCount() != 1 {
		t.Errorf("expected 1 Handle call, got %d", job.callCount())
	}
}

// TestWrapJob_SyncCalledAfterEachSuccessfulHandle is the core at-least-once
// delivery regression test. It verifies that state is synced to disk after
// every successful Handle call, including the final one that returns Stop().
// This ensures that entries durably written to the ingest cache are matched by
// durable BoltDB state, eliminating the duplicate-on-restart window.
func TestWrapJob_SyncCalledAfterEachSuccessfulHandle(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	job := &testJob{
		results: []handleResult{
			{cont: ContinueNow()},
			{cont: ContinueNow()},
			{cont: Stop()},
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	if got := rt.syncCount(); got != 3 {
		t.Errorf("expected 3 sync calls (one per successful Handle), got %d", got)
	}
}

// TestWrapJob_SyncNotCalledOnError verifies that a failed Handle does not
// trigger a state sync — there is nothing reliable to persist.
func TestWrapJob_SyncNotCalledOnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTestRuntime(ctx, cancel)

	job := &testJob{
		results: []handleResult{
			{err: errors.New("transient api error")},
		},
		// Cancel context inside Handle so the subsequent Sleep(jobErrorDelay)
		// returns immediately rather than waiting 30 seconds.
		onCall: func(_ int, _ Runtime) { cancel() },
	}

	WrapJob(job).Run(ctx, rt) //nolint:errcheck

	if got := rt.syncCount(); got != 0 {
		t.Errorf("expected 0 sync calls after error, got %d", got)
	}
}

// TestWrapJob_SyncErrorIsNonFatal verifies that a Sync failure is logged but
// does not stop the adapter or cause Run to return an error.
func TestWrapJob_SyncErrorIsNonFatal(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.syncErr = errors.New("disk full")
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	job := &testJob{
		results: []handleResult{
			{cont: ContinueNow()},
			{cont: Stop()},
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Errorf("expected nil error despite sync failure, got %v", err)
	}
	if job.callCount() != 2 {
		t.Errorf("expected 2 Handle calls, got %d", job.callCount())
	}
}

// TestWrapJob_RetriesAfterError verifies that the adapter retries Handle after
// a transient error rather than giving up.
func TestWrapJob_RetriesAfterError(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	job := &testJob{
		results: []handleResult{
			{err: errors.New("transient error")},
			{cont: Stop()},
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	if job.callCount() != 2 {
		t.Errorf("expected 2 Handle calls (1 error + 1 success), got %d", job.callCount())
	}
	if got := rt.syncCount(); got != 1 {
		t.Errorf("expected 1 sync call (successful call only), got %d", got)
	}
}

// TestWrapJob_StopsOnContextCancellationDuringSleep verifies that when context
// is cancelled while the adapter is sleeping between polls, Run exits promptly.
func TestWrapJob_StopsOnContextCancellationDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTestRuntime(ctx, cancel)

	job := &testJob{
		results: []handleResult{
			{cont: ContinueAfter(time.Minute)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- WrapJob(job).Run(ctx, rt) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not exit after context cancellation")
	}
}

// TestWrapJob_HandlesCanceledContextFromHandle verifies that context.Canceled
// returned directly from Handle is treated as a clean exit, not an error.
func TestWrapJob_HandlesCanceledContextFromHandle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTestRuntime(ctx, cancel)

	job := &testJob{
		results: []handleResult{
			{err: context.Canceled},
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Errorf("expected nil error for context.Canceled, got %v", err)
	}
	if got := rt.syncCount(); got != 0 {
		t.Errorf("expected 0 sync calls on canceled context, got %d", got)
	}
}

// TestWrapJob_MultipleHandleCyclesSyncCount verifies sync call count across a
// longer sequence of successful polls including pagination (ContinueNow) and
// timed polls (ContinueAfter).
func TestWrapJob_MultipleHandleCyclesSyncCount(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	job := &testJob{
		results: []handleResult{
			{cont: ContinueNow()},              // page 1 of pagination
			{cont: ContinueNow()},              // page 2 of pagination
			{cont: ContinueAfter(time.Second)}, // caught up, schedule next poll
			{cont: ContinueNow()},              // new page
			{cont: Stop()},                     // done
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	if got := rt.syncCount(); got != 5 {
		t.Errorf("expected 5 sync calls, got %d", got)
	}
}
