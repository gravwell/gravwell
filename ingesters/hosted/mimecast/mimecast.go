package mimecast

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/hosted"
)

// Storage keys
const (
	auditCursor    = "audit-cursor"
	auditTimestamp = "audit-timestamp"
)

type Mimecast struct {
	c            *Client
	apis         []Api
	includeAudit bool // if the audit api should be polled
	start        time.Time
}

func NewLegacy(conf *LegacyConfig) *Mimecast {
	c := NewClient(defaultBaseDomain, conf.ClientID, conf.ClientSecret, &http.Client{})
	_, ok := SIEMApiEvents[conf.MimecastAPI]
	var apis = make([]Api, 1)
	if ok {
		apis[0] = conf.MimecastAPI
	}
	audit := false
	if conf.MimecastAPI == AuditApi {
		audit = true
	}
	return &Mimecast{
		c:            c,
		apis:         apis,
		includeAudit: audit,
		start:        conf.StartTime,
	}
}

func New(conf *Config) *Mimecast {
	c := NewClient(conf.Host, conf.Client_Id, conf.Client_Secret, &http.Client{})
	apis := make([]Api, 0)
	audit := false
	for _, a := range conf.Api {
		if a == AuditApi {
			audit = true
			continue
		}
		_, ok := SIEMApiEvents[Api(a)]
		if ok {
			apis = append(apis, Api(a))
		}
	}

	start := time.Now().Add(conf.Lookback * -time.Hour)

	return &Mimecast{
		c:            c,
		apis:         apis,
		includeAudit: audit,
		start:        start,
	}
}

func (m *Mimecast) Run(ctx context.Context, rt hosted.Runtime) error {
	rt.Info("starting mimecast")
	errs := make([]error, 2)
	var wg sync.WaitGroup
	if m.includeAudit {
		wg.Add(1)
		go func() {
			err := m.audit(ctx, rt)
			errs[0] = err
			wg.Done()
		}()
	}
	if len(m.apis) > 0 {
		wg.Add(1)
		go func() {
			err := m.mta(ctx, rt)
			errs[1] = err
			wg.Done()
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (m *Mimecast) audit(ctx context.Context, rt hosted.Runtime) error {
	var cursor *string                   // if cursor is non-nil don't update timestamps
	tag, err := rt.NegotiateTag("audit") // TODO: config
	if err != nil {
		return err
	}
	for !rt.Sleep(time.Second * 5) { // TODO: configurable
		if c, err := rt.GetString(auditCursor); err != nil && !errors.Is(err, hosted.ErrStorageNotFound) {
			rt.Error("error getting audit cursor", log.KVErr(err))
			continue
		} else if c != "" {
			cursor = &c
		}

		lastTime := m.start // audit api only holds logs for a day
		if t, err := rt.GetTime(auditTimestamp); err != nil && !errors.Is(err, hosted.ErrStorageNotFound) {
			return fmt.Errorf("error getting last timestamp: %w", err)
		} else if t.Before(time.Now()) && !t.IsZero() {
			lastTime = t
		}
		r, err := m.c.GetRawAuditEvents(ctx, lastTime, time.Now(), cursor)
		if err != nil {
			rt.Error("request error", log.KV("api", "audit"), log.KVErr(err))
			continue
		}

		for _, d := range r.Data {
			data, err := parse[AuditData](bytes.NewReader(d))
			if err != nil {
				rt.Error("error parsing audit record", log.KVErr(err))
				continue
			}
			ts, err := time.Parse(AuditTimeFormat, data.EventTime)
			if err != nil {
				rt.Error("error parsting time for event", log.KVErr(err))
				continue
			}
			e := entry.Entry{
				TS:   entry.FromStandard(ts),
				Data: d,
				Tag:  tag,
			}
			rt.Write(e)
			// save progress on current cursor?
		}
		// don't advance the cursor until we process the page
		if r.Meta.Pagination.Next != "" {
			rt.PutString(auditCursor, r.Meta.Pagination.Next)
		} else {
			rt.PutString(auditCursor, "")
			rt.PutTime(auditTimestamp, time.Now()) // I'm not sure this is true, may need to get the timestamp off the last record, otherwise we might skip
		}
	}

	// TODO: save state before bailing

	return nil
}

func (m *Mimecast) mta(ctx context.Context, rt hosted.Runtime) error {
	errs := make([]error, len(m.apis))
	var wg sync.WaitGroup
	for i, a := range m.apis {
		wg.Add(1)
		go func() {
			err := m.mtaEvent(ctx, rt, a)
			errs[i] = err
			wg.Done()
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (m *Mimecast) mtaEvent(ctx context.Context, rt hosted.Runtime, api Api) error {
	event, _ := SIEMApiEvents[api]
	tag, err := rt.NegotiateTag(string(api))
	if err != nil {
		return err
	}
	for !rt.Sleep(time.Second * 5) { // TODO: configurable
		var cursor *string
		if c, err := rt.GetString(string(api) + auditCursor); err != nil && !errors.Is(err, hosted.ErrStorageNotFound) {
			rt.Error("error getting cursor", log.KV("api", api), log.KVErr(err))
			continue
		} else if c != "" {
			cursor = &c
		}
		lastTime := m.start // mta events are held for 7 days
		if t, err := rt.GetTime(string(api) + auditTimestamp); err != nil && !errors.Is(err, hosted.ErrStorageNotFound) {
			rt.Error("error getting last timestamp", log.KV("api", api), log.KVErr(err))
		} else if t.Before(time.Now()) && !t.IsZero() {
			lastTime = t
		}

		r, err := m.c.GetSIEMEventBatch(ctx, event, lastTime, time.Now(), cursor)
		if err != nil {
			rt.Error("request error", log.KV("api", api), log.KVErr(err))
			continue
		}

		for _, v := range r.Value {
			err := m.handleMtaEvent(ctx, rt, tag, v)
			if err != nil {
				rt.Error("error handling mta page", log.KV("api", api), log.KVErr(err))
				continue
			}
			// save progress on current cursor?
		}
		if !r.IsCaughtUp {
			rt.PutString(string(api)+auditCursor, r.NextPage)
		} else {
			rt.PutString(string(api)+auditCursor, "")
			rt.PutTime(string(api)+auditTimestamp, time.Now()) // I'm not sure this is true, may need to get the timestamp off the last record, otherwise we might skip
		}
	}

	return nil
}

func (m *Mimecast) handleMtaEvent(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag, event SIEMEvent) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, event.URL, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}

	gzreader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzreader.Close()

	data, err := io.ReadAll(gzreader)
	if err != nil {
		return fmt.Errorf("failed to read gzip body: %w", err)
	}

	entries := strings.Split(string(data), "\n")
	for _, e := range entries {
		if e == "" {
			continue
		}
		data, err := parse[MtaEventData](strings.NewReader(e))
		if err != nil {
			rt.Error("failed to parse mta event", log.KVErr(err), log.KV("url", event.URL))
			continue
		}
		e := entry.Entry{
			TS:   entry.FromStandard(time.Unix(data.Timestamp, 0)),
			Data: []byte(e),
			Tag:  tag,
		}
		rt.Write(e)
	}
	return nil
}
