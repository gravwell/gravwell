package main

import (
	"context"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/log"
)

const (
	manualTickerInterval = time.Minute
)

func start(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger) (err error) {
	wg.Add(1)
	go manualScanner(wg, ctx, buckets, ot, lg)
	return
}

func manualScanner(wg *sync.WaitGroup, ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger) {
	defer wg.Done()
	fullScan(ctx, buckets, ot, lg)
	if ctx.Err() != nil {
		return
	}

	lg.Info("completed standup scan")
	ticker := time.NewTicker(manualTickerInterval)
	defer ticker.Stop()

	//ticker loop
loop:
	for {
		select {
		case <-ticker.C:
			fullScan(ctx, buckets, ot, lg)
		case <-ctx.Done():
			break loop
		}
	}
}

func fullScan(ctx context.Context, buckets []*BucketReader, ot *objectTracker, lg *log.Logger) {
	for _, b := range buckets {
		if err := b.ManualScan(ctx, ot); err != nil {
			lg.Error("failed to scan S3 bucket objects",
				log.KV("bucket", b.Name),
				log.KVErr(err))
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err := ot.Flush(); err != nil {
		lg.Error("failed to flush S3 state file", log.KVErr(err))
	}
}
