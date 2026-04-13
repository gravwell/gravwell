/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package mimecast is a plugin used for reading data from Mimecast audit logs.
// It supports both SIEM MTA logs, and general audit logs.
package mimecast

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

const (
	Name    string = `mimecast`
	ID      string = `mimecast.ingesters.gravwell.io`
	Version string = `1.0.0` // must be canonical version string with only major.minor.point
)

type Mimecast struct {
	c            *Client
	apis         []Api
	includeAudit bool // if the audit api should be polled
	start        time.Time
	conf         *Config
	interval     time.Duration
}

func New(conf *Config) *Mimecast {
	apis := make([]Api, 0)
	audit := false
	for _, a := range conf.Api {
		if a == AuditApi {
			audit = true
			continue
		}
		_, ok := SIEMApiEvents[a]
		if ok {
			apis = append(apis, a)
		}
	}

	start := time.Now().Add(time.Duration(-conf.Lookback) * time.Hour)

	return &Mimecast{
		conf:         conf,
		apis:         apis,
		includeAudit: audit,
		start:        start,
		interval:     time.Duration(conf.Request_Interval) * time.Second,
	}
}

func (m *Mimecast) tag(a Api) string {
	return a.Tag(m.conf.Tag_Name, m.conf.Tag_Prefix)
}

func (m *Mimecast) cursor(api Api) string {
	return string(api) + "-cursor"
}

func (m *Mimecast) timestamp(api Api) string {
	return string(api) + "-timestamp"
}

func (m *Mimecast) get(rt hosted.Runtime, api Api, defaultTs time.Time) (cursor string, ts time.Time, err error) {
	cursor, serr := rt.GetString(m.cursor(api))
	if serr != nil && !errors.Is(serr, storage.ErrStorageNotFound) {
		err = fmt.Errorf("error getting cursor, api: %s, error: %w", string(api), serr)
		return
	}

	ts, terr := rt.GetTime(m.timestamp(api))
	if terr != nil && !errors.Is(terr, storage.ErrStorageNotFound) {
		err = fmt.Errorf("error getting timestamp: api: %s, error: %w", string(api), terr)
		return
	} else if ts.IsZero() || ts.After(time.Now()) {
		ts = defaultTs
	}
	return
}

func (m *Mimecast) Run(ctx context.Context, rt hosted.Runtime) error {
	rt.Info("starting mimecast")

	limit := rate.NewLimiter(rate.Every(time.Minute/time.Duration(m.conf.Requests_Per_Minute)), m.conf.Requests_Per_Minute)
	retry := utils.NewRetryHttpClient(limit, 3*time.Second, 10*time.Second, ctx, nil)
	m.c = NewClient(m.conf.Host, m.conf.Client_Id, m.conf.Client_Secret, retry)

	eg, ectx := errgroup.WithContext(ctx)
	if m.includeAudit {
		eg.Go(func() error {
			return m.audit(ectx, rt)
		})
	}
	if len(m.apis) > 0 {
		eg.Go(func() error {
			return m.mta(ectx, rt)
		})
	}
	return eg.Wait()
}

func (m *Mimecast) audit(ctx context.Context, rt hosted.Runtime) error {
	api := log.KV("api", AuditApi)
	tag, err := rt.NegotiateTag(m.tag(AuditApi))
	if err != nil {
		return err
	}
	for !rt.Sleep(m.interval) {
		cursor, lts, err := m.get(rt, AuditApi, m.start)
		if err != nil {
			rt.Error("error getting storage data", api, log.KVErr(err))
			continue
		}

		tr := NewTimeRange(lts, time.Now())

		ts := time.Now()
		if cursor != "" {
			rt.Debug("fetching next page of events", api)
		} else {
			rt.Debug("fetching events between", api, log.KV("start", lts), log.KV("end", ts))
		}
		r, err := m.c.GetRawAuditEvents(ctx, tr, cursor)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				rt.Error("request error", api, log.KVErr(err))
			}
			continue
		}

		rt.Debug("got events", api, log.KV("count", len(r.Data)))
		for _, d := range r.Data {
			data, err := parse[AuditData](bytes.NewReader(d))
			if err != nil {
				rt.Error("error parsing audit record", log.KVErr(err))
				continue
			}
			ets, err := time.Parse(AuditTimeFormat, data.EventTime)
			if err != nil {
				rt.Error("error parsing time for event", api, log.KVErr(err))
				continue
			}
			e := entry.Entry{
				TS:   entry.FromStandard(ets),
				Data: d,
				Tag:  tag,
			}
			err = rt.Write(e)
			if err != nil {
				rt.Error("error writing entry", api, log.KVErr(err))
				continue
			}
			rt.Debug("wrote audit entry", api, log.KV("ts", e.TS))
		}

		rt.PutString(m.cursor(AuditApi), r.Meta.Pagination.Next)
		// don't advance time until we process the entire timespan
		if len(r.Data) == 0 {
			rt.Debug("moving forward in time", api, log.KV("to", tr.End))
			rt.PutTime(m.timestamp(AuditApi), tr.End)
		}

	}

	return nil
}

func (m *Mimecast) mta(ctx context.Context, rt hosted.Runtime) error {
	eg, ectx := errgroup.WithContext(ctx)
	for _, a := range m.apis {
		eg.Go(func() error {
			return m.mtaEvent(ectx, rt, a)
		})
	}
	return eg.Wait()
}

func (m *Mimecast) mtaEvent(ctx context.Context, rt hosted.Runtime, api Api) error {
	event := SIEMApiEvents[api]
	tag, err := rt.NegotiateTag(m.tag(api))
	if err != nil {
		return err
	}
	for !rt.Sleep(m.interval) {
		cursor, lts, err := m.get(rt, api, m.start)
		if err != nil {
			rt.Error("error getting storage data", log.KV("api", api), log.KVErr(err))
			continue
		}

		tr := NewTimeRange(lts, time.Now())
		tr.ClampStart(7*24*time.Hour, time.Minute)

		rt.Debug("fetching batch between", log.KV("api", api), log.KV("start", tr.Start), log.KV("end", tr.End))

		events, err := m.c.GetSIEMEventBatch(ctx, event, tr, cursor)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				rt.Error("request error", log.KV("api", api), log.KVErr(err))
			}
			continue
		}
		var last time.Time
		rt.Debug("got batches", log.KV("api", api), log.KV("count", len(events.Value)))
		for _, batch := range events.Value {
			last, err = m.handleMtaBatch(ctx, rt, tag, tr, batch, api)
			if err != nil {
				rt.Error("error handling mta batch", log.KV("api", api), log.KVErr(err))
				continue
			}
		}

		// Unlike audit the mta cursor ensures we never get dupes even if we request the same time range.
		// We track the last timestamp as there is lag in the batch api so just because no events were returned
		// does not mean that all events have been sent to us for a given time range.
		if last.IsZero() { // there were no events in the range
			last = tr.End
		}
		rt.PutString(m.cursor(api), events.NextPage)
		if events.IsCaughtUp { // Progress forward in time
			rt.Debug("caught up, moving forward in time", log.KV("api", api), log.KV("to", last))
			rt.PutTime(m.timestamp(api), last)
		}
	}

	return nil
}

func (m *Mimecast) handleMtaPage(rt hosted.Runtime, tag entry.EntryTag, page []json.RawMessage, api Api) (time.Time, error) {
	var first time.Time
	var last time.Time
	if len(page) == 0 {
		return last, nil
	}
	count := 0
	for _, event := range page {
		if len(event) == 0 {
			rt.Debug("skipping empty mta event")
			continue
		}
		data, err := parse[MtaEventData](bytes.NewReader(event))
		if err != nil {
			rt.Error("failed to parse mta event", log.KVErr(err))
			continue
		}
		ts := time.UnixMilli(data.Timestamp)
		if first.IsZero() {
			first = ts
		}

		e := entry.Entry{
			TS:   entry.FromStandard(ts),
			Data: event,
			Tag:  tag,
		}
		if err := rt.Write(e); err != nil {
			rt.Error("failed to write mta event", log.KVErr(err))
			continue
		}
		last = ts
		count++
	}
	rt.Debug("finished processing mta events", log.KV("processed-entries", count), log.KV("first-timestamp", first), log.KV("last-timestamp", last), log.KV("api", api))
	return last, nil
}

func (m *Mimecast) handleMtaBatch(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag, tr *TimeRange, event SIEMBatchEvent, api Api) (time.Time, error) {
	var first time.Time
	var last time.Time
	entries, err := m.entries(ctx, event.URL)
	if err != nil {
		return last, err
	}
	count := 0
	for line := range entries {
		if len(line) == 0 {
			rt.Debug("skipping empty mta event")
			continue
		}
		data, err := parse[MtaEventData](bytes.NewReader(line))
		if err != nil {
			rt.Error("failed to parse mta event", log.KVErr(err))
			continue
		}
		ts := time.UnixMilli(data.Timestamp)
		if first.IsZero() {
			first = ts
		}

		e := entry.Entry{
			TS:   entry.FromStandard(ts),
			Data: line,
			Tag:  tag,
		}
		if err := rt.Write(e); err != nil {
			rt.Error("failed to write mta event", log.KVErr(err))
			continue
		}
		last = ts
		count++
	}
	if count == 0 {
		rt.Debug("no new events to ingest in range", log.KV("start", tr.Start), log.KV("end", tr.End), log.KV("api", api))
	} else {
		rt.Debug("finished processing mta events", log.KV("processed-entries", count), log.KV("first-timestamp", first), log.KV("last-timestamp", last), log.KV("api", api))
	}
	return last, nil
}

func (m *Mimecast) entries(ctx context.Context, url string) (iter.Seq[[]byte], error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The DefaultClient is used here as the event.URL is a presigned URL (generally to an aws S3 bucket).
	// Rate Limits don't apply, and using m.client would pass credentials to AWS,
	response, err := http.DefaultClient.Do(request)
	// can't defer drain since we return an iterator
	if err != nil {
		utils.DrainResponse(response)
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		utils.DrainResponse(response)
		return nil, fmt.Errorf("request failed: %s", response.Status)
	}

	gzreader, err := gzip.NewReader(response.Body)
	if err != nil {
		utils.DrainResponse(response)
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	scanner := bufio.NewScanner(gzreader)

	return func(yield func([]byte) bool) {
		defer utils.DrainResponse(response)
		defer gzreader.Close()
		for scanner.Scan() {
			if !yield(scanner.Bytes()) {
				return
			}
		}
	}, nil
}
