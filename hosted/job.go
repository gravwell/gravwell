package hosted

import (
	"context"
	"errors"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
)

const jobErrorDelay = 30 * time.Second

// Job is implemented by poll-based plugins.
// The runner calls Handle on the schedule returned by each Continuation.
type Job interface {
	Handle(context.Context, Runtime) (*Continuation, error)
}

// Continuation tells the runner when to call Handle again.
// A nil Continuation signals the job is complete and should not be rescheduled.
type Continuation struct {
	Delay time.Duration // 0 = run immediately
}

// ContinueNow returns a Continuation that needs to run immediately.
// Used during paginations.
func ContinueNow() *Continuation { return &Continuation{} }

// ContinueAfter returns a Continuation that needs to be ran after d.
func ContinueAfter(d time.Duration) *Continuation { return &Continuation{Delay: d} }

// Stop returns a nil Continuation, signaling the runner to not reschedule the job.
func Stop() *Continuation { return nil }

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
		cont, err := j.job.Handle(ctx, rt)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}

			rt.Error("handle failed", log.KVErr(err))
			if rt.Sleep(jobErrorDelay) {
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

		if cont == nil {
			return nil
		}

		if cont.Delay > 0 && rt.Sleep(cont.Delay) {
			return nil
		}
	}
}
