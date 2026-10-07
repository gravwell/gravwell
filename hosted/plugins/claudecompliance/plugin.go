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
	conf    *Config
	http    *http.Client
	now     func() time.Time
	limiter *rate.Limiter
	// syncIngest is the muxer's ingest delivery barrier, bound at build time
	// (see Synchronizer). No checkpoint advances until it returns.
	syncIngest         func(context.Context, time.Duration) error
	maxRecords         int
	maxManifestEntries int
	// maxEntryBytes bounds a single decoded record. It is not a user-facing
	// option; it defaults to maxResponseBytes and exists so tests can drive
	// the oversize path.
	maxEntryBytes int
}

// entryBound reports the per-record ceiling for this plugin.
func (p *Plugin) entryBound() int {
	if p.maxEntryBytes > 0 {
		return p.maxEntryBytes
	}
	return maxResponseBytes
}

// collector observes each decoded record of a traversal and is how child
// discovery hooks into a scan. Passing it down the call chain keeps a scan's
// behavior fixed for its whole lifetime, rather than depending on fields
// mutated on the Plugin itself.
type collector interface {
	// record is called once per decoded record, before it is written.
	record(Dataset, []byte) error
	// flush is called at each response-page boundary.
	flush() error
}

// Synchronizer is the ingest delivery barrier this plugin needs before it
// may advance a checkpoint. The shared muxer already implements it; it is
// named here so the requirement is an explicit, documented contract rather
// than an undeclared assumption about what a caller happens to pass.
type Synchronizer interface {
	SyncContext(context.Context, time.Duration) error
}

// New builds the plugin. tn must also provide the ingest delivery barrier:
// writing an entry only queues it, and a state-store sync is not proof of
// delivery, so without a barrier a checkpoint could advance past records
// that never reached a backend.
func New(c *Config, tn hosted.TagNegotiator) (*Plugin, error) {
	if c == nil {
		return nil, errors.New("Compliance requires a configuration")
	}
	s, ok := tn.(Synchronizer)
	if !ok {
		return nil, errors.New("Compliance requires an ingest muxer providing SyncContext; a write alone is not a delivery barrier")
	}
	p := &Plugin{
		conf: c,
		http: &http.Client{
			Timeout:       90 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:        time.Now,
		limiter:    requestLimiter(c.Requests_Per_Minute),
		syncIngest: s.SyncContext,
	}
	return p, nil
}

// requestLimiter returns the budget owned by one configured stanza. Every
// dataset and discovered child handled by that stanza shares this limiter.
// Rebuilding a stanza after configuration reload therefore applies both rate
// decreases and rate increases without retaining a process-global setting
// from a removed configuration.
func requestLimiter(rpm int) *rate.Limiter {
	if rpm < 1 {
		rpm = 30
	}
	return rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), 1)
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

// has reports whether key has been seen at all, which is all an immutable
// Event needs to know.
func (m manifest) has(key string) bool { _, ok := m[key]; return ok }

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

// fullParentScan reports whether a root inventory that does have a usable
// time filter must nonetheless be listed without it, so an unchanged parent
// still requeues child work. Chats are excluded: the vendor documents that
// order_by=updated_at returns a chat again whenever it receives a new
// message, moves project, or is deleted. An inventory with no documented
// filter at all is already a full scan through its empty catalog Window and
// needs no entry here.
func fullParentScan(c *Config) bool {
	return c.Follow_Children != "disabled" && c.dataset.Name == "local-sessions"
}

func (p *Plugin) handleOne(ctx context.Context, rt hosted.Runtime, col collector) (*hosted.Continuation, error) {
	c := p.conf
	token, e := c.credential()
	if e != nil {
		return nil, e
	}
	prefix := c.key()
	st, e := loadCheckpoint(rt, prefix)
	if e != nil {
		return nil, e
	}
	now := p.now().UTC()
	d := c.dataset
	// Resume from the stored checkpoint; with no stored state, start at
	// Lookback rather than at the beginning of time.
	since := st.Since
	if since.IsZero() {
		since = now.Add(-time.Duration(c.PollingConfig.Lookback) * time.Hour)
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
	if st.Walk != nil {
		scan = *st.Walk
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
	tag, e := rt.NegotiateTag(c.tag())
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
		body, e := p.request(ctx, rt, token, q, limiter)
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
			compact, e := compactObject(raw, p.entryBound())
			if e != nil {
				return nil, e
			}
			h := sha256.Sum256(compact)
			digest := hex.EncodeToString(h[:])
			key := identity(compact, d.Identity, digest)
			// An Event is immutable: having seen its identity at all is
			// proof it has already been written, whatever bytes the vendor
			// replays. A Record can legitimately change in place, so only an
			// identical digest proves nothing new arrived.
			var unchanged bool
			switch d.Kind {
			case Event:
				unchanged = st.Manifest.has(key) || scan.Manifest.has(key)
			default:
				unchanged = st.Manifest.digestOf(key) == digest || scan.Manifest.digestOf(key) == digest
			}
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
			// while a traversal is in progress.
			//
			// A brand new identity frees nothing from the primary manifest,
			// so when that manifest is already full the budget would be zero
			// and there would be no room for the record being processed.
			// Evict from the primary manifest until there is room, rather
			// than overflowing the bound: the identity dropped is the one the
			// vendor reports as least recently updated, and forgetting it can
			// only cause that single record to be written once more if it is
			// still returned -- never cause a changed record to be missed.
			delete(st.Manifest, key)
			for manifestLimit-len(st.Manifest) < 1 && len(st.Manifest) > 0 {
				st.Manifest.evictOldest()
			}
			budget := manifestLimit - len(st.Manifest)
			if budget < 1 {
				budget = 1
			}
			scan.Manifest.put(key, digest, seenTime, budget)
			if col != nil {
				if e = col.record(d, compact); e != nil {
					return nil, e
				}
			}
			if unchanged {
				continue
			}
			ent := entry.Entry{TS: entry.FromStandard(recordTime), Tag: tag, Data: compact}
			for _, kv := range [][2]string{{"_source", d.Name}, {"_parent", strings.Join(c.Parameter, ",")}} {
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
				v, e := compactObject(raw, p.entryBound())
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
		if col != nil {
			if e = col.flush(); e != nil {
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
		if p.syncIngest == nil {
			return nil, errors.New("Compliance ingest synchronization is not configured")
		}
		if e = p.syncIngest(ctx, 2*time.Minute); e != nil {
			return nil, fmt.Errorf("Compliance ingest synchronization: %w", e)
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if more {
			scan.Cursor = next
			if e = commitPage(rt, prefix, st.Manifest, scan); e != nil {
				return nil, e
			}
			continue
		}
		since := scan.Until.Add(-time.Duration(c.Overlap_Seconds) * time.Second)
		committed := scan.Manifest
		// The next windowed walk starts at since, so an identity last seen
		// before it can never be returned again. Dropping it bounds a
		// windowed dataset's manifest by the records inside one poll
		// interval plus overlap.
		if d.Window != "" && !fullParents {
			for k, m := range committed {
				if m.Seen < since.Unix() {
					delete(committed, k)
				}
			}
		}
		if e = commitDataset(rt, prefix, since, committed, st.HistoryComplete || scan.FullHistory); e != nil {
			return nil, e
		}
		return c.ContinueAfterInterval(), nil
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
func (p *Plugin) request(ctx context.Context, rt hosted.Runtime, token string, q url.Values, limiter *rate.Limiter) ([]byte, error) {
	for attempt := 0; attempt <= p.conf.Max_Retries; attempt++ {
		if e := limiter.Wait(ctx); e != nil {
			return nil, e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, p.conf.Host+p.conf.path+"?"+q.Encode(), nil)
		if e != nil {
			return nil, errors.New("invalid Compliance request")
		}
		req.Header.Set("x-api-key", token)
		req.Header.Set("anthropic-version", apiVersion)
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
			if rt.Sleep(delay) {
				return nil, context.Canceled
			}
			continue
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
		resp.Body.Close()
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == p.conf.Max_Retries {
				return nil, errors.New("incomplete Compliance response")
			}
			if rt.Sleep(delay) {
				return nil, context.Canceled
			}
			continue
		}
		if len(body) > maxResponseBytes {
			return nil, errors.New("Compliance response exceeds size limit")
		}
		if resp.StatusCode == 200 {
			return body, nil
		}
		retry := (resp.StatusCode == 429 || resp.StatusCode >= 500) && resp.Header.Get("x-should-retry") != "false"
		if !retry || attempt == p.conf.Max_Retries {
			return nil, &statusError{resp.StatusCode}
		}
		retryAfter, overCeiling := boundedRetryAfter(resp.Header.Get("retry-after"), retryAfterCeiling(p.conf.Request_Interval))
		if overCeiling {
			return nil, &statusError{resp.StatusCode}
		}
		delay = max(delay, retryAfter)
		if rt.Sleep(delay) {
			return nil, context.Canceled
		}
	}
	return nil, errors.New("Compliance retries exhausted")
}

func retryAfterCeiling(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	const maxSeconds = uint64((1<<63 - 1) / int64(time.Second))
	if uint64(seconds) > maxSeconds {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(seconds) * time.Second
}

// boundedRetryAfter accepts both HTTP forms without allowing an untrusted
// server header to delay this ingester beyond its configured poll interval.
// Numeric values are checked before conversion to time.Duration.
func boundedRetryAfter(value string, ceiling time.Duration) (time.Duration, bool) {
	if ceiling < 0 {
		ceiling = 0
	}
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(ceiling/time.Second) {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, false
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := time.Until(when)
	if delay > ceiling {
		return 0, true
	}
	return max(delay, 0), false
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
