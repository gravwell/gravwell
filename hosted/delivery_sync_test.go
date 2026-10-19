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

// TestAcknowledgedWriterForResolvesTheWrappedRuntime proves capability
// resolution inspects what will actually perform the write. The scoped runtime
// declares WriteAcknowledged unconditionally, so a bare type assertion against
// it always succeeds and the failure would only surface from the first write,
// after a plugin had negotiated a tag and touched its state.
func TestAcknowledgedWriterForResolvesTheWrappedRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unsupported := newTestRuntime(ctx, cancel)
	scopedUnsupported := &scopedRuntime{Runtime: unsupported, ctx: ctx}
	if _, ok := any(scopedUnsupported).(AcknowledgedWriter); !ok {
		t.Fatal("scoped runtime no longer reproduces the vacuous assertion")
	}
	if writer, ok := AcknowledgedWriterFor(scopedUnsupported); ok || writer != nil {
		t.Fatalf("unsupported wrapped runtime resolved writer=%v ok=%v", writer, ok)
	}

	base := &acknowledgedTestRuntime{Runtime: newTestRuntime(ctx, cancel)}
	scopedSupported := &scopedRuntime{Runtime: base, ctx: ctx}
	writer, ok := AcknowledgedWriterFor(scopedSupported)
	if !ok || writer == nil {
		t.Fatalf("supported wrapped runtime resolved writer=%v ok=%v", writer, ok)
	}
	// Resolution hands back the wrapper, so scoped cancellation still applies.
	if writer != AcknowledgedWriter(scopedSupported) {
		t.Fatalf("resolved writer=%T want the scoped runtime", writer)
	}
	if err := writer.WriteAcknowledged(ctx, entry.Entry{Data: []byte("test")}); err != nil {
		t.Fatal(err)
	}
	if base.calls != 1 {
		t.Fatalf("calls=%d want=1", base.calls)
	}

	// A runtime that implements the contract directly needs no unwrapping.
	if writer, ok := AcknowledgedWriterFor(base); !ok || writer == nil {
		t.Fatalf("direct runtime resolved writer=%v ok=%v", writer, ok)
	}
	// A runtime that neither wraps nor implements it is rejected.
	if writer, ok := AcknowledgedWriterFor(unsupported); ok || writer != nil {
		t.Fatalf("unsupported runtime resolved writer=%v ok=%v", writer, ok)
	}

	// Nesting does not reintroduce the vacuous answer.
	nestedUnsupported := &scopedRuntime{Runtime: scopedUnsupported, ctx: ctx}
	if writer, ok := AcknowledgedWriterFor(nestedUnsupported); ok || writer != nil {
		t.Fatalf("nested unsupported runtime resolved writer=%v ok=%v", writer, ok)
	}
	nestedSupported := &scopedRuntime{Runtime: scopedSupported, ctx: ctx}
	if writer, ok := AcknowledgedWriterFor(nestedSupported); !ok || writer != AcknowledgedWriter(nestedSupported) {
		t.Fatalf("nested supported runtime resolved writer=%v ok=%v", writer, ok)
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
