/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package wiz is a plugin that pulls VulnerabilityFinding, Issues, and Audit
// events from the Wiz cloud security GraphQL API. It authenticates via OAuth
// client credentials, pages each event type's connection, and ingests every
// record as JSON while tracking a per-type high water mark so it only forwards
// data newer than what it has already seen.
package wiz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"golang.org/x/time/rate"
)

// export Name, Version, and ID as strings so they are compatible with WASM and any non-native interfaces
const (
	Name    string = `wiz`
	ID      string = `wiz.ingesters.gravwell.io`
	Version string = `1.0.0` // must be canonical version string with only major.minor.point

	httpTimeout = 30 * time.Second
	httpBackoff = 10 * time.Second

	// typeEVName is the enumerated value attached to each entry to record which
	// Wiz query the record came from (e.g. "issues", "auditLogEntries").
	typeEVName = "type"
)

// timestampFields are the node fields we probe, in priority order, to determine
// how recent a record is. Wiz uses several conventions across these event types.
var timestampFields = []string{
	"updatedAt",
	"lastDetectedAt",
	"lastSeenAt",
	"timestamp",
	"detectedAt",
	"createdAt",
	"firstDetectedAt",
	"firstSeenAt",
}

type Wiz struct {
	conf *Config

	mu      sync.Mutex
	c       *Client
	limiter *rate.Limiter
	sources []source
	tags    map[string]entry.EntryTag
	ignored map[string]bool // sources we've been denied access to or that error deterministically
}

func New(conf *Config) *Wiz {
	sources := builtinSources()
	for i := range sources {
		// apply any operator query override for this source.
		if doc, ok := conf.queries[sources[i].name]; ok {
			sources[i].query = doc
		}
		vars := parseQueryVars(sources[i].query)
		sources[i].hasFirst = vars["first"]
		sources[i].hasAfter = vars["after"]
		sources[i].hasSince = vars["since"]
	}
	return &Wiz{
		conf:    conf,
		sources: sources,
		tags:    make(map[string]entry.EntryTag),
		ignored: make(map[string]bool),
	}
}

func (w *Wiz) initClient(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c != nil {
		return
	}
	w.limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(w.conf.Requests_Per_Minute)),
		w.conf.Requests_Per_Minute)
	retry := utils.NewRetryHttpClient(w.limiter, httpTimeout, httpBackoff, ctx, nil)
	w.c = NewClient(w.conf.Endpoint, w.conf.Auth_URL, w.conf.Audience,
		w.conf.Client_Id, w.conf.Client_Secret, retry)
}

func (w *Wiz) ignore(sourceName string) {
	w.mu.Lock()
	w.ignored[sourceName] = true
	w.mu.Unlock()
}

func (w *Wiz) isIgnored(sourceName string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ignored[sourceName]
}

func (w *Wiz) tag(rt hosted.Runtime, sourceName string) (entry.EntryTag, error) {
	name := w.conf.tagFor(sourceName)
	w.mu.Lock()
	if t, ok := w.tags[name]; ok {
		w.mu.Unlock()
		return t, nil
	}
	w.mu.Unlock()

	t, err := rt.NegotiateTag(name)
	if err != nil {
		return 0, fmt.Errorf("failed to negotiate tag %q: %w", name, err)
	}
	w.mu.Lock()
	w.tags[name] = t
	w.mu.Unlock()
	return t, nil
}

func (w *Wiz) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	w.initClient(ctx)

	pending := false
	for _, s := range w.sources {
		more, err := w.scanSource(ctx, rt, s)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			rt.Error("failed to scan source", log.KV("source", s.name), log.KVErr(err))
			continue
		}
		if more {
			pending = true
		}
	}

	// If any source still has pages left in its current scan, come back
	// immediately to keep draining; otherwise wait for the poll interval.
	return w.conf.PendingOrInterval(pending), nil
}

// storage key helpers, namespaced per source.
func (w *Wiz) cursorKey(s string) string  { return s + "-cursor" }
func (w *Wiz) tsKey(s string) string      { return s + "-timestamp" }
func (w *Wiz) pendingKey(s string) string { return s + "-pending" }
func (w *Wiz) initKey(s string) string    { return s + "-initialized" }

// connection is the generic shape of a Relay style connection response.
type connection struct {
	Nodes []json.RawMessage `json:"nodes"`
	Edges []struct {
		Node json.RawMessage `json:"node"`
	} `json:"edges"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (c connection) nodes() []json.RawMessage {
	if len(c.Nodes) > 0 {
		return c.Nodes
	}
	out := make([]json.RawMessage, 0, len(c.Edges))
	for _, e := range c.Edges {
		if len(e.Node) > 0 {
			out = append(out, e.Node)
		}
	}
	return out
}

// scanSource drains up to Max_Pages_Per_Type pages of a single event type. It
// returns more=true when the source still has pages left to fetch next call.
func (w *Wiz) scanSource(ctx context.Context, rt hosted.Runtime, s source) (more bool, err error) {
	if w.isIgnored(s.name) {
		return false, nil
	}

	tag, err := w.tag(rt, s.name)
	if err != nil {
		return false, err
	}

	inited, _ := rt.GetInt64(w.initKey(s.name))
	firstScan := inited != 1

	hwm, err := hosted.GetTimeOrDefault(rt, w.tsKey(s.name), time.Now().Add(-w.conf.LookbackDuration()))
	if err != nil {
		return false, fmt.Errorf("get high water mark: %w", err)
	}

	cursor, err := hosted.GetStringOrDefault(rt, w.cursorKey(s.name), "")
	if err != nil {
		return false, fmt.Errorf("get cursor: %w", err)
	}

	// candidate high water mark tracked across the (possibly multi-cycle) scan.
	pending := hwm
	if cursor == "" {
		pending = hwm
		_ = rt.PutTime(w.pendingKey(s.name), pending)
	} else if pending, err = hosted.GetTimeOrDefault(rt, w.pendingKey(s.name), hwm); err != nil {
		return false, fmt.Errorf("get pending timestamp: %w", err)
	}

	api := log.KV("source", s.name)

	for page := 0; page < w.conf.Max_Pages_Per_Type; page++ {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		vars := map[string]any{}
		if s.hasFirst {
			vars["first"] = w.conf.Page_Size
		}
		if s.hasAfter {
			if cursor == "" {
				vars["after"] = nil
			} else {
				vars["after"] = cursor
			}
		}
		// A query may declare $since for server-side time filtering; feed it the
		// current high water mark so the API only returns newer records.
		if s.hasSince {
			vars["since"] = hwm.Format(time.RFC3339)
		}

		var data map[string]connection
		if err = w.c.Query(ctx, s.query, vars, &data); err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				return false, err
			case errors.Is(err, ErrAccessDenied):
				rt.Warn("access denied, ignoring source in future scans", api)
				w.ignore(s.name)
				return false, nil
			case errors.Is(err, ErrInternal):
				// transient upstream failure; log cleanly and retry next cycle.
				rt.Error("internal error from wiz api", api)
				return false, nil
			default:
				// A deterministic query/resolver error will fail identically every
				// cycle, so quarantine the source instead of spamming. The query is
				// logged so it can be reproduced/corrected via Query-Override.
				rt.Error("query error, ignoring source in future scans", api,
					log.KVErr(err), log.KV("query", s.query))
				w.ignore(s.name)
				return false, nil
			}
		}

		conn := data[s.field]
		nodes := conn.nodes()
		rt.Debug("fetched page", api, log.KV("nodes", len(nodes)), log.KV("page", page))

		for _, node := range nodes {
			cleaned, obj := cleanNode(node)
			ts := timestampFromObj(obj, s.tsField)
			if !(firstScan || ts.After(hwm)) {
				continue
			}
			ets := ts
			if ets.IsZero() {
				ets = time.Now()
			} else if ets.After(pending) {
				pending = ets
			}
			e := entry.Entry{
				TS:   entry.FromStandard(ets),
				Tag:  tag,
				Data: cleaned,
			}
			// Record which Wiz query this record came from as a "type" enumerated
			// value. This is a separate namespace from the JSON body, so it does
			// not conflict with any "type" field the record itself carries.
			if eerr := e.AddEnumeratedValueEx(typeEVName, s.field); eerr != nil {
				rt.Error("failed to attach type EV", api, log.KVErr(eerr))
			}
			if werr := rt.Write(e); werr != nil {
				rt.Error("failed to write entry", api, log.KVErr(werr))
				continue
			}
		}

		// Persist progress after every page so a restart resumes mid-scan.
		cursor = conn.PageInfo.EndCursor
		_ = rt.PutString(w.cursorKey(s.name), cursor)
		_ = rt.PutTime(w.pendingKey(s.name), pending)

		if !s.hasAfter || !conn.PageInfo.HasNextPage || cursor == "" {
			// Scan complete: commit the new high water mark and reset so the next
			// cycle starts a fresh incremental scan.
			_ = rt.PutTime(w.tsKey(s.name), pending)
			_ = rt.PutString(w.cursorKey(s.name), "")
			_ = rt.PutInt64(w.initKey(s.name), 1)
			rt.Debug("scan complete", api, log.KV("high-water-mark", pending))
			return false, nil
		}
	}

	// Hit the page budget for this cycle; there is more to fetch next time.
	return true, nil
}

// cleanNode decodes a node, recursively strips null-valued fields (which are
// just noise, e.g. "tags": null) along with any objects/arrays left empty
// afterward, and re-encodes it. It returns the cleaned JSON along with the
// top-level object for timestamp and query-field probing. When the node is not
// decodable it is returned unchanged with a nil object.
func cleanNode(node json.RawMessage) (json.RawMessage, map[string]any) {
	// UseNumber preserves numeric literals exactly so large integer ids are not
	// mangled by a round-trip through float64.
	dec := json.NewDecoder(bytes.NewReader(node))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return node, nil
	}

	cleaned, _ := stripNulls(v)
	b, err := json.Marshal(cleaned)
	if err != nil {
		return node, nil
	}
	obj, _ := cleaned.(map[string]any)
	return b, obj
}

// stripNulls recursively removes null object keys and null array elements, and
// prunes objects/arrays that become empty as a result (bottom-up). The returned
// bool reports whether the value should be dropped by the caller: true for
// null, an empty object, or an empty array.
func stripNulls(v any) (any, bool) {
	switch val := v.(type) {
	case nil:
		return nil, true
	case map[string]any:
		for k, child := range val {
			if cleaned, drop := stripNulls(child); drop {
				delete(val, k)
			} else {
				val[k] = cleaned
			}
		}
		return val, len(val) == 0
	case []any:
		out := val[:0]
		for _, child := range val {
			if cleaned, drop := stripNulls(child); !drop {
				out = append(out, cleaned)
			}
		}
		return out, len(out) == 0
	default:
		return v, false
	}
}

// timestampFromObj resolves a record's timestamp. It tries the source's
// designated cursor field first (which matches the server-side $since filter),
// then falls back to probing common timestamp fields. Returns the zero time
// when none is found.
func timestampFromObj(obj map[string]any, preferred string) time.Time {
	if preferred != "" {
		if ts, ok := parseObjTime(obj, preferred); ok {
			return ts
		}
	}
	for _, key := range timestampFields {
		if ts, ok := parseObjTime(obj, key); ok {
			return ts
		}
	}
	return time.Time{}
}

// parseObjTime parses obj[key] as an RFC3339 timestamp. RFC3339Nano parses
// values with or without fractional seconds.
func parseObjTime(obj map[string]any, key string) (time.Time, bool) {
	s, ok := obj[key].(string)
	if !ok {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || ts.IsZero() {
		return time.Time{}, false
	}
	return ts, true
}

// extractTimestamp probes a node's JSON for a recognizable timestamp field.
// Returns the zero time when none is found.
func extractTimestamp(node json.RawMessage) time.Time {
	_, obj := cleanNode(node)
	return timestampFromObj(obj, "")
}
