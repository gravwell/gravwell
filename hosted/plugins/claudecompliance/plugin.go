package claudecompliance

import (
	"bytes"
	"container/heap"
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
	"sync"
	"time"
)

type Plugin struct {
	conf    *Config
	http    *http.Client
	now     func() time.Time
	limiter *rate.Limiter
	// syncIngest is the muxer's ingest delivery barrier, bound at build time
	// (see synchronizingTagNegotiator). No checkpoint advances until it
	// returns.
	syncIngest         func(context.Context, time.Duration) error
	maxRecords         int
	maxManifestEntries int
	// maxEntryBytes bounds a single decoded record. It is not a user-facing
	// option; it defaults to maxResponseBytes and exists so tests can drive
	// the oversize path.
	maxEntryBytes int
	// delivery remembers entries already accepted by the muxer for a page
	// whose delivery barrier or checkpoint later failed. It prevents the
	// runner's error retry from queuing the same page again and again while
	// preserving the rule that no durable checkpoint advances before sync.
	delivery *deliveryLedger
	// retry is the aggregate back-pressure wait budget for one Handle cycle.
	// Every dataset and discovered child of the cycle shares it, so a copy of
	// the Plugin made for a bound dataset or a child deliberately keeps the
	// same pointer.
	retry *retryBudget
	// sourceUse is this cycle's proof that the state namespace it selected is
	// usable with the credential that named it. It is shared by every bound
	// dataset and child copy for the same reason retry is: any one of their
	// requests proves it for the whole cycle.
	sourceUse *sourceUseProof
}

// sourceUseProof records whether one Handle cycle has actually used the state
// namespace it selected -- that is, whether a request issued under the current
// credential returned a well-formed successful response. It is a necessary
// condition for reclaiming the namespace a credential or origin change
// superseded (see reclaimSupersededSources). Without it, an operator who
// temporarily writes a wrong API key would have the previous namespace deleted
// before anything proved the replacement usable, and reverting the key would
// then restart that source at Lookback and skip every record older than it.
// Neither a second cycle nor another failed request proves anything more.
//
// It is not a sufficient condition. One success proves only the operation that
// returned it, so a replacement credential holding a subset of the prior one's
// scopes sets the proof on the dataset it may read and is still refused on the
// next. Handle therefore requires a completely successful cycle as well before
// anything is deleted.
//
// An error-free cycle is not sufficient either, which is what incomplete
// records. Collection is spread over cycles on purpose: a traversal hands back
// an immediate continuation, and a discovered child that failed is persisted
// Pending with a future RetryAt. A later cycle can then list what it is
// permitted, attempt nothing that is sleeping, and end with no error at all
// while the replacement's collection is still missing exactly the resource it
// was refused. Reclaiming on the strength of that would destroy the prior
// namespace's lower bound before anything had proven the replacement can read
// the whole of what that namespace covers, so reverting the credential could no
// longer resume it and the source would restart at Lookback. A cycle that
// leaves any part of its own collection unfinished therefore authorizes
// nothing, and the obligation simply stays recorded for a cycle that finishes.
type sourceUseProof struct {
	mu sync.Mutex
	ok bool
	// incomplete is set by whatever leaves work outstanding at the end of a
	// cycle, from any bound dataset or child copy, which is why they all share
	// one proof.
	incomplete bool
}

// observe records a successful use of the cycle's current namespace. A nil
// proof is simply never authorized; Handle always binds one.
func (s *sourceUseProof) observe() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ok = true
	s.mu.Unlock()
}

// observeIncomplete records that this cycle ends with collection unfinished:
// a traversal continuation still to walk, a discovered child still Pending
// (ready now or sleeping until its RetryAt), a page deferred for bounded
// capacity, or a root this cycle skipped while its stored walk is still
// mid-cursor. It is one-way for the cycle; later progress elsewhere in the same
// cycle cannot retract it.
func (s *sourceUseProof) observeIncomplete() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.incomplete = true
	s.mu.Unlock()
}

func (s *sourceUseProof) authorized() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ok && !s.incomplete
}

var (
	errResponseSizeLimit = errors.New("Compliance response exceeds size limit")
	errResponseReadLimit = errors.New("Compliance streamed response exceeds size limit")
	errRetryBudget       = errors.New("Compliance back-pressure wait budget for this cycle is exhausted; state retained")
)

// retryBudget bounds the total time one Handle cycle may spend waiting on
// vendor back-pressure. Each individual wait is already capped at the
// configured poll interval, but a multi-page cycle could otherwise chain many
// ceiling-sized waits together and hold the runner far longer than one poll.
// When the remaining budget cannot cover the next wait, the cycle stops at its
// last committed page and reports the server's own requested delay with the
// error: no page is skipped, no cursor is lost, and the next cycle resumes the
// stored cursor. The runner paces that retry with its own error delay; the
// reported delay is a plugin-local signal (see deferredRemoteError).
type retryBudget struct {
	mu        sync.Mutex
	remaining time.Duration
}

// newRetryBudget allows one configured poll interval of waiting per cycle:
// a cycle never sleeps longer than simply starting the next cycle would take.
func newRetryBudget(intervalSeconds int) *retryBudget {
	return &retryBudget{remaining: retryAfterCeiling(intervalSeconds)}
}

// consume reserves d, reporting false when the remaining budget cannot cover
// it. A nil budget is unbounded; New and Handle always bind one.
func (b *retryBudget) consume(d time.Duration) bool {
	if b == nil || d <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d > b.remaining {
		return false
	}
	b.remaining -= d
	return true
}

// wait applies one bounded backoff against the cycle's aggregate budget.
func (p *Plugin) wait(rt hosted.Runtime, d time.Duration) error {
	if !p.retry.consume(d) {
		return &deferredRemoteError{err: errRetryBudget, delay: d}
	}
	if rt.Sleep(d) {
		return context.Canceled
	}
	return nil
}

type deliverySyncError struct{ err error }

func (e *deliverySyncError) Error() string {
	return "Compliance ingest synchronization: " + e.err.Error()
}
func (e *deliverySyncError) Unwrap() error { return e.err }

// deferredRemoteError carries the delay a remote failure asked for. The shared
// job adapter retries a failed Handle after its own fixed JobErrorDelay and
// has no per-error schedule, so this delay is a *plugin-local* signal only: it
// is what lets a fan-out decide whether a sibling is ready to run immediately
// or whether the cycle should simply end. Teaching the runner to honor it
// would be a shared retry mechanism, which this plugin deliberately does not
// add; back-pressure that must actually be waited out is waited out inside the
// cycle, through Runtime.Sleep and the cycle's aggregate retry budget.
type deferredRemoteError struct {
	err   error
	delay time.Duration
}

const minimumDeferredRetry = 100 * time.Millisecond

func (e *deferredRemoteError) Error() string { return e.err.Error() }
func (e *deferredRemoteError) Unwrap() error { return e.err }
func (e *deferredRemoteError) RetryDelay() time.Duration {
	if e.delay < minimumDeferredRetry {
		return minimumDeferredRetry
	}
	return e.delay
}

func (p *Plugin) deferRemote(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var scheduled *deferredRemoteError
	if errors.As(err, &scheduled) {
		return err
	}
	return &deferredRemoteError{err: err, delay: retryAfterCeiling(p.conf.Request_Interval)}
}

type deliveryLedger struct {
	mu      sync.Mutex
	pages   map[string]map[[sha256.Size]byte]map[string]struct{}
	windows map[string]deliveryWindow
}

type deliveryWindow struct {
	Since       time.Time
	Until       time.Time
	Started     time.Time
	FullHistory bool
}

// writeOnce serializes writes for one plugin instance and remembers only
// successful writes. The ledger is process-local by design: durable state
// cannot be advanced before delivery without creating a data-loss window.
func (d *deliveryLedger) writeOnce(prefix string, page [sha256.Size]byte, key string, rt hosted.Runtime, ent entry.Entry) error {
	if d == nil {
		return rt.Write(ent)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	pages := d.pages[prefix]
	if pages == nil {
		pages = map[[sha256.Size]byte]map[string]struct{}{}
		d.pages[prefix] = pages
	}
	seen := pages[page]
	if _, ok := seen[key]; ok {
		return nil
	}
	if err := rt.Write(ent); err != nil {
		return err
	}
	if seen == nil {
		seen = map[string]struct{}{}
		pages[page] = seen
	}
	seen[key] = struct{}{}
	return nil
}

// pinWindow keeps the initial page's time bounds stable across in-process
// retries. Cursor-backed later pages already carry durable bounds in their
// stored traversal. Only scalar time fields are retained here: a failed
// attempt's in-memory manifest must never suppress rows that were not written.
func (d *deliveryLedger) pinWindow(prefix string, scan traversal) traversal {
	if d == nil {
		return scan
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if pinned, ok := d.windows[prefix]; ok {
		scan.Since = pinned.Since
		scan.Until = pinned.Until
		scan.Started = pinned.Started
		scan.FullHistory = pinned.FullHistory
		return scan
	}
	d.windows[prefix] = deliveryWindow{
		Since: scan.Since, Until: scan.Until, Started: scan.Started,
		FullHistory: scan.FullHistory,
	}
	return scan
}

// clearPrefix releases every successful-write marker and pinned initial
// window for a dataset after one of its pages commits. That bounds the ledger
// to the single uncommitted page that can be retried for each dataset.
func (d *deliveryLedger) clearPrefix(prefix string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	delete(d.pages, prefix)
	delete(d.windows, prefix)
	d.mu.Unlock()
}

func (d *deliveryLedger) reset() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.pages = map[string]map[[sha256.Size]byte]map[string]struct{}{}
	d.windows = map[string]deliveryWindow{}
	d.mu.Unlock()
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

// synchronizingTagNegotiator is the capability this plugin requires of the
// object the shared builder hands it. plugins.IngesterBuilder.Build is typed
// on hosted.TagNegotiator, which can only negotiate a tag, so the delivery
// barrier is asserted here instead of being declared as a shared interface.
// The assertion is made against the concrete object -- production passes the
// ingest muxer itself, which implements SyncContext -- and construction fails
// closed when it does not hold.
type synchronizingTagNegotiator interface {
	hosted.TagNegotiator
	// SyncContext blocks until the muxer has drained what it has been handed.
	// It is the only delivery evidence available to a plugin; Runtime.Write
	// alone merely queues an entry.
	SyncContext(context.Context, time.Duration) error
}

// New builds the plugin. tn must also provide the ingest delivery barrier:
// writing an entry only queues it, and a state-store sync is not proof of
// delivery, so without a barrier a checkpoint could advance past records that
// never reached a backend. That is a correctness requirement rather than an
// optional capability, so a negotiator without it is rejected here rather than
// quietly collected without a barrier.
func New(c *Config, tn hosted.TagNegotiator) (*Plugin, error) {
	if c == nil {
		return nil, errors.New("Compliance requires a configuration")
	}
	if tn == nil {
		return nil, errors.New("Compliance requires an ingest muxer providing SyncContext; a write alone is not a delivery barrier")
	}
	barrier, ok := tn.(synchronizingTagNegotiator)
	if !ok {
		return nil, errors.New("Compliance requires an ingest muxer providing SyncContext; a write alone is not a delivery barrier")
	}
	// Fail construction on an unreadable credential rather than on the first
	// poll. The state namespace itself is resolved against durable state at
	// the start of every cycle (see Config.selectNamespace), which this
	// constructor has no Runtime for.
	if _, err := c.credential(); err != nil {
		return nil, err
	}
	p := &Plugin{
		conf: c,
		http: &http.Client{
			Timeout:       90 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		now:        time.Now,
		limiter:    requestLimiter(c.Requests_Per_Minute),
		syncIngest: barrier.SyncContext,
		retry:      newRetryBudget(c.Request_Interval),
		delivery: &deliveryLedger{
			pages:   map[string]map[[sha256.Size]byte]map[string]struct{}{},
			windows: map[string]deliveryWindow{},
		},
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
	Since                 time.Time `json:"since"`
	Until                 time.Time `json:"until"`
	Cursor                string    `json:"cursor"`
	Manifest              manifest  `json:"manifest"`
	FullHistory           bool      `json:"full_history,omitempty"`
	ConsecutiveEmptyPages int64     `json:"consecutive_empty_pages,omitempty"`
	// Started is when this page walk issued its first request; a walk older
	// than maxTraversalAge restarts rather than reusing an expired cursor.
	Started time.Time `json:"started,omitempty"`
	// Deltas is how many records of this walk's per-page manifest journal are
	// durable. It is stored in its own key (see keyWalkDeltas) and is the
	// index the next page's record is written under.
	Deltas int64 `json:"deltas,omitempty"`
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

// manifestEvictor removes identities in oldest-Seen/key order while reducing
// repeated insertion at the manifest bound from a full map scan per record to
// one heap build plus logarithmic pops.
// Stale heap entries are expected when a key moves or is updated and are
// discarded after checking the current map value.
type manifestHeapItem struct {
	key     string
	entry   manifestEntry
	current bool
}

type manifestHeap []manifestHeapItem

func (h manifestHeap) Len() int { return len(h) }
func (h manifestHeap) Less(i, j int) bool {
	if h[i].entry.Seen != h[j].entry.Seen {
		return h[i].entry.Seen < h[j].entry.Seen
	}
	if h[i].key != h[j].key {
		return h[i].key < h[j].key
	}
	return !h[i].current && h[j].current
}
func (h manifestHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *manifestHeap) Push(v any)   { *h = append(*h, v.(manifestHeapItem)) }
func (h *manifestHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	*h = old[:n-1]
	return v
}

type manifestEvictor struct{ items manifestHeap }

func newManifestEvictor(committed, current manifest) *manifestEvictor {
	e := &manifestEvictor{items: make(manifestHeap, 0, len(committed)+len(current))}
	for k, v := range committed {
		e.items = append(e.items, manifestHeapItem{key: k, entry: v})
	}
	for k, v := range current {
		e.items = append(e.items, manifestHeapItem{key: k, entry: v, current: true})
	}
	heap.Init(&e.items)
	return e
}

func (e *manifestEvictor) track(key string, value manifestEntry) {
	heap.Push(&e.items, manifestHeapItem{key: key, entry: value, current: true})
}

// evict removes one identity and reports which key went and whether it came
// out of the in-progress walk manifest rather than the committed one, so a
// caller journaling the page's change can record the removal it performed.
func (e *manifestEvictor) evict(committed, current manifest) (string, bool, bool) {
	for e.items.Len() > 0 {
		candidate := heap.Pop(&e.items).(manifestHeapItem)
		from := committed
		if candidate.current {
			from = current
		}
		if actual, ok := from[candidate.key]; !ok || actual != candidate.entry {
			continue
		}
		delete(from, candidate.key)
		return candidate.key, candidate.current, true
	}
	return "", false, false
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
	// maxConsecutiveEmptyPages permits sparse session pagination while
	// bounding a vendor-controlled walk that continuously supplies a fresh
	// continuation token without any records.
	maxConsecutiveEmptyPages = 100
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
	// Lookback rather than at the beginning of time. A zero lookback anchors
	// collection at this poll.
	since := st.Since
	initial := since.IsZero()
	if initial {
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
	// A zero-lookback window starts at now, while the safe upper bound stays
	// one indexing-lag behind now. Persist the lower-bound anchor and wait for
	// the next poll instead of issuing an inverted gte/lte request. This also
	// makes the start time survive a restart before the first request.
	//
	// A stored page walk is excluded. A traversal only writes the dataset lower
	// bound when it finishes, so a walk interrupted by Max-Pages or the
	// per-cycle record bound is durable with st.Since still zero -- which is
	// also exactly the state an operator leaves behind by lowering Lookback to
	// zero while a first traversal is still mid-cursor. Anchoring there would
	// clear that cursor and the walk's in-progress manifest (see commitDataset)
	// and move the lower bound to now, skipping every record the rest of the
	// walk still has to read. The resumed walk below keeps its own bounds.
	if initial && c.PollingConfig.Lookback == 0 && st.Walk == nil && windowed(scan) {
		// This cycle issued no request, so it has proven nothing about the
		// namespace it selected and has collected none of what that namespace
		// covers. Record it as unfinished, or a sibling dataset's success in the
		// same cycle would authorize reclaiming the superseded namespace whose
		// lower bound reverting a credential still needs (see sourceUseProof).
		p.sourceUse.observeIncomplete()
		if e = commitDataset(rt, prefix, since, st.Manifest, st.HistoryComplete); e != nil {
			return nil, e
		}
		return c.ContinueAfterInterval(), nil
	}
	// restart begins a stored walk again from its first page. The walk's
	// lower bound and in-progress manifest are kept, so records already
	// written by the abandoned walk are deduplicated rather than replayed.
	restart := func(old traversal) traversal {
		old.Cursor, old.Started, old.Until = "", now, now
		old.ConsecutiveEmptyPages = 0
		if windowed(old) {
			old.Until = now.Add(-indexingLag)
		}
		if old.Manifest == nil {
			old.Manifest = manifest{}
		}
		return old
	}
	resumed := false
	pinInitialWindow := st.Walk == nil
	if st.Walk != nil {
		scan = *st.Walk
		if scan.Manifest == nil {
			scan.Manifest = manifest{}
		}
		if scan.Started.IsZero() || now.Sub(scan.Started) >= maxTraversalAge {
			rt.Warn("Compliance stored page walk exceeded the cursor lifetime; restarting it from the first page", log.KV("dataset", d.Name))
			scan = restart(scan)
			pinInitialWindow = true
		} else {
			resumed = true
		}
	}
	if pinInitialWindow {
		scan = p.delivery.pinWindow(prefix, scan)
	}
	evictor := newManifestEvictor(st.Manifest, scan.Manifest)
	tag, e := rt.NegotiateTag(c.tag())
	if e != nil {
		return nil, e
	}
	limiter := p.limiter
	seen := map[[sha256.Size]byte]bool{}
	count := 0
	maxRecords := p.maxRecords
	if maxRecords == 0 {
		maxRecords = 100000
	}
	manifestLimit := p.maxManifestEntries
	if manifestLimit == 0 {
		manifestLimit = defaultMaxManifestEntries
	}
	pageSize := c.Page_Size
	for page := 0; page < c.Max_Pages; page++ {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		q := query(c, scan, windowed(scan))
		if d.Rows != "" {
			q.Set("limit", strconv.Itoa(pageSize))
		}
		var body []byte
		var requestKey, pageID [sha256.Size]byte
		for {
			requestKey = sha256.Sum256([]byte(q.Encode()))
			if seen[requestKey] {
				return nil, p.deferRemote(errors.New("Compliance returned a repeated cursor"))
			}
			body, e = p.request(ctx, rt, token, q, limiter)
			if (errors.Is(e, errResponseSizeLimit) || errors.Is(e, errResponseReadLimit)) && d.Rows != "" && pageSize > 1 {
				if errors.Is(e, errResponseReadLimit) {
					// A chunked response has no announced size. Jump directly to
					// one record so an unbounded peer can consume at most two
					// bounded reads while still giving a valid page a recovery path.
					pageSize = 1
				} else {
					pageSize = max(1, pageSize/2)
				}
				q.Set("limit", strconv.Itoa(pageSize))
				rt.Warn("Compliance response exceeded the size limit; retrying with a smaller page", log.KV("dataset", d.Name), log.KV("limit", pageSize))
				continue
			}
			if e == nil {
				seen[requestKey] = true
				pageMaterial := append([]byte(prefix+"\x00"), requestKey[:]...)
				pageID = sha256.Sum256(pageMaterial)
			}
			break
		}
		if e != nil {
			// A rejected stored cursor (expired or no longer decodable) is
			// permanent for that cursor; restart the walk once instead of
			// failing identically on every later poll.
			if resumed && page == 0 && httpStatus(e) == http.StatusBadRequest {
				rt.Warn("Compliance rejected a stored page cursor; restarting the walk from the first page", log.KV("dataset", d.Name))
				scan, resumed = p.delivery.pinWindow(prefix, restart(scan)), false
				seen = map[[sha256.Size]byte]bool{}
				page--
				continue
			}
			return nil, p.deferRemote(e)
		}
		resumed = false
		envelope, rows, e := decodePage(body, d)
		if e != nil {
			return nil, p.deferRemote(e)
		}
		if applicationErrorEnvelope(envelope) {
			return nil, p.deferRemote(errors.New("Compliance returned an application error"))
		}
		// A well-formed successful response is proof that the credential which
		// named this state namespace is the right one for it. That, and nothing
		// weaker, is what may later authorize reclaiming the namespace this one
		// superseded; an authentication failure, a transport failure, or a cycle
		// that issued no request at all authorizes nothing.
		p.sourceUse.observe()
		if count+len(rows) > maxRecords {
			if count == 0 {
				return nil, p.deferRemote(errors.New("Compliance response page exceeds the per-cycle record bound"))
			}
			// This walk stops short of its end and resumes next cycle, so this
			// cycle has not collected everything this namespace covers.
			p.sourceUse.observeIncomplete()
			return hosted.ContinueNow(), nil
		}
		next, more, e := continuation(envelope, d)
		if e != nil {
			return nil, p.deferRemote(e)
		}
		if len(rows) == 0 && more {
			scan.ConsecutiveEmptyPages++
			if scan.ConsecutiveEmptyPages > maxConsecutiveEmptyPages {
				return nil, p.deferRemote(errors.New("Compliance returned too many consecutive empty pages"))
			}
		} else {
			scan.ConsecutiveEmptyPages = 0
		}
		// Session metadata is common to every row in this page. Validate and
		// compact it once so a large envelope cannot multiply work and warnings
		// by the number of messages in the response.
		var sessionContext string
		if raw := envelope["session"]; d.SessionContext && len(raw) > 0 && string(raw) != "null" {
			v, err := compactObject(raw, p.entryBound())
			if err != nil {
				rt.Warn("Compliance session context is not an object; messages retained without _session", log.KV("dataset", d.Name))
			} else if len(v) <= entry.MaxEvDataLength {
				sessionContext = string(v)
			} else {
				rt.Warn("Compliance session context exceeds enumerated-value limit; messages retained without _session", log.KV("dataset", d.Name), log.KV("bytes", len(v)))
			}
		}
		type writePlan struct {
			entry entry.Entry
			key   string
		}
		plans := make([]writePlan, 0, len(rows))
		// This page's own manifest change, journaled instead of rewriting both
		// complete maps (see commitPage).
		pageAdded := manifest{}
		pageDropped := map[string]struct{}{}
		pageRemoved := map[string]struct{}{}
		for _, raw := range rows {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			count++
			compact, e := compactObject(raw, p.entryBound())
			if e != nil {
				return nil, p.deferRemote(e)
			}
			// A repeated top-level field would leave deletion, identity,
			// timestamp and discovery decisions depending on which copy a
			// decoder kept, while the emitted bytes retain every copy. Fail the
			// page closed: nothing from this row is written and the dataset
			// checkpoint does not advance. Discovery effects from earlier valid
			// rows may still drain and persist before the page is retried.
			if field, repeated := duplicateDecisionField(compact, decisionFields(d)...); repeated {
				return nil, p.deferRemote(fmt.Errorf("Compliance record repeats the %s field", field))
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
			if d.Window != "" && d.Window != d.Time {
				seenTime = sourceTime(compact, d.Window, recordTime)
			}
			if seenTime.After(now) {
				seenTime = now
			}
			// Compare against the prior digest above (unchanged is already
			// decided), then move the visited key from the committed manifest
			// into the in-progress one. Both maps share one bound. When a new
			// identity needs room, evict the oldest identity across the two
			// maps rather than reducing the in-progress map's own capacity.
			// That preserves all identities when the live set fits the bound,
			// including when a full committed manifest contains only stale
			// identities from the preceding traversal.
			if _, held := st.Manifest[key]; held {
				delete(st.Manifest, key)
				pageDropped[key] = struct{}{}
			}
			if !scan.Manifest.has(key) {
				for len(st.Manifest)+len(scan.Manifest) >= manifestLimit {
					evicted, fromWalk, ok := evictor.evict(st.Manifest, scan.Manifest)
					if !ok {
						break
					}
					if fromWalk {
						pageRemoved[evicted] = struct{}{}
						delete(pageAdded, evicted)
					} else {
						pageDropped[evicted] = struct{}{}
					}
				}
			}
			manifestValue := manifestEntry{Digest: digest, Seen: seenTime.Unix()}
			scan.Manifest[key] = manifestValue
			pageAdded[key] = manifestValue
			delete(pageRemoved, key)
			evictor.track(key, manifestValue)
			if col != nil {
				if e = col.record(d, compact); e != nil {
					return nil, e
				}
			}
			if unchanged {
				continue
			}
			ent := entry.Entry{TS: entry.FromStandard(recordTime), Tag: tag, Data: compact}
			intrinsics := [][2]string{{"_source", d.Name}}
			if c.discovered {
				parent, err := parentContextValue(c.parentContext)
				if err != nil {
					return nil, err
				}
				intrinsics = append(intrinsics, [2]string{"_parent", parent})
			}
			for _, kv := range intrinsics {
				// Discovered parameter values are bounded well below this
				// limit (see maxDiscoveredParameterLen). Keep this final
				// encoding guard aligned with "_session" so a context field
				// can never fail an otherwise valid entry. Never log the
				// value itself, only its length.
				if kv[0] == "_parent" && len(kv[1]) > entry.MaxEvDataLength {
					rt.Warn("Compliance _parent context exceeds enumerated-value limit; entry retained without _parent", log.KV("dataset", d.Name), log.KV("bytes", len(kv[1])))
					continue
				}
				if e = ent.AddEnumeratedValueEx(kv[0], kv[1]); e != nil {
					return nil, e
				}
			}
			// Session/chat envelope context is retained intrinsically for each message.
			if sessionContext != "" {
				if e = ent.AddEnumeratedValueEx("_session", sessionContext); e != nil {
					return nil, e
				}
			}
			deliveryKey := key
			if d.Kind != Event {
				deliveryKey += "\x00" + digest
			}
			plans = append(plans, writePlan{entry: ent, key: deliveryKey})
		}
		if col != nil {
			if e = col.flush(); e != nil {
				return nil, e
			}
		}
		for _, plan := range plans {
			if e = p.delivery.writeOnce(prefix, pageID, plan.key, rt, plan.entry); e != nil {
				return nil, e
			}
		}
		// SyncContext is the upstream muxer barrier, not proof that every
		// cached entry has reached a backend. Keep cache and state together.
		if p.syncIngest == nil {
			return nil, errors.New("Compliance ingest synchronization is not configured")
		}
		if e = p.syncIngest(ctx, 2*time.Minute); e != nil {
			return nil, &deliverySyncError{err: e}
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if more {
			scan.Cursor = next
			delta := manifestDelta{Dropped: sortedKeySet(pageDropped), Removed: sortedKeySet(pageRemoved)}
			if len(pageAdded) > 0 {
				delta.Added = pageAdded
			}
			if e = commitPage(rt, prefix, st.Manifest, &scan, delta, !delta.empty()); e != nil {
				return nil, e
			}
			p.delivery.clearPrefix(prefix)
			continue
		}
		// The first discovered chat-message traversal is an unwindowed history
		// scan. Its first windowed successor still needs the same indexing-lag
		// cushion as every later window, or a message created shortly before the
		// history scan but indexed shortly after it can fall into the handoff gap.
		until := scan.Until
		if scan.FullHistory && d.Window != "" && !fullParents {
			until = until.Add(-indexingLag)
		}
		since := until.Add(-time.Duration(c.Overlap_Seconds) * time.Second)
		// Overlap may not move a *windowed* source behind the lower bound it was
		// just collected with: under a zero lookback that bound is the first-poll
		// anchor, and there is nothing before it to collect. The clamp is scoped
		// to a windowed scan on purpose. The unwindowed scan above -- a newly
		// discovered chat's full message history -- has just read everything the
		// endpoint holds, so its handoff to the first windowed poll deliberately
		// reaches back by one indexing lag plus the overlap; clamping that to the
		// scan's own Since would reopen precisely the gap the cushion closes.
		if windowed(scan) && since.Before(scan.Since) {
			since = scan.Since
		}
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
		p.delivery.clearPrefix(prefix)
		return c.ContinueAfterInterval(), nil
	}
	// Max-Pages exhausted with the walk still mid-cursor: same unfinished
	// collection as the record-bound continuation above.
	p.sourceUse.observeIncomplete()
	return hosted.ContinueNow(), nil
}

// decodePage walks only the small set of top-level envelope fields used by
// this plugin and streams a row array one element at a time. It therefore
// never materializes an attacker-sized map or a slice header for every array
// element before enforcing the endpoint's documented page limit.
func decodePage(body []byte, d Dataset) (map[string]json.RawMessage, []json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return nil, nil, errors.New("Compliance response is not an object")
	}
	envelope := map[string]json.RawMessage{}
	var rows []json.RawMessage
	rowsSeen := false
	fields := 0
	for dec.More() {
		fields++
		if d.Rows != "" && fields > maxEnvelopeFields {
			return nil, nil, errors.New("Compliance response has too many envelope fields")
		}
		nameToken, err := dec.Token()
		if err != nil {
			return nil, nil, errors.New("invalid Compliance response envelope")
		}
		name, ok := nameToken.(string)
		if !ok {
			return nil, nil, errors.New("invalid Compliance response envelope")
		}
		if d.Rows != "" && name == d.Rows {
			if rowsSeen {
				return nil, nil, fmt.Errorf("Compliance response repeats %s", d.Rows)
			}
			rowsSeen = true
			open, err := dec.Token()
			if err != nil || open != json.Delim('[') {
				return nil, nil, fmt.Errorf("Compliance response missing %s array", d.Rows)
			}
			for dec.More() {
				if d.Limit > 0 && len(rows) >= d.Limit {
					return nil, nil, fmt.Errorf("Compliance response exceeds documented page limit %d", d.Limit)
				}
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return nil, nil, fmt.Errorf("invalid Compliance %s row", d.Rows)
				}
				rows = append(rows, raw)
			}
			closeToken, err := dec.Token()
			if err != nil || closeToken != json.Delim(']') {
				return nil, nil, fmt.Errorf("invalid Compliance %s array", d.Rows)
			}
			continue
		}

		if name == "type" || name == "error" || name == "has_more" ||
			name == "last_id" || name == "next_page" || name == "session" {
			// A repeated envelope field would leave the pagination and
			// application-error decisions depending on which copy a decoder
			// kept. Reject the response instead of resolving it arbitrarily.
			if _, repeated := envelope[name]; repeated {
				return nil, nil, fmt.Errorf("Compliance response repeats %s", name)
			}
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return nil, nil, errors.New("invalid Compliance response envelope")
			}
			envelope[name] = raw
			continue
		}
		// Unknown fields are consumed one at a time and immediately released.
		// The outer body cap bounds the largest single skipped value.
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			return nil, nil, errors.New("invalid Compliance response envelope")
		}
	}
	last, err := dec.Token()
	if err != nil || last != json.Delim('}') {
		return nil, nil, errors.New("invalid Compliance response envelope")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, nil, errors.New("invalid trailing Compliance response data")
	}
	if d.Rows != "" && !rowsSeen {
		return nil, nil, fmt.Errorf("Compliance response missing %s array", d.Rows)
	}
	if d.Rows == "" {
		rows = []json.RawMessage{body}
	}
	return envelope, rows, nil
}

// applicationErrorEnvelope recognizes the vendor's top-level error shape
// without rejecting a valid single-object record that merely has an "error"
// field of its own.
func applicationErrorEnvelope(envelope map[string]json.RawMessage) bool {
	var kind string
	if json.Unmarshal(envelope["type"], &kind) != nil || kind != "error" {
		return false
	}
	raw, ok := envelope["error"]
	return ok && len(raw) > 0 && string(raw) != "null"
}

func parentContextValue(parameters []string) (string, error) {
	context := make(map[string]string, len(parameters))
	for _, parameter := range parameters {
		name, value, ok := strings.Cut(parameter, ":")
		if !ok || name == "" {
			return "", errors.New("invalid Compliance parent context")
		}
		context[name] = value
	}
	b, err := json.Marshal(context)
	if err != nil {
		return "", errors.New("invalid Compliance parent context")
	}
	return string(b), nil
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
		var err error
		if next, err = paginationToken(b); err != nil {
			return "", false, errors.New("invalid pagination token")
		}
	}
	if has && next == "" {
		return "", false, errors.New("continuing response has no next token")
	}
	return next, next != "", nil
}

// paginationToken accepts the documented opaque string token and preserves a
// JSON number's exact lexical form for defensive compatibility. Other JSON
// types are rejected rather than being reformatted into a different token.
func paginationToken(raw json.RawMessage) (string, error) {
	var token string
	if err := json.Unmarshal(raw, &token); err == nil {
		if len(token) > maxPaginationTokenBytes {
			return "", errors.New("pagination token exceeds size limit")
		}
		return token, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	n, ok := v.(json.Number)
	if !ok {
		return "", errors.New("pagination token is not a string or number")
	}
	token = n.String()
	if len(token) > maxPaginationTokenBytes {
		return "", errors.New("pagination token exceeds size limit")
	}
	return token, nil
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
				return nil, &deferredRemoteError{err: errors.New("Compliance transport failed"), delay: retryAfterCeiling(p.conf.Request_Interval)}
			}
			if e = p.wait(rt, delay); e != nil {
				return nil, e
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			retry := (resp.StatusCode == 429 || resp.StatusCode >= 500) && resp.Header.Get("x-should-retry") != "false"
			if !retry || attempt == p.conf.Max_Retries {
				return nil, &deferredRemoteError{err: &statusError{resp.StatusCode}, delay: retryAfterCeiling(p.conf.Request_Interval)}
			}
			retryAfter, overCeiling := boundedRetryAfter(resp.Header.Get("retry-after"), retryAfterCeiling(p.conf.Request_Interval))
			if overCeiling {
				return nil, &deferredRemoteError{err: &statusError{resp.StatusCode}, delay: retryAfterCeiling(p.conf.Request_Interval)}
			}
			delay = max(delay, retryAfter)
			if e = p.wait(rt, delay); e != nil {
				return nil, e
			}
			continue
		}
		if resp.ContentLength > maxResponseBytes {
			resp.Body.Close()
			return nil, p.deferRemote(errResponseSizeLimit)
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
		resp.Body.Close()
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == p.conf.Max_Retries {
				return nil, p.deferRemote(errors.New("incomplete Compliance response"))
			}
			if e = p.wait(rt, delay); e != nil {
				return nil, e
			}
			continue
		}
		if len(body) > maxResponseBytes {
			return nil, p.deferRemote(errResponseReadLimit)
		}
		return body, nil
	}
	return nil, p.deferRemote(errors.New("Compliance retries exhausted"))
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
