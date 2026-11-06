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
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/stretchr/testify/require"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func init() {
	// queueRunner logs through the package-global lg; give it a real, discarding
	// logger so tests don't panic on a nil receiver.
	lg = log.NewDiscardLogger()
}

// fakeSQS is a hand-written sqsClient double used to drive queueRunner's
// receive loop without a real SQS connection.
type fakeSQS struct {
	calls      atomic.Int64
	getMessage func(ctx context.Context) ([]types.Message, error)
}

func (f *fakeSQS) GetMessages(ctx context.Context) ([]types.Message, error) {
	f.calls.Add(1)
	return f.getMessage(ctx)
}

func (f *fakeSQS) DeleteMessages(ctx context.Context, m []types.Message) error {
	return nil
}

// waitTimeout waits for wg to finish, returning false if timeout elapses first.
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// waitForGoroutineBaseline polls runtime.NumGoroutine until it settles back
// to within tolerance of baseline, or fails the test once timeout elapses.
// This is how we detect the goroutine leak the double-send bug caused: every
// erroring receive spawned a goroutine that could never return, so the count
// would climb and never come back down.
func waitForGoroutineBaseline(t *testing.T, baseline, tolerance int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		runtime.Gosched()
		if n := runtime.NumGoroutine(); n <= baseline+tolerance {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count did not settle: have %d, want <= %d", runtime.NumGoroutine(), baseline+tolerance)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestQueueRunner_ErrorLoopDoesNotLeakGoroutines is a regression test for a bug
// where, on a GetMessages error, the per-iteration goroutine did `c <- nil`
// and then unconditionally fell through to also do `c <- o` -- a double send
// on a single-receiver channel. Since only one value is ever received per
// loop iteration, each erroring iteration left one goroutine permanently
// blocked trying to deliver its second, unwanted send: a goroutine leaked on
// every single receive error.
//
// The fix buffers the channel by one and returns immediately after the error
// send. This test drives many consecutive receive errors and asserts the
// loop keeps making progress (GetMessages keeps getting called) and that no
// goroutines are left behind once the loop is stopped.
func TestQueueRunner_ErrorLoopDoesNotLeakGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	fake := &fakeSQS{
		getMessage: func(ctx context.Context) ([]types.Message, error) {
			return nil, errTestGetMessages
		},
	}

	done := make(chan bool)
	var wg sync.WaitGroup
	hcfg := &handlerConfig{
		Name: "errtest",
		SQS:  fake,
		wg:   &wg,
		done: done,
		// hcfg.ctx is only consulted by the ERROR_BACKOFF sleep in the receive
		// loop; pre-cancelling it makes that sleep return immediately instead
		// of blocking real wall-clock time for every erroring iteration, so the
		// loop can churn through many iterations quickly.
		ctx: canceledContext(),
	}

	const wantIterations = 25

	wg.Add(1)
	go queueRunner(context.Background(), hcfg)

	require.Eventually(t, func() bool {
		return fake.calls.Load() >= wantIterations
	}, 2*time.Second, time.Millisecond, "queueRunner did not keep retrying after receive errors")

	close(done)
	require.True(t, waitTimeout(&wg, 2*time.Second), "queueRunner did not return after done was closed")

	waitForGoroutineBaseline(t, baseline, 5, 2*time.Second)
}

// TestQueueRunner_ShutdownDuringInFlightFetch exercises the case where
// hcfg.done is closed while a GetMessages call is still in flight (simulating
// a real long poll). queueRunner must return promptly rather than waiting for
// the fetch, and once the fetch's context is eventually canceled and it
// unblocks, its buffered send onto the internal channel must not panic or
// hang -- it has no receiver left, so the buffer of one must silently absorb
// it.
func TestQueueRunner_ShutdownDuringInFlightFetch(t *testing.T) {
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	fake := &fakeSQS{
		getMessage: func(ctx context.Context) ([]types.Message, error) {
			close(started)
			<-ctx.Done() // blocks like a real long poll until shutdown cancels ctx
			return nil, ctx.Err()
		},
	}

	done := make(chan bool)
	var wg sync.WaitGroup
	hcfg := &handlerConfig{
		Name: "shutdowntest",
		SQS:  fake,
		wg:   &wg,
		done: done,
		ctx:  ctx,
	}

	wg.Add(1)
	go queueRunner(ctx, hcfg)

	<-started // fetch is now in flight and blocked on ctx

	close(done)

	returned := waitTimeout(&wg, 250*time.Millisecond)
	require.True(t, returned, "queueRunner waited for the in-flight fetch instead of returning on done")

	// Now unblock the still-running fetch goroutine; its eventual send onto
	// the buffered channel should complete silently rather than hang forever.
	cancel()

	waitForGoroutineBaseline(t, baseline, 5, 2*time.Second)
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

var errTestGetMessages = &fakeError{"simulated GetMessages error"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }
