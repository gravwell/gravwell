package mimecast

import (
	"context"
	"net/http"
	"time"
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
	_, err := m.c.GetRawAuditEvents(ctx, time.Now().Add(-time.Second*10), time.Now(), nil)
	if err != nil {
		return err
	}

	return nil
}
