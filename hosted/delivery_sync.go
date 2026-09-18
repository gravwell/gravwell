package hosted

import (
	"context"
	"errors"
	"time"
)

// SyncDelivered is an optional runtime capability for destructive source acks.
// Bucket state synchronization is separate and cannot substitute for this.
func (nr *NativeRuntime) SyncDelivered(ctx context.Context, timeout time.Duration) error {
	if nr == nil || nr.igst == nil {
		return ErrNotReady
	}
	return nr.igst.SyncDelivered(ctx, timeout)
}
func (sr *scopedRuntime) SyncDelivered(ctx context.Context, timeout time.Duration) error {
	if e := sr.ctx.Err(); e != nil {
		return e
	}
	barrier, ok := sr.Runtime.(interface {
		SyncDelivered(context.Context, time.Duration) error
	})
	if !ok {
		return errors.New("runtime does not provide acknowledged delivery")
	}
	if e := barrier.SyncDelivered(ctx, timeout); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	return sr.ctx.Err()
}
