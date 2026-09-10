/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package thinkst implements a plugin for ingesting Thinkst Canary incidents
// and audit trail events.
package thinkst

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"golang.org/x/time/rate"
)

// export Name, ID, and Version as strings for compatibility with non-native interfaces
const (
	Name    string = `thinkst`
	ID      string = `thinkst.ingesters.gravwell.io`
	Version string = `1.0.0` // must be canonical version string with only major.minor.point
)

const (
	httpTimeout = 10 * time.Second
	httpBackoff = 10 * time.Second

	// storage keys
	sinceIDKey   = "since-id"  // last incidents_since value used for the incidents API
	cursorKey    = "cursor"    // pagination cursor, shared shape for both APIs
	timestampKey = "timestamp" // last-seen record timestamp, used by the audit API
)

type Thinkst struct {
	conf   *Config
	client *Client
	once   sync.Once
}

// New builds a Thinkst job. conf is assumed to have already passed Verify().
func New(conf *Config) *Thinkst {
	return &Thinkst{conf: conf}
}

// initClient lazily builds the rate-limited HTTP client on first Handle call,
// binding it to the long-lived job context obtained from the runtime.
func (t *Thinkst) initClient(ctx context.Context) {
	t.once.Do(func() {
		rl := rate.NewLimiter(rate.Every(time.Minute/time.Duration(t.conf.Requests_Per_Minute)), t.conf.Requests_Per_Minute)
		retry := utils.NewRetryHttpClient(rl, httpTimeout, httpBackoff, ctx, nil)
		t.client = NewClient(t.conf.Domain, t.conf.Token, retry)
	})
}

// Handle fetches a single page of data from the configured API and returns a
// Continuation telling the runner when to call Handle again: immediately if
// more pages are pending, otherwise after the configured poll interval.
func (t *Thinkst) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	t.initClient(rt.Context())

	tag, err := rt.NegotiateTag(t.conf.Tag_Name)
	if err != nil {
		return nil, fmt.Errorf("failed to negotiate tag: %w", err)
	}

	switch t.conf.Api {
	case IncidentApi:
		return t.handleIncidents(ctx, rt, tag)
	case AuditApi:
		return t.handleAudit(ctx, rt, tag)
	}
	return nil, fmt.Errorf("unsupported api %q", t.conf.Api)
}

func (t *Thinkst) handleIncidents(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag) (*hosted.Continuation, error) {
	sinceID, err := hosted.GetStringOrDefault(rt, sinceIDKey, "")
	if err != nil {
		return nil, fmt.Errorf("get since-id: %w", err)
	}
	cursor, err := hosted.GetStringOrDefault(rt, cursorKey, "")
	if err != nil {
		return nil, fmt.Errorf("get cursor: %w", err)
	}

	resp, err := t.client.GetIncidents(ctx, sinceID, cursor)
	if err != nil {
		return nil, err
	}
	rt.Debug("got incidents page", log.KV("count", len(resp.Incidents)))

	newSinceID := sinceID
	for _, raw := range resp.Incidents {
		var meta incidentMeta
		var ts time.Time
		if err := parseInto(raw, &meta); err != nil {
			rt.Error("failed to parse incident metadata", log.KVErr(err))
		} else {
			if parsedTS, perr := time.Parse(TimeFormat, meta.UpdatedStd); perr == nil {
				ts = parsedTS
			}
			if id, ierr := strconv.Atoi(newSinceID); ierr == nil {
				if meta.UpdatedID > id {
					newSinceID = strconv.Itoa(meta.UpdatedID)
				}
			} else {
				newSinceID = strconv.Itoa(meta.UpdatedID)
			}
		}
		if ts.IsZero() {
			ts = time.Now()
		}

		if err := rt.Write(entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: raw,
		}); err != nil {
			rt.Error("failed to write incident entry", log.KVErr(err))
		}
	}

	if err := rt.PutString(sinceIDKey, newSinceID); err != nil {
		rt.Error("failed to store since-id", log.KVErr(err))
	}

	// The API only returns a usable cursor value when next_link is present;
	// this mirrors a quirk of the upstream API where "next" can be populated
	// even when there is no further page to fetch.
	next := ""
	if resp.Cursor.NextLink != nil {
		next = fmt.Sprintf("%v", resp.Cursor.Next)
	}
	if err := rt.PutString(cursorKey, next); err != nil {
		rt.Error("failed to store cursor", log.KVErr(err))
	}

	pending := next != "" && len(resp.Incidents) > 0
	return t.conf.PendingOrInterval(pending), nil
}

func (t *Thinkst) handleAudit(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag) (*hosted.Continuation, error) {
	cursor, err := hosted.GetStringOrDefault(rt, cursorKey, "")
	if err != nil {
		return nil, fmt.Errorf("get cursor: %w", err)
	}
	lastTS, err := hosted.GetTimeOrDefault(rt, timestampKey, time.Now().Add(-t.conf.LookbackDuration()))
	if err != nil {
		return nil, fmt.Errorf("get timestamp: %w", err)
	}

	resp, err := t.client.GetAuditTrail(ctx, cursor)
	if err != nil {
		return nil, err
	}
	rt.Debug("got audit trail page", log.KV("count", len(resp.AuditTrail)))

	latest := lastTS
	for _, raw := range resp.AuditTrail {
		var meta auditMeta
		if err := parseInto(raw, &meta); err != nil {
			rt.Error("failed to parse audit trail metadata", log.KVErr(err))
			continue
		}
		ts, perr := time.Parse(TimeFormat, meta.Timestamp)
		if perr != nil {
			ts = time.Now()
		}
		// The audit trail API has no server-side time filter, so records
		// already seen on a prior page are skipped client-side.
		if !ts.After(lastTS) {
			continue
		}

		if err := rt.Write(entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: raw,
		}); err != nil {
			rt.Error("failed to write audit entry", log.KVErr(err))
			continue
		}
		if ts.After(latest) {
			latest = ts
		}
	}

	next := ""
	if resp.Cursor.Next != nil {
		next = fmt.Sprintf("%v", resp.Cursor.Next)
	}
	if err := rt.PutString(cursorKey, next); err != nil {
		rt.Error("failed to store cursor", log.KVErr(err))
	}
	if err := rt.PutTime(timestampKey, latest); err != nil {
		rt.Error("failed to store timestamp", log.KVErr(err))
	}

	pending := next != "" && len(resp.AuditTrail) > 0
	return t.conf.PendingOrInterval(pending), nil
}
