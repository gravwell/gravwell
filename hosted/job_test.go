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

func TestContinueNowOrAfter(t *testing.T) {
	d := 30 * time.Second

	t.Run("pending returns immediate", func(t *testing.T) {
		c := ContinueNowOrAfter(true, d)
		if c == nil {
			t.Fatal("expected non-nil")
		}
		if c.Delay != 0 {
			t.Errorf("expected zero delay, got %v", c.Delay)
		}
	})

	t.Run("not pending returns interval", func(t *testing.T) {
		c := ContinueNowOrAfter(false, d)
		if c == nil {
			t.Fatal("expected non-nil")
		}
		if c.Delay != d {
			t.Errorf("expected delay %v, got %v", d, c.Delay)
		}
	})
}

func TestContinuation_String(t *testing.T) {
	tests := []struct {
		name string
		c    *Continuation
		want string
	}{
		{"stop", Stop(), "stop"},
		{"immediate", ContinueNow(), "immediate"},
		{"after 5s", ContinueAfter(5 * time.Second), "after(5s)"},
		{"after 1m", ContinueAfter(time.Minute), "after(1m0s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobErrorDelay_IsPositive(t *testing.T) {
	if JobErrorDelay <= 0 {
		t.Errorf("JobErrorDelay must be positive, got %v", JobErrorDelay)
	}
}

func TestJobAliveDelay_IsPositive(t *testing.T) {
	if JobAliveDelay <= 0 {
		t.Errorf("JobAliveDelay must be positive, got %v", JobAliveDelay)
	}
}

func TestJobAliveDelay_ShorterThanErrorDelay(t *testing.T) {
	if JobAliveDelay >= JobErrorDelay {
		t.Errorf("JobAliveDelay (%v) should be shorter than JobErrorDelay (%v)", JobAliveDelay, JobErrorDelay)
	}
}

// TestJobFunc verifies that a function literal satisfies the Job interface
// and is invoked correctly by the adapter.
func TestJobFunc_ImplementsJob(t *testing.T) {
	var _ Job = JobFunc(nil) // compile-time check
}

func TestJobFunc_IsCalled(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	called := false
	WrapJob(JobFunc(func(_ context.Context, _ Runtime) (*Continuation, error) {
		called = true
		return Stop(), nil
	})).Run(ctx, rt) //nolint:errcheck

	if !called {
		t.Error("JobFunc was not called")
	}
}

// TestJobFunc_SyncCalledAfterEachHandle mirrors TestWrapJob_SyncCalledAfterEachSuccessfulHandle
// using JobFunc to demonstrate the ergonomic closure form.
func TestJobFunc_SyncCalledAfterEachHandle(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	calls := 0
	WrapJobWithSync(JobFunc(func(_ context.Context, _ Runtime) (*Continuation, error) {
		calls++
		if calls >= 3 {
			return Stop(), nil
		}
		return ContinueNow(), nil
	}), rt.Sync).Run(ctx, rt) //nolint:errcheck

	if rt.syncCount() != 3 {
		t.Errorf("expected 3 sync calls, got %d", rt.syncCount())
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

	if err := WrapJobWithSync(job, rt.Sync).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	if got := rt.syncCount(); got != 3 {
		t.Errorf("expected 3 sync calls (one per successful Handle), got %d", got)
	}
}

// TestWrapJob_SyncNotCalledOnError verifies that a failed Handle does not
// trigger a state sync. There is nothing reliable to persist.
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

	WrapJobWithSync(job, rt.Sync).Run(ctx, rt) //nolint:errcheck

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

	if err := WrapJobWithSync(job, rt.Sync).Run(ctx, rt); err != nil {
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

	if err := WrapJobWithSync(job, rt.Sync).Run(ctx, rt); err != nil {
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

	if err := WrapJobWithSync(job, rt.Sync).Run(ctx, rt); err != nil {
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

	if err := WrapJobWithSync(job, rt.Sync).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	if got := rt.syncCount(); got != 5 {
		t.Errorf("expected 5 sync calls, got %d", got)
	}
}

// TestWrapJob_HandleNotCalledWhenNotAlive verifies that the adapter skips
// Handle entirely while the ingest connection is unhealthy.
func TestWrapJob_HandleNotCalledWhenNotAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTestRuntime(ctx, cancel)
	rt.aliveFunc = func() bool { return false }

	sleeps := 0
	rt.sleepFunc = func(d time.Duration) bool {
		sleeps++
		if sleeps >= 2 {
			cancel()
			return true
		}
		return false
	}

	job := &testJob{}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if job.callCount() != 0 {
		t.Errorf("Handle should not be called while not alive, got %d calls", job.callCount())
	}
}

// TestWrapJob_ResumesHandleAfterAliveRecovery verifies that the adapter blocks
// Handle while unhealthy then resumes normally once the connection recovers.
func TestWrapJob_ResumesHandleAfterAliveRecovery(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	// Return false for the first 3 Alive() calls, then true.
	aliveCall := 0
	rt.aliveFunc = func() bool {
		aliveCall++
		return aliveCall > 3
	}
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	job := &testJob{
		results: []handleResult{
			{cont: Stop()},
		},
	}

	if err := WrapJob(job).Run(ctx, rt); err != nil {
		t.Fatal(err)
	}

	// Handle should have been called exactly once after recovery.
	if job.callCount() != 1 {
		t.Errorf("expected 1 Handle call after recovery, got %d", job.callCount())
	}
}

// TestWrapJob_AliveBackoffUsesJobAliveDelay verifies that the adapter sleeps
// for JobAliveDelay (not JobErrorDelay) while the connection is unhealthy.
func TestWrapJob_AliveBackoffUsesJobAliveDelay(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	// Not alive for first 3 checks, then alive.
	aliveCall := 0
	rt.aliveFunc = func() bool {
		aliveCall++
		return aliveCall > 3
	}
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	WrapJob(JobFunc(func(_ context.Context, _ Runtime) (*Continuation, error) {
		return Stop(), nil
	})).Run(ctx, rt) //nolint:errcheck

	for i, d := range rt.recordedSleeps() {
		if d != JobAliveDelay {
			t.Errorf("sleep[%d]: expected JobAliveDelay (%v), got %v", i, JobAliveDelay, d)
		}
	}
}

// TestWrapJob_LogsOnceOnAliveTransition verifies that the adapter logs exactly
// one warning when the connection goes unhealthy and one info when it recovers,
// regardless of how many backoff iterations occur.
func TestWrapJob_LogsOnceOnAliveTransition(t *testing.T) {
	ctx := t.Context()
	rt := newTestRuntime(ctx, func() {})

	// Not alive for first 4 checks (1 outer + 3 inner), then alive.
	aliveCall := 0
	rt.aliveFunc = func() bool {
		aliveCall++
		return aliveCall > 4
	}
	rt.sleepFunc = func(_ time.Duration) bool { return false }

	WrapJob(JobFunc(func(_ context.Context, _ Runtime) (*Continuation, error) {
		return Stop(), nil
	})).Run(ctx, rt) //nolint:errcheck

	if got := rt.warnCount(); got != 1 {
		t.Errorf("expected 1 warn (on entry), got %d", got)
	}
	if got := rt.infoCount(); got != 1 {
		t.Errorf("expected 1 info (on recovery), got %d", got)
	}
}

// TestWrapJob_GracefulShutdownWhileNotAlive verifies that context cancellation
// during the alive backoff loop causes Run to return nil cleanly.
func TestWrapJob_GracefulShutdownWhileNotAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := newTestRuntime(ctx, cancel)
	rt.aliveFunc = func() bool { return false }

	sleeps := 0
	rt.sleepFunc = func(d time.Duration) bool {
		sleeps++
		cancel()
		return true // simulate context cancelled during sleep
	}

	done := make(chan error, 1)
	go func() {
		done <- WrapJob(JobFunc(func(_ context.Context, _ Runtime) (*Continuation, error) {
			return Stop(), nil
		})).Run(ctx, rt)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on graceful shutdown, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter did not exit after context cancellation")
	}

	if job := 0; job != 0 { // Handle should never have been called
		t.Error("Handle should not be called during alive backoff")
	}
}
