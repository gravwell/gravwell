package mimecast

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type Mimecast struct {
	c *Client
}

func New(host, id, secret string) *Mimecast {
	c := NewClient(host, id, secret, &http.Client{Timeout: 15})
	return &Mimecast{
		c: c,
	}
}

func (m *Mimecast) Run(ctx context.Context) error {
	for {
		select {
		case <-time.After(time.Second * 10):
		case <-ctx.Done():
			return nil
		}

		_ = m.audit(ctx)

	}
}

func (m *Mimecast) audit(ctx context.Context) error {
	var cursor *string // if cursor is non-nil don't update timestamps
	var lastTime time.Time

routine:
	for { // TODO: quitable sleep
		select {
		case <-time.After(time.Second * 10): // TODO: configurable
		case <-ctx.Done():
			break routine
		}

		if cursor == nil {
			lastTime = time.Now().Add(-time.Second * 10) // TODO: this probably isn't it
		}
		r, err := m.c.GetRawAuditEvents(ctx, lastTime, time.Now(), cursor)
		if err != nil {
			return err
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
			_ = entry.Entry{
				SRC:  net.ParseIP("127.0.0.1"),
				TS:   entry.FromStandard(ts),
				Data: d,
				Tag:  1234, // TODO: real tag
			}
			// TODO: write entry
			// save progress on currenet cursor?
		}
		// save cursor
	}

	// TODO: save state before bailing

	return nil
}
