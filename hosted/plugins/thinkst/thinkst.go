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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
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

	// storage keys, namespaced per Api so an instance configured to poll both
	// incidents and audit trail doesn't have one API's pagination cursor
	// clobber the other's.
	incidentSinceIDKey = "incident-since-id" // last incidents_since value used for the incidents API
	incidentCursorKey  = "incident-cursor"   // incidents API pagination cursor

	auditCursorKey          = "audit-cursor"           // audit trail API pagination cursor
	auditTimestampKey       = "audit-timestamp"        // last-seen audit record timestamp
	auditTimestampHashesKey = "audit-timestamp-hashes" // content hashes of records already ingested at auditTimestampKey
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

// Handle fetches a single page of data from every configured Api and returns
// a Continuation telling the runner when to call Handle again: immediately
// if any Api still has more pages pending, otherwise after the configured
// poll interval.
func (t *Thinkst) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	t.initClient(rt.Context())

	if len(t.conf.Api) == 0 {
		return nil, fmt.Errorf("unsupported api %q", t.conf.Api)
	}

	var pending bool
	for _, api := range t.conf.Api {
		tag, err := rt.NegotiateTag(api.Tag(t.conf.Tag_Name, t.conf.Tag_Prefix))
		if err != nil {
			return nil, fmt.Errorf("failed to negotiate tag: %w", err)
		}

		var p bool
		switch api {
		case IncidentApi:
			p, err = t.handleIncidents(ctx, rt, tag)
		case AuditApi:
			p, err = t.handleAudit(ctx, rt, tag)
		default:
			err = fmt.Errorf("unsupported api %q", api)
		}
		if err != nil {
			return nil, err
		}
		pending = pending || p
	}
	return t.conf.PendingOrInterval(pending), nil
}

// handleIncidents fetches a single page of incidents and reports whether
// another page is immediately pending.
func (t *Thinkst) handleIncidents(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag) (bool, error) {
	sinceID, err := hosted.GetStringOrDefault(rt, incidentSinceIDKey, "")
	if err != nil {
		return false, fmt.Errorf("get since-id: %w", err)
	}
	cursor, err := hosted.GetStringOrDefault(rt, incidentCursorKey, "")
	if err != nil {
		return false, fmt.Errorf("get cursor: %w", err)
	}

	resp, err := t.client.GetIncidents(ctx, sinceID, cursor)
	if err != nil {
		return false, err
	}
	rt.Debug("got incidents page", log.KV("count", len(resp.Incidents)))

	newSinceID := sinceID
	for _, raw := range resp.Incidents {
		var meta incidentMeta
		if err := parseInto(raw, &meta); err != nil {
			// The record's true ID is unknown, so since-id can never be
			// advanced past it safely; ingesting it anyway would mean
			// re-fetching and re-writing it on every poll forever, so it's
			// dropped instead (mirroring handleAudit's parse-failure
			// handling below).
			rt.Error("failed to parse incident metadata, skipping record", log.KVErr(err))
			continue
		}

		var ts time.Time
		if parsedTS, perr := time.Parse(TimeFormat, meta.UpdatedStd); perr == nil {
			ts = parsedTS
		}
		if ts.IsZero() {
			ts = time.Now()
		}

		if id, ierr := strconv.Atoi(newSinceID); ierr == nil {
			if meta.UpdatedID > id {
				newSinceID = strconv.Itoa(meta.UpdatedID)
			}
		} else {
			newSinceID = strconv.Itoa(meta.UpdatedID)
		}

		if err := rt.Write(entry.Entry{
			TS:   entry.FromStandard(ts),
			Tag:  tag,
			Data: raw,
		}); err != nil {
			rt.Error("failed to write incident entry", log.KVErr(err))
		}
	}

	if err := rt.PutString(incidentSinceIDKey, newSinceID); err != nil {
		rt.Error("failed to store since-id", log.KVErr(err))
	}

	// The API only returns a usable cursor value when next_link is present;
	// this mirrors a quirk of the upstream API where "next" can be populated
	// even when there is no further page to fetch.
	next := ""
	if resp.Cursor.NextLink != nil {
		next = stringify(resp.Cursor.Next)
	}
	if err := rt.PutString(incidentCursorKey, next); err != nil {
		rt.Error("failed to store cursor", log.KVErr(err))
	}

	return next != "" && len(resp.Incidents) > 0, nil
}

// handleAudit fetches a single page of audit trail records and reports
// whether another page is immediately pending.
func (t *Thinkst) handleAudit(ctx context.Context, rt hosted.Runtime, tag entry.EntryTag) (bool, error) {
	cursor, err := hosted.GetStringOrDefault(rt, auditCursorKey, "")
	if err != nil {
		return false, fmt.Errorf("get cursor: %w", err)
	}
	lastTS, err := hosted.GetTimeOrDefault(rt, auditTimestampKey, time.Now().Add(-t.conf.LookbackDuration()))
	if err != nil {
		return false, fmt.Errorf("get timestamp: %w", err)
	}
	seenRaw, err := hosted.GetStringOrDefault(rt, auditTimestampHashesKey, "")
	if err != nil {
		return false, fmt.Errorf("get timestamp-hashes: %w", err)
	}
	seenAtWatermark := splitHashes(seenRaw)

	resp, err := t.client.GetAuditTrail(ctx, cursor)
	if err != nil {
		return false, err
	}
	rt.Debug("got audit trail page", log.KV("count", len(resp.AuditTrail)))

	// latestHashes tracks the content hashes of every record ingested at
	// exactly `latest`. It starts as a copy of the hashes already known at
	// the current watermark (lastTS) and is reset whenever the watermark
	// actually advances, so a poll that never sees a newer record doesn't
	// forget what was already deduped at this second.
	latest := lastTS
	latestHashes := maps.Clone(seenAtWatermark)

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

		// The audit trail API has no server-side time filter and its
		// timestamps only carry second resolution, so records already seen
		// on a prior page/poll are skipped client-side: anything strictly
		// before the watermark was already handled, and anything exactly at
		// the watermark is deduped by content hash so that distinct records
		// legitimately sharing that same second aren't dropped.
		h := recordHash(raw)
		if ts.Before(lastTS) || (ts.Equal(lastTS) && seenAtWatermark[h]) {
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

		switch {
		case ts.After(latest):
			latest = ts
			latestHashes = map[string]bool{h: true}
		case ts.Equal(latest):
			latestHashes[h] = true
		}
	}

	next := ""
	if resp.Cursor.Next != nil {
		next = stringify(resp.Cursor.Next)
	}
	if err := rt.PutString(auditCursorKey, next); err != nil {
		rt.Error("failed to store cursor", log.KVErr(err))
	}
	if err := rt.PutTime(auditTimestampKey, latest); err != nil {
		rt.Error("failed to store timestamp", log.KVErr(err))
	}
	if err := rt.PutString(auditTimestampHashesKey, joinHashes(latestHashes)); err != nil {
		rt.Error("failed to store timestamp-hashes", log.KVErr(err))
	}

	return next != "" && len(resp.AuditTrail) > 0, nil
}

// stringify renders a JSON-decoded `any` cursor value as a string, without
// producing the literal "<nil>" when the value is absent.
func stringify(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// recordHash returns a short, stable content hash for an audit trail record,
// used to dedup records that share a watermark timestamp.
func recordHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func splitHashes(s string) map[string]bool {
	out := make(map[string]bool)
	if s == "" {
		return out
	}
	for _, h := range strings.Split(s, ",") {
		out[h] = true
	}
	return out
}

func joinHashes(m map[string]bool) string {
	hashes := make([]string, 0, len(m))
	for h := range m {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	return strings.Join(hashes, ",")
}
