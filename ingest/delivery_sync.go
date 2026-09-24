package ingest

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// SyncDelivered uses the native server acknowledgement barrier, and rejects
// cache backlog or absent live connections. It never equates an in-memory
// WriteEntry enqueue or disk-cache availability with backend acknowledgement.
func (im *IngestMuxer) SyncDelivered(ctx context.Context, timeout time.Duration) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if timeout <= 0 {
		return errors.New("delivery sync requires a positive timeout")
	}
	if atomic.LoadInt32(&im.connHot) == 0 {
		return ErrAllConnsDown
	}
	if e := im.SyncContext(ctx, timeout); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if atomic.LoadInt32(&im.connHot) == 0 {
		return ErrAllConnsDown
	}
	if im.cacheEnabled && (im.cache.Size() != 0 || im.bcache.Size() != 0) {
		return errors.New("delivery sync has outstanding cache backlog")
	}
	return nil
}
