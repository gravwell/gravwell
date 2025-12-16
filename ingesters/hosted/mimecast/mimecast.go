package mimecast

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
	events       []EventType
	includeAudit bool // if the audit api should be polled
}

func NewLegacy(conf *LegacyConfig) *Mimecast {
	c := NewClient("", conf.ClientID, conf.ClientSecret, &http.Client{Timeout: 15})
	event, ok := SIEMApiEvents[conf.MimecastAPI]
	var events = make([]EventType, 0)
	if ok {
		events = append(events, event)
	}
	audit := false
	if conf.MimecastAPI == AuditApi {
		audit = true
	}
	return &Mimecast{
		c:            c,
		events:       events,
		includeAudit: audit,
	}
}

func New(conf *Config) *Mimecast {
	c := NewClient(conf.Host, conf.Client_Id, conf.Client_Secret, &http.Client{Timeout: 15})
	return &Mimecast{
		c: c,
	}
}

func (m *Mimecast) Run(ctx context.Context, rt hosted.Runtime) error {
	rt.Info("starting audit")
	return m.audit(ctx, rt)
}

func (m *Mimecast) audit(ctx context.Context, rt hosted.Runtime) error {
	var cursor *string                          // if cursor is non-nil don't update timestamps
	lastTime := time.Now().Add(-24 * time.Hour) // audit api only holds logs for a day
	if t, err := rt.GetTime(auditTimestamp); err != nil {
		return fmt.Errorf("error getting last timestamp: %w", err)
	} else if t.Before(time.Now()) {
		lastTime = t
	}

	tag, err := rt.NegotiateTag("audit") // TODO: config
	if err != nil {
		return err
	}
	for rt.Sleep(time.Second * 1) { // TODO: configurable
		if c, err := rt.GetString(auditCursor); err != nil {
			continue
		} else if c != "" {
			cursor = &c
		}

		r, err := m.c.GetRawAuditEvents(ctx, lastTime, time.Now(), cursor)
		if err != nil {
			rt.Error("request error", log.KVErr(err))
			continue
		}

		if r.Meta.Pagination.Next != "" {
			cursor = &r.Meta.Pagination.Next
		} else {
			cursor = nil
		}
		for _, d := range r.Data {
			data, err := parse[AuditData](io.NopCloser(bytes.NewReader(d)))
			if err != nil {
				continue // TODO: what do
			}
			ts, err := time.Parse(AuditTimeFormat, data.EventTime)
			if err != nil {
				continue // TODO: same ^
			}
			e := entry.Entry{
				SRC:  net.ParseIP("127.0.0.1"),
				TS:   entry.FromStandard(ts),
				Data: d,
				Tag:  tag,
			}
			rt.Write(e)
			// save progress on current cursor?
		}

		if cursor != nil {
			rt.PutString(auditCursor, *cursor)
		} else {
			rt.PutString(auditCursor, "")
			rt.PutTime(auditTimestamp, time.Now()) // I'm not sure this is true, may need to get the timestamp off the last record, otherwise we might skip
		}
	}

	// TODO: save state before bailing

	return nil
}

func (m *Mimecast) mta(ctx context.Context, rt hosted.Runtime) error {
	for rt.Sleep(time.Second) { // TODO: config
		for _, e := range m.events {
			m.c.GetSIEMEventBatch(ctx, e, time.Now(), time.Now(), nil)
		}
	}
	return nil
}

func (m *Mimecast) mtaEvent(ctx context.Context, rt hosted.Runtime, event EventType) error {
	var cursor *string                          // if cursor is non-nil don't update timestamps
	lastTime := time.Now().Add(-7 * 24 * time.Hour) // mta events are held for 7 days
	if t, err := rt.GetTime(string(event) + auditTimestamp); err != nil {
		return fmt.Errorf("error getting last timestamp: %w", err)
	} else if t.Before(time.Now()) {
		lastTime = t
	}

	tag, err := rt.NegotiateTag("audit") // TODO: config
	if err != nil {
		return err
	}
	for rt.Sleep(time.Second * 1) { // TODO: configurable
		if c, err := rt.GetString(string(event)+auditCursor); err != nil {
			continue
		} else if c != "" {
			cursor = &c
		}

		r, err := m.c.GetSIEMEventBatch(ctx, lastTime, time.Now(), cursor)
		if err != nil {
			rt.Error("request error", log.KVErr(err))
			continue
		}

		if r.NextPage != "" {
			cursor = &r.NextPage
		} else {
			cursor = nil
		}
		for _, v := range r.Value {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.URL, nil)
			if err != nil {
				continue //TODO: what do
			}
			response, err := m.c.Do(request)
			if err != nil {
				continue // TODO: ???
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			data, err := parse[MtaEventData](io.NopCloser(bytes.NewReader(body))
			if err != nil {
				continue // TODO: what do
			}
			e := entry.Entry{
				SRC:  net.ParseIP("127.0.0.1"),
				TS:   entry.FromStandard(time.Unix(data.Timestamp, 0)),
				Data: body,
				Tag:  tag,
			}
			rt.Write(e)
			// save progress on current cursor?
		}

		if cursor != nil {
			rt.PutString(string(event)+auditCursor, *cursor)
		} else {
			rt.PutString(string(event)+auditCursor, "")
			rt.PutTime(string(event)+auditTimestamp, time.Now()) // I'm not sure this is true, may need to get the timestamp off the last record, otherwise we might skip
		}
	}

	// TODO: save state before bailing

	return nil
}
