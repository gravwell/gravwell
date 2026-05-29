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
	"sync"
	"sync/atomic"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

const (
	Name    string = `mimecast`
	ID      string = `mimecast.ingesters.gravwell.io`
	Version string = `1.0.0`
)

type Mimecast struct {
	c            *Client
	mu           sync.Mutex
	limiter      *rate.Limiter
	apis         []Api
	includeAudit bool
	start        time.Time
	conf         *Config
}

func New(conf *Config) *Mimecast {
	apis := make([]Api, 0)
	audit := false
	for _, a := range conf.Api {
		if a == AuditApi {
			audit = true
			continue
		}
		if _, ok := SIEMApiEvents[a]; ok {
			apis = append(apis, a)
		}
	}
	limiter :=
		rate.NewLimiter(rate.Every(time.Minute/time.Duration(conf.Requests_Per_Minute)),
			conf.Requests_Per_Minute)
	return &Mimecast{
		conf:         conf,
		apis:         apis,
		includeAudit: audit,
		start:        time.Now().Add(-conf.LookbackDuration()),
		limiter:      limiter,
	}
}

func (m *Mimecast) initClient(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.c != nil {
		return
	}
	retry := utils.NewRetryHttpClient(m.limiter, 3*time.Second, 10*time.Second, ctx, nil)
	m.c = NewClient(m.conf.Host, m.conf.Client_Id, m.conf.Client_Secret, retry)
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

func (m *Mimecast) get(rt hosted.Runtime, api Api, defaultTs time.Time) (cursor string,
	ts time.Time, err error) {
	if cursor, err = hosted.GetStringOrDefault(rt, m.cursor(api), ""); err != nil {
		err = fmt.Errorf("get cursor for %s: %w", api, err)
		return
	}
	if ts, err = hosted.GetTimeOrDefault(rt, m.timestamp(api), defaultTs); err != nil {
		err = fmt.Errorf("get timestamp for %s: %w", api, err)
		return
	}
	return
}

func (m *Mimecast) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation,
	error) {
	m.initClient(rt.Context())

	eg, egCtx := errgroup.WithContext(ctx)
	var hasPending atomic.Bool

	if m.includeAudit {
		eg.Go(func() error {
			pending, err := m.auditOnce(egCtx, rt)
			if pending {
				hasPending.Store(true)
			}
			return err
		})
	}
	for _, a := range m.apis {
		eg.Go(func() error {
			pending, err := m.mtaEventOnce(egCtx, rt, a)
			if pending {
				hasPending.Store(true)
			}
			return err
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return m.conf.PendingOrInterval(hasPending.Load()), nil
}

func (m *Mimecast) auditOnce(ctx context.Context, rt hosted.Runtime) (hasPending bool,
	err error) {
	api := log.KV("api", AuditApi)
	tag, err := rt.NegotiateTag(m.tag(AuditApi))
	if err != nil {
		return
	}

	cursor, lts, err := m.get(rt, AuditApi, m.start)
	if err != nil {
		rt.Error("error getting storage data", api, log.KVErr(err))
		err = nil
		return
	}

	tr := NewTimeRange(lts, time.Now())
	if cursor != "" {
		rt.Debug("fetching next page of events", api)
	} else {
		rt.Debug("fetching events between", api, log.KV("start", lts), log.KV("end",
			tr.End))
	}

	r, err := m.c.GetRawAuditEvents(ctx, tr, cursor)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		return
	}

	rt.Debug("got events", api, log.KV("count", len(r.Data)))
	for _, d := range r.Data {
		data, lerr := parse[AuditData](bytes.NewReader(d))
		if lerr != nil {
			rt.Error("error parsing audit record", log.KVErr(lerr))
			continue
		}
		ets, lerr := time.Parse(AuditTimeFormat, data.EventTime)
		if lerr != nil {
			rt.Error("error parsing time for event", api, log.KVErr(lerr))
			continue
		}
		e := entry.Entry{
			TS:   entry.FromStandard(ets),
			Data: d,
			Tag:  tag,
		}
		if lerr = rt.Write(e); lerr != nil {
			rt.Error("error writing entry", api, log.KVErr(lerr))
			continue
		}
		rt.Debug("wrote audit entry", api, log.KV("ts", e.TS))
	}

	_ = rt.PutString(m.cursor(AuditApi), r.Meta.Pagination.Next)
	if len(r.Data) == 0 {
		rt.Debug("moving forward in time", api, log.KV("to", tr.End))
		_ = rt.PutTime(m.timestamp(AuditApi), tr.End)
	}
	hasPending = len(r.Data) > 0 && r.Meta.Pagination.Next != ""
	return
}

func (m *Mimecast) mtaEventOnce(ctx context.Context, rt hosted.Runtime, api Api) (hasPending bool, err error) {
	event := SIEMApiEvents[api]
	tag, err := rt.NegotiateTag(m.tag(api))
	if err != nil {
		return
	}

	cursor, lts, err := m.get(rt, api, m.start)
	if err != nil {
		rt.Error("error getting storage data", log.KV("api", api), log.KVErr(err))
		err = nil
		return
	}

	tr := NewTimeRange(lts, time.Now())
	tr.ClampStart(7*24*time.Hour, time.Minute)
	rt.Debug("fetching batch between", log.KV("api", api), log.KV("start", tr.Start),
		log.KV("end", tr.End))

	events, err := m.c.GetSIEMEventBatch(ctx, event, tr, cursor)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			err = nil
		}
		return
	}

	var last time.Time
	rt.Debug("got batches", log.KV("api", api), log.KV("count", len(events.Value)))
	for _, batch := range events.Value {
		last, err = m.handleMtaBatch(ctx, rt, tag, tr, batch, api)
		if err != nil {
			rt.Error("error handling mta batch", log.KV("api", api), log.KVErr(err))
			err = nil
			continue
		}
	}

	if last.IsZero() {
		last = tr.End
	}
	_ = rt.PutString(m.cursor(api), events.NextPage)
	if events.IsCaughtUp {
		rt.Debug("caught up, moving forward in time", log.KV("api", api), log.KV("to",
			last))
		_ = rt.PutTime(m.timestamp(api), last)
	}
	hasPending = !events.IsCaughtUp
	return
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
	rt.Debug("finished processing mta events", log.KV("processed-entries", count),
		log.KV("first-timestamp", first), log.KV("last-timestamp", last), log.KV("api", api))
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
		rt.Debug("no new events to ingest in range", log.KV("start", tr.Start),
			log.KV("end", tr.End), log.KV("api", api))
	} else {
		rt.Debug("finished processing mta events", log.KV("processed-entries", count),
			log.KV("first-timestamp", first), log.KV("last-timestamp", last), log.KV("api", api))
	}
	return last, nil
}

func (m *Mimecast) entries(ctx context.Context, url string) (iter.Seq[[]byte], error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
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
