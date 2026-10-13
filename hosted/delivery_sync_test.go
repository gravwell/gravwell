package hosted

import (
	"context"
	"errors"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type acknowledgedTestRuntime struct {
	Runtime
	calls int
	err   error
	after func()
}

func (r *acknowledgedTestRuntime) WriteAcknowledged(context.Context, entry.Entry) error {
	r.calls++
	if r.after != nil {
		r.after()
	}
	return r.err
}

func TestScopedRuntimeForwardsAcknowledgedWriteAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	base := &acknowledgedTestRuntime{Runtime: newTestRuntime(ctx, cancel)}
	scoped := &scopedRuntime{Runtime: base, ctx: ctx}
	if err := scoped.WriteAcknowledged(ctx, entry.Entry{Data: []byte("test")}); err != nil {
		t.Fatal(err)
	}
	if base.calls != 1 {
		t.Fatalf("calls=%d want=1", base.calls)
	}
	base.err = errors.New("synthetic acknowledgement failure")
	if err := scoped.WriteAcknowledged(ctx, entry.Entry{}); !errors.Is(err, base.err) {
		t.Fatalf("error=%v want=%v", err, base.err)
	}
	cancel()
	if err := scoped.WriteAcknowledged(ctx, entry.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want cancellation", err)
	}
	if base.calls != 2 {
		t.Fatalf("canceled write reached base runtime: calls=%d", base.calls)
	}
}

func TestScopedRuntimeDoesNotMaskDurableAcknowledgementWithLateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	base := &acknowledgedTestRuntime{Runtime: newTestRuntime(ctx, cancel), after: cancel}
	scoped := &scopedRuntime{Runtime: base, ctx: ctx}
	if err := scoped.WriteAcknowledged(ctx, entry.Entry{Data: []byte("test")}); err != nil {
		t.Fatalf("durable acknowledgement was masked by cancellation: %v", err)
	}
	if base.calls != 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("calls=%d cancellation=%v", base.calls, ctx.Err())
	}
}

func TestScopedRuntimeRejectsMissingAcknowledgedContract(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scoped := &scopedRuntime{Runtime: newTestRuntime(ctx, cancel), ctx: ctx}
	if err := scoped.WriteAcknowledged(ctx, entry.Entry{}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("error=%v want ErrNotReady", err)
	}
}

func TestScopedRuntimeHonorsCallerCancellationBeforeAcknowledgedWrite(t *testing.T) {
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	base := &acknowledgedTestRuntime{Runtime: newTestRuntime(runnerCtx, cancelRunner)}
	scoped := &scopedRuntime{Runtime: base, ctx: runnerCtx}
	callCtx, cancelCall := context.WithCancel(context.Background())
	cancelCall()
	if err := scoped.WriteAcknowledged(callCtx, entry.Entry{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want caller cancellation", err)
	}
	if base.calls != 0 {
		t.Fatalf("canceled caller reached base runtime: calls=%d", base.calls)
	}
}
