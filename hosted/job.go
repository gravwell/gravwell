package hosted

import (
	"context"
	"errors"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	// JobErrorDelay is the time the adapter waits before retrying after a failed Handle call.
	JobErrorDelay = 30 * time.Second
	// JobAliveDelay is the time the adapter waits before retrying after detecting the ingester is unhealthy.
	JobAliveDelay = 10 * time.Second
)

// Job is implemented by poll-based plugins.
// The runner calls Handle on the schedule returned by each Continuation.
type Job interface {
	Handle(context.Context, Runtime) (*Continuation, error)
}

// JobFunc is a function type that implements Job, allowing closures to be used
// directly as jobs without defining a named struct.
type JobFunc func(context.Context, Runtime) (*Continuation, error)

func (f JobFunc) Handle(ctx context.Context, rt Runtime) (*Continuation, error) {
	return f(ctx, rt)
}

// Continuation tells the runner when to call Handle again.
// A nil Continuation signals the job is complete and should not be rescheduled.
type Continuation struct {
	Delay time.Duration // 0 = run immediately
}

// String implements fmt.Stringer.
func (c *Continuation) String() string {
	if c == nil {
		return "stop"
	}
	if c.Delay == 0 {
		return "immediate"
	}
	return "after(" + c.Delay.String() + ")"
}

// ContinueNow returns a Continuation that needs to run immediately.
// Used during paginations.
func ContinueNow() *Continuation { return &Continuation{} }

// ContinueAfter returns a Continuation that needs to be ran after d.
func ContinueAfter(d time.Duration) *Continuation { return &Continuation{Delay: d} }

// Stop returns a nil Continuation, signaling the runner to not reschedule the job.
func Stop() *Continuation { return nil }

// ContinueNowOrAfter returns ContinueNow if pending is true, otherwise ContinueAfter(d).
// This collapses the common fan-out pattern at the end of Handle implementations
// that aggregate pagination state across multiple concurrent content types.
func ContinueNowOrAfter(pending bool, d time.Duration) *Continuation {
	if pending {
		return ContinueNow()
	}
	return ContinueAfter(d)
}

// WrapJob adapts a Job to the Ingester interface for use with NativeRunner.
// The adapter owns the poll looping, errors from Handle are logged and retried
// after jobErrorDelay rather than surfacing to the runner's restart logic.
func WrapJob(j Job) Ingester {
	return &jobIngesterAdapter{job: j}
}

type jobIngesterAdapter struct {
	job Job
}

func (j *jobIngesterAdapter) Run(ctx context.Context, rt Runtime) error {
	for {
		// There's no point in calling Handle() if writes will block.
		// The cache is full and the indexer is unreachable.
		// Back off and wait for the connection to recover.
		if !rt.Alive() {
			rt.Warn("ingest connection is unhealthy, backing off")
			for !rt.Alive() {
				if rt.Sleep(JobAliveDelay) {
					return nil
				}
			}
			rt.Info("ingest connection recovered, continuing")
			continue
		}

		cont, err := j.job.Handle(ctx, rt)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}

			rt.Error("handle failed", log.KVErr(err))
			if rt.Sleep(JobErrorDelay) {
				return nil
			}

			continue
		}

		// Sync our state before sleeping.
		// This can narrow down the duplicatation window to only entries that are
		// still in the muxer's in-memory channel.
		// Anything that has reached the disk cache will be in storage.
		if syncErr := rt.Sync(); syncErr != nil {
			rt.Error("sync state failed", log.KVErr(syncErr))
		}

		rt.Debug("handle complete", log.KV("next", cont.String()))
		if cont == nil {
			return nil
		}

		if cont.Delay > 0 && rt.Sleep(cont.Delay) {
			return nil
		}
	}
}
