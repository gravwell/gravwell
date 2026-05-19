/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package msgraph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

const (
	Name    = "msgraph"
	ID      = "msgraph.ingesters.gravwell.io"
	Version = "1.0.0"
)

func TimestampKey(ct ContentType) string { return string(ct) + "-timestamp" }
func NextLinkKey(ct ContentType) string  { return string(ct) + "-nextlink" }

type Ingester struct {
	config   *Config
	client   *Client
	start    time.Time
	interval time.Duration
}

func NewIngester(cfg *Config) *Ingester {
	start := time.Now().Add(-time.Duration(cfg.Lookback) * time.Hour)
	return &Ingester{
		config:   cfg,
		start:    start,
		interval: time.Duration(cfg.Request_Interval) * time.Second,
	}
}

func (i *Ingester) Run(ctx context.Context, rt hosted.Runtime) error {
	rt.Info("starting msgraph ingester")

	limiter := rate.NewLimiter(rate.Every(time.Minute/time.Duration(i.config.Requests_Per_Minute)), i.config.Requests_Per_Minute)
	retry := utils.NewRetryHttpClient(limiter, 5*time.Second, 10*time.Second, ctx, nil)
	i.client = NewClient(i.config.Graph_Host, i.config.Auth_Host, i.config.Tenant_ID, i.config.Client_ID, i.config.Client_Secret, retry)

	eg, egCtx := errgroup.WithContext(ctx)
	for _, ct := range i.config.Content_Type {
		eg.Go(func() error {
			return i.poll(egCtx, rt, ct)
		})
	}
	return eg.Wait()
}

func (i *Ingester) poll(ctx context.Context, rt hosted.Runtime, ct ContentType) error {
	tag, err := rt.NegotiateTag(ct.Tag(i.config.Tag_Name, i.config.Tag_Prefix))
	if err != nil {
		return fmt.Errorf("negotiate tag for %q: %w", ct, err)
	}

	for !rt.Sleep(i.interval) {
		if err := i.pollOnce(ctx, rt, ct, tag); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			rt.Error("poll error", log.KV("content_type", string(ct)), log.KVErr(err))
		}
	}
	return nil
}

func (i *Ingester) pollOnce(ctx context.Context, rt hosted.Runtime, ct ContentType, tag entry.EntryTag) error {
	nlk := NextLinkKey(ct)
	tsk := TimestampKey(ct)

	lastTS, err := rt.GetTime(tsk)
	if err != nil && !errors.Is(err, storage.ErrStorageNotFound) {
		return fmt.Errorf("load timestamp: %w", err)
	}
	if lastTS.IsZero() || lastTS.After(time.Now()) {
		lastTS = i.start
	}

	nextLink, err := rt.GetString(nlk)
	if err != nil && !errors.Is(err, storage.ErrStorageNotFound) {
		return fmt.Errorf("load next link: %w", err)
	}

	resp, err := i.client.List(ctx, ContentTypeEndpoint(ct), BuildParams(ct, lastTS), nextLink)
	if err != nil {
		return err
	}

	rt.Debug("got results", log.KV("content_type", string(ct)), log.KV("count", len(resp.Value)))

	var latest time.Time
	for _, raw := range resp.Value {
		ts := ExtractTimestamp(ct, raw)
		if ts.After(latest) {
			latest = ts
		}
		if err := rt.Write(entry.Entry{TS: entry.FromStandard(ts), Tag: tag, Data: raw}); err != nil {
			rt.Error("write error", log.KV("content_type", string(ct)), log.KVErr(err))
		}
	}

	if resp.NextLink != "" {
		_ = rt.PutString(nlk, resp.NextLink)
	} else {
		_ = rt.PutString(nlk, "")
		if latest.After(lastTS) {
			_ = rt.PutTime(tsk, latest)
		} else if len(resp.Value) == 0 {
			_ = rt.PutTime(tsk, time.Now().Add(-time.Minute))
		}
	}

	return nil
}
