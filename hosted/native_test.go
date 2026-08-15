package hosted

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNativeRunnerCloseStopsOneIngester covers stopping a single ingester while the runtime
// it was handed stays alive, which is exactly what a config reload does. The process wide
// runtime context is never cancelled here, so a runner that waits on the runtime instead of
// its own context hangs in Close and the reload never finishes.
func TestNativeRunnerCloseStopsOneIngester(t *testing.T) {
	// this context stands in for the runtime shared by every ingester in the process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := newTestRuntime(ctx, cancel)

	// an ingester that only ever waits on the runtime, the way the job adapter does
	var once sync.Once
	started := make(chan struct{})
	ig := ingesterFunc(func(_ context.Context, rt Runtime) error {
		for {
			once.Do(func() { close(started) })
			if rt.Sleep(10 * time.Millisecond) {
				return nil
			}
		}
	})

	nr, err := NewNativeRunner("test.ingesters.gravwell.io", "test", "1.0.0", uuid.New(), &struct{}{}, ig, rt)
	if err != nil {
		t.Fatal(err)
	}
	if err = nr.Start(); err != nil {
		t.Fatal(err)
	}
	// closing before the ingester is actually up would exit on the run loop guard and
	// prove nothing, wait until it is really in its poll loop
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("ingester never started")
	}

	done := make(chan error, 1)
	go func() { done <- nr.Close() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("close returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not return, the ingester cannot be stopped on its own")
	}
	if nr.Running() {
		t.Error("runner still reports running after close")
	}
	if ctx.Err() != nil {
		t.Error("closing one runner cancelled the shared runtime context")
	}
}

type ingesterFunc func(context.Context, Runtime) error

func (f ingesterFunc) Run(ctx context.Context, rt Runtime) error { return f(ctx, rt) }
