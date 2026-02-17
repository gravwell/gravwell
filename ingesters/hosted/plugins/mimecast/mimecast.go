// Package mimecast is a plugin used for reading data from Mimecast audit logs.
// It supports both SIEM MTA logs, and general audit logs.
package mimecast

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
	"github.com/gravwell/gravwell/v3/ingesters/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
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
		interval:     time.Duration(conf.Interval) * time.Second,
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
	tag, err := rt.NegotiateTag(m.tag(AuditApi))
	if err != nil {
		return err
	}
	for !rt.Sleep(m.interval) {
		cursor, lts, err := m.get(rt, AuditApi, m.start)
		if err != nil {
			rt.Error("error getting storage data", log.KVErr(err))
			continue
		}

		ts := time.Now()
		if cursor != "" {
			rt.Debug("fetching next page of events", log.KV("api", AuditApi))
		} else {
			rt.Debug("fetching events between", log.KV("api", AuditApi), log.KV("start", lts), log.KV("end", ts))
		}
		r, err := m.c.GetRawAuditEvents(ctx, lts, ts, cursor)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				rt.Error("request error", log.KV("api", AuditApi), log.KVErr(err))
			}
			continue
		}

		last := ts
		rt.Debug("got events", log.KV("api", AuditApi), log.KV("count", len(r.Data)))
		for _, d := range r.Data {
			data, err := parse[AuditData](bytes.NewReader(d))
			if err != nil {
				rt.Error("error parsing audit record", log.KVErr(err))
				continue
			}
			ets, err := time.Parse(AuditTimeFormat, data.EventTime)
			if err != nil {
				rt.Error("error parsing time for event", log.KVErr(err))
				continue
			}
			e := entry.Entry{
				TS:   entry.FromStandard(ets),
				Data: d,
				Tag:  tag,
			}
			err = rt.Write(e)
			if err != nil {
				rt.Error("error writing entry", log.KV("api", "audit"), log.KVErr(err))
				continue
			}
			last = ets
			rt.Debug("wrote audit entry", log.KV("ts", e.TS))
			// save progress on current cursor?
		}

		// don't advance time until we process the entire timespan
		if r.Meta.Pagination.Next != "" {
			rt.Debug("got another page of events", log.KV("api", AuditApi))
			rt.PutString(m.cursor(AuditApi), r.Meta.Pagination.Next)
		} else {
			rt.Debug("no more pages, moving forward in time", log.KV("api", AuditApi), log.KV("to", last))
			rt.PutString(m.cursor(AuditApi), "")
			rt.PutTime(m.timestamp(AuditApi), last)
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

		ts := time.Now()
		if cursor != "" {
			rt.Debug("fetching next page of batch", log.KV("api", api))
		} else {
			rt.Debug("fetching batch between", log.KV("api", api), log.KV("start", lts), log.KV("end", ts))
		}
		r, err := m.c.GetSIEMEventBatch(ctx, event, lts, ts, cursor)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				rt.Error("request error", log.KV("api", api), log.KVErr(err))
			}
			continue
		}
		last := &ts
		rt.Debug("got batches", log.KV("api", api), log.KV("count", len(r.Value)))
		for _, v := range r.Value {
			t, err := m.handleMtaEvent(ctx, rt, tag, v)
			if err != nil {
				rt.Error("error handling mta page", log.KV("api", api), log.KVErr(err))
				continue
			}
			last = t
		}
		if r.IsCaughtUp { // Progress forward in time
			rt.Debug("no more pages, moving forward in time", log.KV("api", api), log.KV("to", *last))
			rt.PutString(m.cursor(api), "")
			rt.PutTime(m.timestamp(api), *last)
		} else {
			rt.Debug("got another page of batch", log.KV("api", api))
			rt.PutString(m.cursor(api), r.NextPage)
		}
	}

	return nil
}

func (m *Mimecast) handleMtaEvent(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag, event SIEMEvent) (*time.Time, error) {
	entries, err := m.entries(ctx, event.URL)
	if err != nil {
		return nil, err
	}
	var first *time.Time
	var last time.Time
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
		if first == nil {
			first = &ts
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
	rt.Debug("finished processing mta events", log.KV("processed-entries", count), log.KV("first-timestamp", first), log.KV("last-timestamp", last))
	return &last, nil
}

func (m *Mimecast) entries(ctx context.Context, url string) (iter.Seq[[]byte], error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The DefaultClient is used here as the event.URL is a presigned URL (generally to an aws S3 bucket).
	// Rate Limits don't apply, and using m.client would pass credentials to AWS,
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed: %s", response.Status)
	}

	gzreader, err := gzip.NewReader(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}

	scanner := bufio.NewScanner(gzreader)

	return func(yield func([]byte) bool) {
		for scanner.Scan() {
			if !yield(scanner.Bytes()) {
				utils.DrainResponse(response)
				gzreader.Close()
				return
			}
		}
	}, nil
}
