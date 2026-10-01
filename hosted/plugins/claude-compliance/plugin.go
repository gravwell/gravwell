package claudecompliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Plugin struct {
	conf               *Config
	http               *http.Client
	now                func() time.Time
	wait               func(context.Context, time.Duration) error
	limiter            *rate.Limiter
	onRecord           func(Dataset, []byte) error
	flushRecords       func() error
	syncIngest         func(context.Context, time.Duration) error
	maxRecords         int
	maxManifestEntries int
}

// New binds the muxer's existing synchronization capability at build time.
// Runtime wrappers need not expose anything beyond hosted.Runtime.
func New(c *Config, tn hosted.TagNegotiator) (*Plugin, error) {
	s, ok := tn.(interface {
		SyncContext(context.Context, time.Duration) error
	})
	if !ok {
		return nil, errors.New("Compliance requires an ingest muxer with SyncContext")
	}
	return &Plugin{conf: c, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now, wait: sleep, limiter: sharedLimiter(c.Scope_Identity, c.Requests_Per_Minute), syncIngest: s.SyncContext}, nil
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type state struct {
	Since           time.Time  `json:"since"`
	Manifest        manifest   `json:"manifest"`
	Traversal       *traversal `json:"traversal,omitempty"`
	Retired         string     `json:"retired_revision,omitempty"`
	HistoryComplete bool       `json:"history_complete,omitempty"`
}

type traversal struct {
	Since       time.Time `json:"since"`
	Until       time.Time `json:"until"`
	Cursor      string    `json:"cursor"`
	Manifest    manifest  `json:"manifest"`
	FullHistory bool      `json:"full_history,omitempty"`
	// Started is when this page walk issued its first request; a walk older
	// than maxTraversalAge restarts rather than reusing an expired cursor.
	Started time.Time `json:"started,omitempty"`
}

// manifestEntry pairs a record's content digest with a hint of how recently
// the vendor itself reports the record changing. Seen is Unix seconds taken
// from the record's own updated_at/created_at/timestamp field (see
// sourceTime); it is used only to choose which identity to forget first once
// the manifest is full, never to decide whether a record changed.
type manifestEntry struct {
	Digest string `json:"d"`
	Seen   int64  `json:"s,omitempty"`
}

// manifest bounds the retained identity->digest dedup cache for a dataset.
// A full, unwindowed parent scan (organizations, groups, chats/projects/
// local-sessions with Follow-Children enabled, and similar) re-lists every
// record the vendor still reports on every cycle, including long-settled or
// deleted-but-retained compliance records, so this cache would otherwise
// grow without bound. Capping it can only cause an already-unchanged record
// to be rewritten once more after its identity is forgotten; it can never
// cause a changed record to be silently treated as unchanged, because
// eviction only ever runs on insertion of a *new* identity, never on a
// lookup of an existing one.
type manifest map[string]manifestEntry

// defaultMaxManifestEntries bounds retained identities to the same order of
// magnitude as the existing per-cycle record cap (see Plugin.maxRecords),
// keeping the marshaled checkpoint on the order of a few MiB even for the
// largest observed tenants, and well under the 32 MiB bound already enforced
// for the child-work list.
const defaultMaxManifestEntries = 100000

// digestOf reports the retained digest for key, or "" if key is not tracked.
func (m manifest) digestOf(key string) string { return m[key].Digest }

// put records key's digest and vendor-reported seen time, evicting the
// identity the vendor reports as least recently updated when the manifest is
// already at limit. Eviction never removes the identity being inserted.
func (m manifest) put(key, digest string, seen time.Time, limit int) {
	if limit <= 0 {
		limit = defaultMaxManifestEntries
	}
	if _, exists := m[key]; !exists {
		for len(m) >= limit {
			m.evictOldest()
		}
	}
	m[key] = manifestEntry{Digest: digest, Seen: seen.Unix()}
}

// evictOldest removes the identity with the oldest recorded Seen time,
// breaking ties on the identity key so eviction is deterministic.
func (m manifest) evictOldest() {
	victim, victimSeen := "", int64(0)
	for k, e := range m {
		if victim == "" || e.Seen < victimSeen || (e.Seen == victimSeen && k < victim) {
			victim, victimSeen = k, e.Seen
		}
	}
	if victim != "" {
		delete(m, victim)
	}
}

// UnmarshalJSON accepts both the current {"key":{"d":"...","s":...}} shape
// and the plain {"key":"digest"} shape written before this bound existed, so
// checkpoints persisted by earlier builds keep loading.
func (m *manifest) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if e := json.Unmarshal(b, &raw); e != nil {
		return e
	}
	out := make(manifest, len(raw))
	for k, v := range raw {
		var legacy string
		if e := json.Unmarshal(v, &legacy); e == nil {
			out[k] = manifestEntry{Digest: legacy}
			continue
		}
		var entry manifestEntry
		if e := json.Unmarshal(v, &entry); e != nil {
			return e
		}
		out[k] = entry
	}
	*m = out
	return nil
}

// statusError reports a non-200 Compliance response by status code only;
// the response body is never surfaced because it can echo request data.
type statusError struct{ code int }

func (e *statusError) Error() string {
	return fmt.Sprintf("Compliance HTTP %d; state retained", e.code)
}

func httpStatus(e error) int {
	var se *statusError
	if errors.As(e, &se) {
		return se.code
	}
	return 0
}

const (
	// indexingLag keeps a window's upper bound behind the documented Activity
	// Feed queryability delay so a late-indexed record is never excluded by a
	// window that has already advanced past it, whatever Overlap-Seconds is.
	indexingLag = time.Minute
	// maxTraversalAge restarts a stored page walk before the vendor's
	// 24-hour cursor lifetime (local-session messages reject older cursors
	// with a permanent 400; remote tokens must not be stored long-term).
	maxTraversalAge = 23 * time.Hour
)

// fullParentScan reports whether a root inventory must be listed without its
// time filter so that unchanged parents still requeue child work. Chats are
// excluded: the vendor documents that order_by=updated_at returns a chat
// again whenever it receives a new message, moves project, or is deleted.
func fullParentScan(c *Config) bool {
	d := c.dataset
	return c.Follow_Children != "disabled" && (d.Name == "projects" || d.Name == "local-sessions")
}

func (p *Plugin) handleOne(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if p.syncIngest == nil {
		return nil, errors.New("Compliance ingest synchronization is not configured")
	}
	c := p.conf
	token, e := credential(c.Credential_File)
	if e != nil {
		return nil, e
	}
	st := state{Manifest: manifest{}}
	b, e := rt.Get(c.key())
	if e == nil {
		if e = json.Unmarshal(b, &st); e != nil {
			return nil, errors.New("invalid Compliance state")
		}
	} else if !errors.Is(e, storage.ErrStorageNotFound) {
		return nil, e
	}
	if st.Manifest == nil {
		st.Manifest = manifest{}
	}
	now := p.now().UTC()
	d := c.dataset
	since := st.Since
	if since.IsZero() {
		if c.Start_Time != "" {
			since, _ = time.Parse(time.RFC3339, c.Start_Time)
		} else {
			since = now.Add(-time.Duration(c.Lookback) * time.Hour)
		}
	}
	fullParents := fullParentScan(c)
	scan := traversal{
		Since:       since,
		Until:       now,
		Manifest:    manifest{},
		FullHistory: c.discovered && d.Name == "chat-messages" && !st.HistoryComplete,
		Started:     now,
	}
	windowed := func(t traversal) bool { return d.Window != "" && !fullParents && !t.FullHistory }
	if windowed(scan) {
		scan.Until = now.Add(-indexingLag)
	}
	// restart begins a stored walk again from its first page. The walk's
	// lower bound and in-progress manifest are kept, so records already
	// written by the abandoned walk are deduplicated rather than replayed.
	restart := func(old traversal) traversal {
		old.Cursor, old.Started, old.Until = "", now, now
		if windowed(old) {
			old.Until = now.Add(-indexingLag)
		}
		if old.Manifest == nil {
			old.Manifest = manifest{}
		}
		return old
	}
	resumed := false
	if st.Traversal != nil {
		scan = *st.Traversal
		if scan.Cursor == "" || scan.Until.IsZero() {
			return nil, errors.New("invalid Compliance traversal state")
		}
		if scan.Manifest == nil {
			scan.Manifest = manifest{}
		}
		if scan.Started.IsZero() || now.Sub(scan.Started) >= maxTraversalAge {
			rt.Warn("Compliance stored page walk exceeded the cursor lifetime; restarting it from the first page", log.KV("dataset", d.Name))
			scan = restart(scan)
		} else {
			resumed = true
		}
	}
	tag, e := rt.NegotiateTag(c.Tag_Name)
	if e != nil {
		return nil, e
	}
	limiter := p.limiter
	seen := map[string]bool{}
	count := 0
	maxRecords := p.maxRecords
	if maxRecords == 0 {
		maxRecords = 100000
	}
	manifestLimit := p.maxManifestEntries
	if manifestLimit == 0 {
		manifestLimit = defaultMaxManifestEntries
	}
	for page := 0; page < c.Max_Pages; page++ {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		q := query(c, scan, windowed(scan))
		requestKey := q.Encode()
		if seen[requestKey] {
			return nil, errors.New("Compliance returned a repeated cursor")
		}
		seen[requestKey] = true
		body, e := p.request(ctx, token, q, limiter)
		if e != nil {
			// A rejected stored cursor (expired or no longer decodable) is
			// permanent for that cursor; restart the walk once instead of
			// failing identically on every later poll.
			if resumed && page == 0 && httpStatus(e) == http.StatusBadRequest {
				rt.Warn("Compliance rejected a stored page cursor; restarting the walk from the first page", log.KV("dataset", d.Name))
				scan, resumed = restart(scan), false
				seen = map[string]bool{}
				page--
				continue
			}
			return nil, e
		}
		resumed = false
		var envelope map[string]json.RawMessage
		if e = json.Unmarshal(body, &envelope); e != nil || envelope == nil {
			return nil, errors.New("Compliance response is not an object")
		}
		if raw := envelope["error"]; len(raw) > 0 && string(raw) != "null" {
			return nil, errors.New("Compliance returned an application error")
		}
		rows := []json.RawMessage{body}
		if d.Rows != "" {
			raw, ok := envelope[d.Rows]
			if !ok || string(raw) == "null" || json.Unmarshal(raw, &rows) != nil {
				return nil, fmt.Errorf("Compliance response missing %s array", d.Rows)
			}
		}
		if count > 0 && count+len(rows) > maxRecords {
			return hosted.ContinueNow(), nil
		}
		next, more, e := continuation(envelope, d)
		if e != nil {
			return nil, e
		}
		plans := make([]entry.Entry, 0, len(rows))
		for _, raw := range rows {
			count++
			compact, e := compactObject(raw, c.Max_Entry_Bytes)
			if e != nil {
				return nil, e
			}
			h := sha256.Sum256(compact)
			digest := hex.EncodeToString(h[:])
			key := identity(compact, d.Identity, digest)
			unchanged := st.Manifest.digestOf(key) == digest || scan.Manifest.digestOf(key) == digest
			recordTime := sourceTime(compact, d.Time, now)
			// seenTime orders manifest eviction and, for windowed datasets,
			// decides when an identity can no longer be returned again.
			seenTime := recordTime
			if d.Window != "" {
				seenTime = sourceTime(compact, d.Window, recordTime)
			}
			// Compare against the prior digest above (unchanged is already
			// decided), then compact: a key visited this scan no longer
			// needs to live in the pre-scan primary manifest, because the
			// put below immediately re-establishes its currency in the
			// in-progress scan manifest. Sizing that put's own limit to the
			// remaining room in the (shrinking, as compaction proceeds)
			// primary manifest -- rather than a second independent
			// manifestLimit -- keeps the combined retained-entry count
			// bounded by manifestLimit as a single shared budget, instead
			// of letting each manifest reach manifestLimit on its own
			// while a traversal is in progress. The floor of 1 guarantees
			// put can always insert the record currently being processed;
			// it only matters while the primary manifest is still fully
			// unvisited-and-stale (nothing yet compacted out of it), and
			// self-corrects to the full manifestLimit bound as soon as any
			// visited identity is compacted out of the primary manifest.
			delete(st.Manifest, key)
			budget := manifestLimit - len(st.Manifest)
			if budget < 1 {
				budget = 1
			}
			scan.Manifest.put(key, digest, seenTime, budget)
			if p.onRecord != nil {
				if e = p.onRecord(d, compact); e != nil {
					return nil, e
				}
			}
			if unchanged {
				continue
			}
			ent := entry.Entry{TS: entry.FromStandard(recordTime), Tag: tag, Data: compact}
			for _, kv := range [][2]string{{"_vendor", "Anthropic"}, {"_product", "Claude Enterprise Compliance"}, {"_source", d.Name}, {"_recordType", d.Name}, {"_endpoint", "/v1/compliance" + d.Path}, {"_apiVersion", "2023-06-01"}, {"_parent", strings.Join(c.Parameter, ",")}} {
				// Discovered parameter values are bounded well below this
				// limit (see maxDiscoveredParameterLen), but a directly
				// user-configured Parameter is not. Degrade the same way
				// "_session" does below rather than failing an otherwise
				// valid entry over a single oversized context field. Never
				// log the value itself, only its length.
				if kv[0] == "_parent" && len(kv[1]) > entry.MaxEvDataLength {
					rt.Warn("Compliance _parent context exceeds enumerated-value limit; entry retained without _parent", log.KV("dataset", d.Name), log.KV("bytes", len(kv[1])))
					continue
				}
				if e = ent.AddEnumeratedValueEx(kv[0], kv[1]); e != nil {
					return nil, e
				}
			}
			// Session/chat envelope context is retained intrinsically for each message.
			if raw := envelope["session"]; d.Rows != "" && len(raw) > 0 && string(raw) != "null" {
				v, e := compactObject(raw, c.Max_Response_Bytes)
				if e != nil {
					return nil, e
				}
				if len(v) <= entry.MaxEvDataLength {
					if e = ent.AddEnumeratedValueEx("_session", string(v)); e != nil {
						return nil, e
					}
				} else {
					rt.Warn("Compliance session context exceeds enumerated-value limit; message retained without _session", log.KV("dataset", d.Name), log.KV("bytes", len(v)))
				}
			}
			plans = append(plans, ent)
		}
		if p.flushRecords != nil {
			if e = p.flushRecords(); e != nil {
				return nil, e
			}
		}
		for _, plan := range plans {
			if e = rt.Write(plan); e != nil {
				return nil, e
			}
		}
		// SyncContext is the upstream muxer barrier, not proof that every
		// cached entry has reached a backend. Keep cache and state together.
		if e = p.syncIngest(ctx, 2*time.Minute); e != nil {
			return nil, fmt.Errorf("Compliance ingest synchronization: %w", e)
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		checkpoint := state{Since: st.Since, Manifest: st.Manifest, HistoryComplete: st.HistoryComplete}
		if more {
			scan.Cursor = next
			checkpoint.Traversal = &scan
		} else {
			checkpoint.Since = scan.Until.Add(-time.Duration(c.Overlap_Seconds) * time.Second)
			checkpoint.Manifest = scan.Manifest
			checkpoint.HistoryComplete = checkpoint.HistoryComplete || scan.FullHistory
			// The next windowed walk starts at checkpoint.Since, so an
			// identity last seen before it can never be returned again.
			// Dropping it bounds a windowed dataset's manifest by the
			// records inside one poll interval plus overlap.
			if d.Window != "" && !fullParents {
				for k, m := range checkpoint.Manifest {
					if m.Seen < checkpoint.Since.Unix() {
						delete(checkpoint.Manifest, k)
					}
				}
			}
		}
		b, e = json.Marshal(checkpoint)
		if e != nil {
			return nil, e
		}
		if e = rt.Put(c.key(), b); e != nil {
			return nil, e
		}
		if !more {
			return c.ContinueAfterInterval(), nil
		}
	}
	return hosted.ContinueNow(), nil
}

// query builds one page request for the dataset and traversal.
func query(c *Config, scan traversal, windowed bool) url.Values {
	d := c.dataset
	q := url.Values{}
	if d.Rows != "" {
		q.Set("limit", strconv.Itoa(c.Page_Size))
	}
	if windowed {
		q.Set(d.Window+".gte", scan.Since.Format(time.RFC3339Nano))
		// Local sessions document only updated_at.gte.
		if d.Name != "local-sessions" {
			q.Set(d.Window+".lte", scan.Until.Format(time.RFC3339Nano))
		}
	}
	if scan.Cursor != "" {
		q.Set(d.Cursor, scan.Cursor)
	}
	if d.Name == "activities" || strings.HasSuffix(d.Name, "messages") {
		q.Set("order", "asc")
	}
	if d.Name == "chats" {
		q.Set("order_by", "updated_at")
	}
	return q
}

// continuation applies the documented pagination contract: after_id
// endpoints continue only while has_more is true (absent means false) and
// return last_id; page endpoints stop when has_more is false or, on the
// session endpoints that omit has_more, when next_page is null.
func continuation(env map[string]json.RawMessage, d Dataset) (string, bool, error) {
	if d.Cursor == "" {
		return "", false, nil
	}
	var has bool
	b, present := env["has_more"]
	if present && string(b) != "null" {
		if json.Unmarshal(b, &has) != nil {
			return "", false, errors.New("invalid has_more")
		}
	} else {
		present = false
	}
	field := "next_page"
	if d.Cursor == "after_id" {
		field = "last_id"
	}
	if (d.Cursor == "after_id" || present) && !has {
		return "", false, nil
	}
	var next string
	if b, ok := env[field]; ok && string(b) != "null" {
		if json.Unmarshal(b, &next) != nil {
			return "", false, errors.New("invalid pagination token")
		}
	}
	if has && next == "" {
		return "", false, errors.New("continuing response has no next token")
	}
	return next, next != "", nil
}
func (p *Plugin) request(ctx context.Context, token string, q url.Values, limiter *rate.Limiter) ([]byte, error) {
	for attempt := 0; attempt <= p.conf.Max_Retries; attempt++ {
		if e := limiter.Wait(ctx); e != nil {
			return nil, e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com"+p.conf.path+"?"+q.Encode(), nil)
		if e != nil {
			return nil, errors.New("invalid Compliance request")
		}
		req.Header.Set("x-api-key", token)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Accept", "application/json")
		// Documented backoff: start at one second, double, cap at 60.
		delay := time.Duration(min(1<<attempt, 60)) * time.Second
		resp, e := p.http.Do(req)
		if e != nil {
			// A GET is idempotent, so a connection or timeout failure is
			// retried with the same bounded backoff as a transient 5xx.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == p.conf.Max_Retries {
				return nil, errors.New("Compliance transport failed")
			}
			if e = p.wait(ctx, delay); e != nil {
				return nil, e
			}
			continue
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, int64(p.conf.Max_Response_Bytes)+1))
		resp.Body.Close()
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == p.conf.Max_Retries {
				return nil, errors.New("incomplete Compliance response")
			}
			if e = p.wait(ctx, delay); e != nil {
				return nil, e
			}
			continue
		}
		if len(body) > p.conf.Max_Response_Bytes {
			return nil, errors.New("Compliance response exceeds size limit")
		}
		if resp.StatusCode == 200 {
			return body, nil
		}
		retry := (resp.StatusCode == 429 || resp.StatusCode >= 500) && resp.Header.Get("x-should-retry") != "false"
		if !retry || attempt == p.conf.Max_Retries {
			return nil, &statusError{resp.StatusCode}
		}
		if seconds, e := strconv.ParseUint(resp.Header.Get("retry-after"), 10, 32); e == nil {
			delay = max(delay, time.Duration(seconds)*time.Second)
		} else if when, e := http.ParseTime(resp.Header.Get("retry-after")); e == nil {
			delay = max(delay, time.Until(when))
		}
		if e = p.wait(ctx, delay); e != nil {
			return nil, e
		}
	}
	return nil, errors.New("Compliance retries exhausted")
}
func compactObject(raw []byte, maxBytes int) ([]byte, error) {
	s := bytes.TrimSpace(raw)
	if len(s) == 0 || s[0] != '{' {
		return nil, errors.New("Compliance record is not an object")
	}
	var b bytes.Buffer
	if e := json.Compact(&b, s); e != nil {
		return nil, errors.New("invalid Compliance JSON record")
	}
	if b.Len() > maxBytes {
		return nil, errors.New("Compliance entry exceeds size limit")
	}
	return b.Bytes(), nil
}
