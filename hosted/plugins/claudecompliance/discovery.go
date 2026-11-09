package claudecompliance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
)

// collectorFuncs adapts a pair of closures to the collector interface, so a
// scan is handed exactly the behavior it should have for its whole lifetime.
type collectorFuncs struct {
	recordFn func(Dataset, []byte) error
	flushFn  func() error
}

func (c collectorFuncs) record(d Dataset, raw []byte) error { return c.recordFn(d, raw) }
func (c collectorFuncs) flush() error                       { return c.flushFn() }

type work struct {
	Dataset   string
	Parameter []string
	Revision  string
	// Slot is the checkpoint slot this child owns inside its root's namespace,
	// and StateKey is the namespace that slot names. The worklist is the
	// durable ownership record for both: a slot is allocated here before the
	// child's first request and is handed back only once the obligation to
	// clear it has completed, which is what keeps the stanza's physical key
	// space finite under child churn (see childStateKey).
	Slot          int    `json:",omitempty"`
	StateKey      string `json:",omitempty"`
	Pending       bool
	LastCompleted time.Time
	LastAttempt   time.Time
	RetryAt       time.Time
	Failures      uint
	// ManifestEntries accounts for the committed plus in-progress dedup
	// entries owned by this child. The worklist uses the aggregate to keep all
	// discovered-child manifests under one stanza-wide budget.
	ManifestEntries  int
	ManifestMeasured bool
	// RetireAfterDrain keeps a deleted parent's child readable until an
	// already-started (or final best-effort) traversal completes or returns
	// 404. Only then is its checkpoint compacted and tombstoned.
	RetireAfterDrain bool
	// SeenThisScan and AbsentStreak support bounded retirement of child work
	// whose parent kind has no vendor deletion signal (see
	// absenceTrackedParents). Their zero values -- never seen, never missed --
	// are the correct starting state for a newly discovered item.
	SeenThisScan bool
	AbsentStreak uint
}
type worklist struct {
	Items        map[string]work
	LastRootScan time.Time `json:",omitzero"`
}

func readyWorkKeys(list worklist, now time.Time) []string {
	keys := make([]string, 0, len(list.Items))
	for k, w := range list.Items {
		if w.Pending && !now.Before(w.RetryAt) {
			keys = append(keys, k)
		}
	}
	// Order the oldest-attempted work first. A stored item with a zero
	// LastAttempt is deliberately lowest priority until its first attempt.
	sort.Slice(keys, func(i, j int) bool {
		a, b := list.Items[keys[i]].LastAttempt, list.Items[keys[j]].LastAttempt
		az, bz := a.IsZero(), b.IsZero()
		switch {
		case az && !bz:
			return false
		case !az && bz:
			return true
		case a.Equal(b):
			return keys[i] < keys[j]
		default:
			return a.Before(b)
		}
	})
	return keys
}

// sortedWorkKeys lists every worklist key in a stable order, so a retirement
// or cascade pass does not depend on map iteration order.
func sortedWorkKeys(items map[string]work) []string {
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func pendingRetryDelay(list worklist, now time.Time, ceiling time.Duration) time.Duration {
	delay := ceiling
	found := false
	for _, item := range list.Items {
		if !item.Pending || item.RetryAt.IsZero() || !item.RetryAt.After(now) {
			continue
		}
		candidate := item.RetryAt.Sub(now)
		if !found || candidate < delay {
			delay, found = candidate, true
		}
	}
	if !found || delay <= 0 {
		return ceiling
	}
	return min(delay, ceiling)
}

// pendingWorkRemains reports whether any discovered child of this worklist is
// still owed a collection attempt, whether it is ready now or sleeping until a
// future RetryAt. Unlike readyWorkKeys it deliberately ignores the clock: a
// child refused for want of scope and backing off is just as much unfinished
// collection as one that could be attempted immediately, and that is what
// decides whether a cycle may authorize reclaiming the namespace it superseded
// (see sourceUseProof).
func pendingWorkRemains(list worklist) bool {
	for _, item := range list.Items {
		if item.Pending {
			return true
		}
	}
	return false
}

func healthyWorkReady(list worklist, now time.Time) bool {
	keys := readyWorkKeys(list, now)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if list.Items[key].Failures != 0 {
			return false
		}
	}
	return true
}

func rootScanDue(list worklist, now time.Time, interval time.Duration) bool {
	return list.LastRootScan.IsZero() || now.Before(list.LastRootScan) || now.Sub(list.LastRootScan) >= interval
}

var (
	errPendingCapacity   = errors.New("Compliance pending child-work capacity reached")
	errPendingBytes      = errors.New("Compliance pending child-work byte capacity reached")
	errCleanupCapacity   = errors.New("Compliance retired-checkpoint cleanup capacity reached")
	errMissingParentID   = errors.New("Compliance parent identity is missing")
	errOversizedIdentity = errors.New("Compliance discovered identity exceeds safe parameter length")
	errInvalidIdentity   = errors.New("Compliance discovered identity contains an invalid character")
	errUnknownChild      = errors.New("unknown Compliance child dataset")
	errCrossFamilyChild  = errors.New("Compliance cross-family discovery requires an explicit tag mapping")
)

// capacityBackpressure reports whether err is one of the bounded-state
// conditions that defers the current page instead of failing the cycle.
func capacityBackpressure(err error) bool {
	return errors.Is(err, errPendingBytes) || errors.Is(err, errPendingCapacity) ||
		errors.Is(err, errCleanupCapacity)
}

// maxDiscoveredParameterLen bounds a vendor-supplied id/uuid before it can
// become a child Parameter value or a persisted worklist identity. Real
// vendor identities are short opaque tokens; this is a defensive ceiling far
// below entry.MaxEvDataLength (the hard limit on a single enumerated value),
// so a pathological or corrupted vendor id cannot make the derived "_parent"
// enumerated value fail to encode. The same ceiling applies to an
// operator-configured Parameter value, which is bound into the very same
// child request paths and persisted work items.
const (
	maxDiscoveredParameterLen = 256
	maxPersistedWorklistBytes = 32 << 20
	// maxPendingWorkItems is the supported queue ceiling, derived from the
	// worst-case *serialized* work item rather than from its worst-case
	// length. An identifier at maxDiscoveredParameterLen is stored three times
	// over -- in the worklist map key, in Parameter, and percent-encoded
	// inside the child's state key -- and invalidOpaqueID keeps every
	// permitted identifier at one JSON byte per byte, leaving percent-encoding
	// as the only expansion at three bytes per byte. The resulting worst case
	// is a little over three kilobytes per item, so this ceiling stays inside
	// the independent byte bound above with mutation headroom to spare while
	// still admitting one complete discovery page (maxDiscoveredWorkPerPage)
	// alongside existing work. The fixed parts of that state key -- the source
	// fingerprint, the role, the bounded catalog name of the owning discovery
	// root, and the child's own catalog name -- add well under two hundred
	// bytes, so they do not change the order of that estimate.
	// TestMaxPendingCeilingFitsWorstCaseWorklist measures the worst case
	// through the production key path rather than trusting this arithmetic.
	maxPendingWorkItems = 9000
	// Leave room for the bounded timestamp, retry and accounting fields that a
	// scheduled item can add before its next persist. Without headroom a list
	// shed to the exact byte ceiling can become unwritable when that item
	// completes and no other healthy pending item is a safe victim.
	worklistMutationHeadroom = 1 << 20
	// maxCleanupTasks bounds the durable checkpoint-reclamation backlog. It
	// matches the queue ceiling so every item one cycle can release has room
	// for its obligation; a backlog that cannot drain becomes ordinary
	// capacity backpressure rather than a lost obligation.
	maxCleanupTasks = maxPendingWorkItems
	// maxTombstones bounds the deletion tombstones a configured root retains.
	// Forgetting the oldest costs at most one more final drain of an already
	// deleted child -- the same cost the manifest bound already accepts -- and
	// is what keeps retirement bookkeeping bounded under child churn.
	maxTombstones = 1000
	// Discovered children share one aggregate dedup budget. A child may
	// temporarily add at most one per-dataset budget while it is running; the
	// aggregate is reconciled before the worklist is persisted.
	maxAggregateManifestEntries = defaultMaxManifestEntries
	// Organizations are the largest discovery page (1,000 records), and each
	// can create two direct children. Holding one complete page atomically keeps
	// capacity backpressure recoverable instead of alternating partial pages.
	maxDiscoveredWorkPerPage = 2000
)

// slotCeiling bounds the checkpoint slots one configured root can ever
// allocate: one for every item the bounded pending queue may hold, plus one
// for every reclamation the bounded cleanup backlog may still owe, because a
// slot is deliberately withheld while its clearing is outstanding. Both terms
// are fixed bounds, so the root's physical key space is finite.
//
// Allocation always takes the lowest free slot, so the number of slots a root
// ever touches is the high-water mark of simultaneously owned namespaces, not
// the number of children that have ever churned through it.
func slotCeiling(maxPending int) int {
	return maxPending + maxCleanupTasks
}

func checkpointManifestEntries(rt hosted.Runtime, prefix string) (int, error) {
	cp, err := loadCheckpoint(rt, prefix)
	if err != nil {
		return 0, err
	}
	total := len(cp.Manifest)
	if cp.Walk != nil {
		total += len(cp.Walk.Manifest)
	}
	return total, nil
}

func trimCheckpointManifests(rt hosted.Runtime, prefix string, target int) (int, error) {
	cp, err := loadCheckpoint(rt, prefix)
	if err != nil {
		return 0, err
	}
	walk := manifest{}
	if cp.Walk != nil {
		walk = cp.Walk.Manifest
	}
	total := len(cp.Manifest) + len(walk)
	if total <= target {
		return total, nil
	}
	evictor := newManifestEvictor(cp.Manifest, walk)
	for total > target {
		if _, _, ok := evictor.evict(cp.Manifest, walk); !ok {
			break
		}
		total--
	}
	// Both complete maps are rewritten here, which also folds and empties the
	// page journal whose records would otherwise restore what was just
	// trimmed.
	if err := writeManifests(rt, prefix, cp.Manifest, walk); err != nil {
		return 0, err
	}
	return total, nil
}

// reconcileManifestBudget keeps the aggregate retained dedup state of every
// discovered child inside one stanza-wide budget. active, when set, is the
// worklist key of the child whose page capacity is reserved for the request
// about to run; its reservation is exempt from equal-share trimming but not
// from the hard aggregate bound.
func reconcileManifestBudget(rt hosted.Runtime, list *worklist, dirty *bool, budget int, active string) error {
	total := 0
	for key, item := range list.Items {
		if !item.ManifestMeasured {
			count := 0
			var err error
			if item.StateKey != "" {
				count, err = checkpointManifestEntries(rt, item.StateKey)
				if err != nil {
					return err
				}
			}
			item.ManifestEntries = count
			item.ManifestMeasured = true
			list.Items[key] = item
			*dirty = true
		}
		total += item.ManifestEntries
	}
	// Trimming is demand driven. While the aggregate fits, every child keeps
	// what it has: enforcing an equal share unconditionally would discard a
	// manifest the moment it was built and make the very same records arrive
	// again on the next cycle even though there was room to remember them.
	if total <= budget {
		return nil
	}
	share := budget
	if len(list.Items) > 0 {
		share = max(1, budget/len(list.Items))
	}
	// Under pressure every discovered child gets an equal retained-identity
	// share, so the first child encountered cannot monopolize the stanza-wide
	// budget. The active child's durably reserved page capacity is excluded:
	// trimming the reservation that was just written for the request about to
	// run would make the reservation meaningless and re-ingest its page on
	// every cycle. It remains subject to the hard aggregate pass below.
	keys := make([]string, 0, len(list.Items))
	for key, item := range list.Items {
		if key != active && item.StateKey != "" && item.ManifestEntries > share {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := list.Items[keys[i]], list.Items[keys[j]]
		if a.Pending != b.Pending {
			return !a.Pending
		}
		at, bt := a.LastCompleted, b.LastCompleted
		if a.Pending {
			at, bt = a.LastAttempt, b.LastAttempt
		}
		if at.Equal(bt) {
			return keys[i] < keys[j]
		}
		return at.Before(bt)
	})
	for _, key := range keys {
		item := list.Items[key]
		remaining, err := trimCheckpointManifests(rt, item.StateKey, share)
		if err != nil {
			return err
		}
		total -= item.ManifestEntries - remaining
		item.ManifestEntries = remaining
		item.ManifestMeasured = true
		list.Items[key] = item
		*dirty = true
	}
	if total <= budget {
		return nil
	}
	// Rounding, an active reservation, or malformed pre-release state can
	// still leave an aggregate excess. Trim the oldest retained identities
	// until the hard total holds, reaching the active child only once no
	// sibling is left to give room back.
	overflow := keys
	if active != "" {
		if item, ok := list.Items[active]; ok && item.StateKey != "" {
			overflow = append(append([]string(nil), keys...), active)
		}
	}
	for _, key := range overflow {
		if total <= budget {
			break
		}
		item := list.Items[key]
		target := max(0, item.ManifestEntries-(total-budget))
		remaining, err := trimCheckpointManifests(rt, item.StateKey, target)
		if err != nil {
			return err
		}
		total -= item.ManifestEntries - remaining
		item.ManifestEntries = remaining
		item.ManifestMeasured = true
		list.Items[key] = item
		*dirty = true
	}
	if total > budget {
		return errors.New("Compliance child manifest budget cannot be reconciled")
	}
	return nil
}

func discoveryPendingFloor(names []string) int {
	floor := 0
	for _, name := range names {
		d, ok := Datasets[name]
		if !ok {
			continue
		}
		floor = max(floor, d.Limit*len(childSpecs(name)))
	}
	return floor
}

// capacityVictim returns the deterministic work item that may be evicted to
// honor Max-Pending. Completed work is preferred. A repeatedly failing
// pending item is eligible only when no completed item is available. Healthy
// pending work is never discarded because doing so could skip unread child
// data.
func capacityVictim(list worklist, protected map[string]struct{}) string {
	victim := ""
	for candidate, item := range list.Items {
		if _, scheduled := protected[candidate]; scheduled {
			continue
		}
		if item.RetireAfterDrain {
			continue
		}
		if !item.Pending && (victim == "" || item.LastCompleted.Before(list.Items[victim].LastCompleted) || (item.LastCompleted.Equal(list.Items[victim].LastCompleted) && candidate < victim)) {
			victim = candidate
		}
	}
	if victim != "" {
		return victim
	}
	for candidate, item := range list.Items {
		if _, scheduled := protected[candidate]; scheduled {
			continue
		}
		if item.Pending && item.Failures >= maxChildFailures && (victim == "" || item.LastAttempt.Before(list.Items[victim].LastAttempt) || (item.LastAttempt.Equal(list.Items[victim].LastAttempt) && candidate < victim)) {
			// This fallback intentionally includes RetireAfterDrain work. A
			// deleted parent's child that never returns the documented 404 must
			// not occupy capacity forever after exhausting bounded retries.
			victim = candidate
		}
	}
	return victim
}

// reconcileWorklistCapacity applies a newly lowered Max-Pending value to
// already stored work. Safely evictable entries are removed immediately.
// Healthy pending entries may temporarily remain above the new bound so they
// can finish; discovery will admit no new work until the list is back within
// capacity.
// limit bounds how many victims one cycle may release, because each one owes
// a durable reclamation obligation; a stored list far above the bound simply
// converges over several cycles instead of overflowing that backlog.
//
// The logical key travels with each victim because that is what a reclamation
// obligation is recorded against: the physical checkpoint namespace is a
// reusable slot and cannot stand in for the identity that owned it.
func reconcileWorklistCapacity(parent *Config, list *worklist, protected map[string]struct{}, dirty *bool, limit int) []prunedWork {
	var pruned []prunedWork
	for len(list.Items) > parent.Max_Pending && len(pruned) < limit {
		victim := capacityVictim(*list, protected)
		if victim == "" {
			return pruned
		}
		pruned = append(pruned, prunedWork{Key: victim, Item: list.Items[victim]})
		delete(list.Items, victim)
		*dirty = true
	}
	return pruned
}

// prunedWork is one work item removed from a worklist together with the
// logical identity it was stored under.
type prunedWork struct {
	Key  string
	Item work
}

// absentRetirementThreshold is the number of consecutive, complete,
// unwindowed parent enumerations in which a child's parent must be observed
// absent before that child's work is retired. It intentionally requires more
// than one miss so that a single incomplete/racy listing can never retire
// live work.
const absentRetirementThreshold = 3

// maxChildFailures is the number of consecutive attempt failures after
// which a pending child is treated as stuck rather than actively
// in-progress, for the sole purpose of Max-Pending eviction eligibility
// (see the capacity check in Handle). It matches the existing cap already
// applied to w.Failures itself when computing retry backoff, so reaching
// this threshold already means the child has been failing, with
// exponentially growing backoff between attempts, for an extended period
// -- not a single transient error.
const maxChildFailures = 10

// absenceTrackedParents are the root parent dataset kinds that have no
// vendor-reported deletion signal (unlike "chats", which reports
// deleted_at). For these, and only these, absence from a complete,
// unwindowed full listing -- observed on absentRetirementThreshold
// consecutive such listings -- is treated as a safe proxy for deletion.
// It excludes "remote-sessions", which is refreshed every poll rather than
// hourly. A parameter-bound "organization-roles" selector can also be an
// explicit discovery root, so its complete role listing authoritatively ages
// out permission work for roles that disappear.
var absenceTrackedParents = map[string]bool{
	"organizations":      true,
	"organization-roles": true,
	"groups":             true,
	"projects":           true,
	"local-sessions":     true,
}

// Handle runs one collection cycle for every dataset this stanza selects.
// Each is collected against its own bound config and its own checkpoint, so
// one stanza can cover a whole family without a config stanza per endpoint.
func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	// Phase one of a source handover happens inside cycleConfig: the state
	// namespace this stanza owns is selected from a finite set and recorded,
	// and the namespace a credential or origin change left behind is
	// *retained*. Nothing is cleared there. Only the one-way fingerprints that
	// name a namespace are stored, never credential material.
	conf, changed, err := p.conf.cycleConfig(rt)
	if err != nil {
		return nil, err
	}
	if changed {
		p.delivery.reset()
	}
	cycle := *p
	cycle.conf = conf
	// One aggregate back-pressure wait budget for the whole cycle, shared by
	// every dataset and discovered child it visits.
	cycle.retry = newRetryBudget(conf.Request_Interval)
	// One proof, shared the same way, that the namespace this cycle selected is
	// really usable with the credential that named it.
	cycle.sourceUse = &sourceUseProof{}
	cont, err := cycle.handleConfigured(ctx, rt)
	// Phase two: reclamation, authorized by nothing except a completely
	// successful cycle that also proved the namespace it selected usable. A
	// cycle whose requests were all rejected -- a mistyped or expired API key is
	// non-empty and selects its own namespace just as a valid one does -- leaves
	// the superseded namespace and everything inside it (lower bound, manifests,
	// child worklist, cleanup backlog, tombstones) exactly where it is, so
	// restoring the credential resumes that source instead of restarting it at
	// Lookback and skipping everything older. Another failed cycle proves no
	// more than the first did.
	//
	// The proof alone is not enough. One well-formed successful response says
	// only that the credential works for the operation that returned it, and a
	// replacement credential can be granted a subset of the prior one's scopes:
	// it collects one configured dataset and is refused 401/403 on the next. A
	// cycle that ends in any error -- a sibling or child dataset refused for
	// want of scope, a decode, record-bound, write, delivery-synchronization or
	// checkpoint failure after the proof was observed -- has not shown that the
	// replacement can carry the whole of what the superseded namespace holds, so
	// it reclaims nothing and the obligation stays recorded for a later complete
	// cycle.
	//
	// Nor is an error-free cycle enough on its own, because collection is spread
	// over cycles by design. The refused child above is persisted Pending with a
	// future RetryAt, so the *next* cycle lists what it is permitted, never
	// reaches the sleeping child, and returns no error at all -- while the
	// replacement's collection is still missing precisely that child. A
	// traversal that handed back an immediate continuation, a page deferred
	// because child-work capacity was full, and a cycle that skipped its root to
	// drain healthy children while that root's own walk is still stored
	// mid-cursor all leave the same gap. authorized()
	// therefore also requires that the cycle left nothing of its own
	// outstanding; anything less keeps the obligation recorded rather than
	// destroying a prior lower bound that reverting the credential would need to
	// resume.
	//
	// A namespace the per-cycle clearing budget could not finish is likewise
	// kept recorded by reclaimSupersededSources itself and reclaimed by a
	// later complete cycle, without failing this one; only a failure to read
	// or rewrite the record that holds those obligations is reported here,
	// because leaving it unreported would silently stop every later retry.
	if err == nil && cycle.sourceUse.authorized() {
		if e := reclaimSupersededSources(rt, conf.sourceID); e != nil {
			return nil, e
		}
	}
	return cont, err
}

func (p *Plugin) handleConfigured(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if len(p.conf.Dataset) == 1 {
		return p.handleDataset(ctx, rt)
	}
	var errs []error
	pending := false
	for _, name := range p.conf.Dataset {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		bound, e := p.conf.bind(name)
		if e != nil {
			return nil, e
		}
		one := *p
		one.conf = bound
		cont, e := one.handleDataset(ctx, rt)
		if e != nil {
			var syncErr *deliverySyncError
			if errors.As(e, &syncErr) {
				return nil, e
			}
			errs = append(errs, fmt.Errorf("dataset %s: %w", name, e))
			continue
		}
		pending = pending || (cont != nil && cont.Delay == 0)
	}
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		immediate := pending
		for _, err := range errs {
			var scheduled interface{ RetryDelay() time.Duration }
			if errors.As(err, &scheduled) && scheduled.RetryDelay() == minimumDeferredRetry {
				immediate = true
				break
			}
		}
		if immediate {
			return nil, &deferredRemoteError{err: joined, delay: minimumDeferredRetry}
		}
		return nil, joined
	}
	return p.conf.PendingOrInterval(pending), nil
}

func (p *Plugin) handleDataset(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if p.conf.Follow_Children == "disabled" {
		return p.handleOne(ctx, rt, nil)
	}
	stanza := p.conf.key()
	key := stanza + keyChildren
	cleanupKey := stanza + keyCleanup
	tombstoneKey := stanza + keyTombstones
	list, e := loadWorklist(rt, key)
	if e != nil {
		return nil, e
	}
	cleanup, e := loadCleanup(rt, cleanupKey)
	if e != nil {
		return nil, e
	}
	// The deletion tombstones this root retains are keyed by logical work
	// identity rather than by checkpoint prefix, because a checkpoint prefix
	// here is a reusable slot: a prefix-keyed tombstone would be inherited by
	// whichever unrelated child the slot is handed to next and would suppress
	// it forever. They are read once per cycle and written only when they
	// actually change.
	tombstones, e := loadTombstones(rt, tombstoneKey)
	if e != nil {
		return nil, e
	}
	noteTombstone := func(identity, revision string) error {
		updated, changed := addTombstone(tombstones, identity, revision)
		tombstones = updated
		if !changed {
			return nil
		}
		return putTombstones(rt, tombstoneKey, tombstones)
	}
	dirty := false
	cleanupDirty := false
	// A child can discover grandchildren while this Handle call is consuming
	// an already-sorted worklist. Protect the keys selected for this cycle so
	// capacity eviction cannot invalidate the schedule or delete the item that
	// the outer loop will later write back.
	protected := map[string]struct{}{}
	commitCleanup := func() error {
		if !cleanupDirty {
			return nil
		}
		if err := putCleanup(rt, cleanupKey, cleanup); err != nil {
			return err
		}
		cleanupDirty = false
		return nil
	}
	// allocateSlot picks the smallest checkpoint slot that neither a stored
	// work item nor an outstanding reclamation obligation still owns. Slots
	// are what bound this root's key space: a retired child's namespace is
	// cleared and handed back rather than abandoned. A slot is deliberately
	// withheld while its reclamation is still pending, because reusing it
	// first would let that pending clearing wipe its successor's checkpoint.
	allocateSlot := func() (int, error) {
		used := make(map[int]struct{}, len(list.Items)+len(cleanup))
		for _, item := range list.Items {
			if item.Slot > 0 {
				used[item.Slot] = struct{}{}
			}
		}
		for _, task := range cleanup {
			if task.Slot > 0 {
				used[task.Slot] = struct{}{}
			}
		}
		for slot := 1; slot <= slotCeiling(p.conf.Max_Pending); slot++ {
			if _, taken := used[slot]; !taken {
				return slot, nil
			}
		}
		// Every slot is spoken for. That is ordinary bounded-capacity
		// back-pressure, handled exactly like a full pending queue, and never
		// a reason to write outside the finite key space.
		return 0, errPendingCapacity
	}
	// bindSlot gives w the namespace it collects into. Callers run it before
	// the work item is stored, so the allocation is durable before the first
	// request that could write into the slot.
	bindSlot := func(w *work) error {
		if w.Slot <= 0 {
			slot, err := allocateSlot()
			if err != nil {
				return err
			}
			w.Slot, w.StateKey = slot, childStateKey(stanza, slot)
			return nil
		}
		if w.StateKey == "" {
			w.StateKey = childStateKey(stanza, w.Slot)
		}
		return nil
	}
	// stage records a durable obligation to reclaim w's checkpoint namespace
	// and, with it, to release the slot that namespace occupies. It only
	// mutates the in-memory backlog; commitCleanup writes it, and that write
	// always happens before the worklist releases ownership of the child, so
	// neither a crash nor a failed worklist write can orphan an owned
	// checkpoint, hand out a slot that still holds data, or lose the
	// obligation. identity is the logical work key, which is what a deletion
	// tombstone is recorded against.
	stage := func(identity string, w work, tombstone bool) error {
		if w.StateKey == "" {
			// An item that never had a namespace of its own holds no slot and
			// has nothing to reclaim.
			return nil
		}
		retired := ""
		if tombstone {
			retired = w.Revision
		}
		for i := range cleanup {
			if cleanup[i].StateKey != w.StateKey {
				continue
			}
			if retired != "" && cleanup[i].Retired != retired {
				cleanup[i].Retired, cleanup[i].Key = retired, identity
				cleanupDirty = true
			}
			return nil
		}
		if len(cleanup) >= maxCleanupTasks {
			return errCleanupCapacity
		}
		cleanup = append(cleanup, cleanupTask{StateKey: w.StateKey, Slot: w.Slot, Key: identity, Retired: retired})
		cleanupDirty = true
		return nil
	}
	// runCleanup acts on every obligation the durable worklist no longer owns.
	// An obligation whose child is still owned is simply dropped: the worklist
	// write that would have released it never happened. The in-process
	// delivery ledger for a prefix is released only once its checkpoint has
	// actually been reclaimed, which bounds that ledger across repeated child
	// creation and retirement.
	runCleanup := func() error {
		if len(cleanup) == 0 {
			return nil
		}
		owned := make(map[string]struct{}, len(list.Items))
		for _, item := range list.Items {
			// Two work items can only share a state key if they are the same
			// item, so an item that still lacks a resolved key cannot be the
			// owner of any staged obligation.
			if item.StateKey != "" {
				owned[item.StateKey] = struct{}{}
			}
		}
		remaining := make([]cleanupTask, 0, len(cleanup))
		var failure error
		for _, task := range cleanup {
			if _, still := owned[task.StateKey]; still {
				continue
			}
			if failure != nil {
				remaining = append(remaining, task)
				continue
			}
			// The tombstone is recorded before the checkpoint is cleared, so a
			// failure between the two costs one more final drain of an
			// already-deleted child rather than losing the proof of deletion.
			if task.Retired != "" && task.Key != "" {
				if err := noteTombstone(task.Key, task.Retired); err != nil {
					failure = err
					remaining = append(remaining, task)
					continue
				}
			}
			if err := compact(rt, task.StateKey); err != nil {
				failure = err
				remaining = append(remaining, task)
				continue
			}
			p.delivery.clearPrefix(task.StateKey)
		}
		if len(remaining) != len(cleanup) {
			cleanup, cleanupDirty = remaining, true
			if err := commitCleanup(); err != nil {
				return err
			}
		}
		return failure
	}
	reload := func() error {
		stored, e := loadWorklist(rt, key)
		if e != nil {
			return e
		}
		list = stored
		dirty = false
		durable, e := loadCleanup(rt, cleanupKey)
		if e != nil {
			return e
		}
		cleanup, cleanupDirty = durable, false
		return nil
	}
	persist := func(reserveMutationHeadroom bool) error {
		if !dirty {
			return nil
		}
		limit := maxPersistedWorklistBytes
		if reserveMutationHeadroom {
			limit -= worklistMutationHeadroom
		}
		target := maxPersistedWorklistBytes - worklistMutationHeadroom
		candidate := worklist{Items: make(map[string]work, len(list.Items)), LastRootScan: list.LastRootScan}
		for k, w := range list.Items {
			candidate.Items[k] = w
		}
		var encoded []byte
		for {
			b, e := json.Marshal(candidate)
			if e != nil {
				return e
			}
			if len(b) <= limit {
				encoded = b
				break
			}

			// Max-Pending bounds the number of work items, while this bound
			// limits their serialized bytes. Pre-release or corrupted stored
			// state can exceed it even though every accepted configuration
			// fits. Shed the same safe victims used for capacity eviction
			// until the state fits, reclaiming their checkpoints without
			// tombstones so unchanged parents may rediscover them.
			remaining := len(b) - target
			for remaining > 0 {
				victim := capacityVictim(candidate, protected)
				if victim == "" {
					return errPendingBytes
				}
				item := candidate.Items[victim]
				enc, e := json.Marshal(map[string]work{victim: item})
				if e != nil {
					return e
				}
				removed := len(enc) - 2
				if len(candidate.Items) > 1 {
					removed++ // comma separating this item from another map entry
				}
				if e = stage(victim, item, false); e != nil {
					if errors.Is(e, errCleanupCapacity) {
						// Ownership cannot be released faster than the
						// reclamation backlog drains. Defer this page the same
						// way ordinary byte pressure does rather than dropping
						// either the item or its obligation.
						return errPendingBytes
					}
					return e
				}
				delete(candidate.Items, victim)
				remaining -= removed
			}
		}
		// The worklist is the ownership record for child checkpoints, and the
		// cleanup backlog is the record of what is owed once ownership ends.
		// Write the backlog first, then the worklist, then act: a failed
		// worklist write leaves both the stored list and every checkpoint
		// untouched, and a crash after it leaves the obligation durable.
		if e := commitCleanup(); e != nil {
			return e
		}
		if e := rt.Put(key, encoded); e != nil {
			return e
		}
		list = candidate
		dirty = false
		return runCleanup()
	}
	persistOwned := func() error { return persist(true) }
	persistMutation := func() error { return persist(false) }
	// Obligations left by an earlier cycle or a restart are retried first.
	if e = runCleanup(); e != nil {
		return nil, e
	}
	if e = reconcileManifestBudget(rt, &list, &dirty, maxAggregateManifestEntries, ""); e != nil {
		return nil, e
	}
	for _, pruned := range reconcileWorklistCapacity(p.conf, &list, protected, &dirty, maxCleanupTasks-len(cleanup)) {
		if e = stage(pruned.Key, pruned.Item, false); e != nil {
			return nil, e
		}
	}
	if e = persistMutation(); e != nil {
		return nil, e
	}
	discover := func(parent *Config) collector {
		return collectorFuncs{flushFn: persistOwned, recordFn: func(d Dataset, raw []byte) error {
			if retiredItems, ok, e := deletedChildWork(d, parent.Parameter, raw); e != nil {
				if errors.Is(e, errRecordFieldLimit) || errors.Is(e, errInvalidRecord) {
					rt.Warn("Compliance deleted parent fields are invalid; parent retained and child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errMissingParentID) {
					rt.Warn("Compliance deleted parent is missing its child-discovery identity; child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errOversizedIdentity) {
					rt.Warn("Compliance deleted parent identity exceeds safe parameter length; child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errInvalidIdentity) {
					rt.Warn("Compliance deleted parent identity contains an invalid character; child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				return e
			} else if ok {
				for _, retiring := range retiredItems {
					k := retiring.Dataset + "/" + strings.Join(retiring.Parameter, "/")
					old, exists := list.Items[k]
					if !exists {
						// The tombstone is looked up by logical identity, so a
						// still-deleted parent stays suppressed however many
						// times its checkpoint slot has since been reused.
						if retiredIdentity(tombstones, k, retiring.Revision) {
							continue
						}
						if len(list.Items) >= p.conf.Max_Pending {
							victim := capacityVictim(list, protected)
							if victim == "" {
								return errPendingCapacity
							}
							if ce := stage(victim, list.Items[victim], false); ce != nil {
								return ce
							}
							delete(list.Items, victim)
							dirty = true
						}
						retiring.LastAttempt = p.now().UTC()
					} else {
						retiring.Slot, retiring.StateKey = old.Slot, old.StateKey
						retiring.LastCompleted = old.LastCompleted
						retiring.LastAttempt = old.LastAttempt
						retiring.RetryAt = old.RetryAt
						retiring.Failures = old.Failures
						retiring.ManifestEntries = old.ManifestEntries
						retiring.ManifestMeasured = old.ManifestMeasured
					}
					// The final drain needs a namespace of its own before it
					// can run; the slot it takes is released only once the
					// reclamation its retirement records has completed.
					if ce := bindSlot(&retiring); ce != nil {
						return ce
					}
					retiring.Pending = true
					retiring.RetireAfterDrain = true
					list.Items[k] = retiring
					dirty = true
				}
				return nil
			}
			children, e := childWork(d, parent.Parameter, raw)
			if e != nil {
				if errors.Is(e, errRecordFieldLimit) || errors.Is(e, errInvalidRecord) {
					rt.Warn("Compliance parent fields are invalid; parent retained and child discovery skipped", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errMissingParentID) {
					rt.Warn("Compliance parent is missing its child-discovery identity; parent retained and child discovery skipped", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errOversizedIdentity) {
					rt.Warn("Compliance parent identity exceeds safe parameter length; child expansion skipped for this record", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errInvalidIdentity) {
					rt.Warn("Compliance parent identity contains an invalid character; child expansion skipped for this record", log.KV("dataset", d.Name))
					return nil
				}
				return e
			}
			tracked := absenceTrackedParents[d.Name]
			active := d.Name == "remote-sessions" && jsonString(raw, "status") == "active"
			for _, w := range children {
				k := w.Dataset + "/" + strings.Join(w.Parameter, "/")
				old, exists := list.Items[k]
				if tracked && exists && (!old.SeenThisScan || old.AbsentStreak != 0) {
					old.SeenThisScan, old.AbsentStreak = true, 0
					list.Items[k] = old
					dirty = true
				}
				if !exists && retiredIdentity(tombstones, k, w.Revision) {
					// A vendor-confirmed deletion is remembered against the
					// logical identity, so it still suppresses this child
					// whatever has since occupied its old checkpoint slot.
					continue
				}
				if !exists && len(list.Items) >= p.conf.Max_Pending {
					// Completed history is expendable; pending work is never
					// evicted -- except a child stuck at maxChildFailures
					// (see its doc comment), which is only ever considered
					// once no genuinely non-pending candidate exists. This
					// keeps a healthy, in-progress child fully protected
					// while still giving Max-Pending capacity a way out of
					// being permanently occupied by a child that can never
					// succeed, which would otherwise wedge discovery of
					// every subsequent new child on this parent forever.
					victim := capacityVictim(list, protected)
					if victim == "" {
						return errPendingCapacity
					}
					if e := stage(victim, list.Items[victim], false); e != nil {
						return e
					}
					delete(list.Items, victim)
					dirty = true
				}
				// Membership/content can change without a parent revision, so child
				// families refresh hourly. A local session's updated_at is its last
				// inference call, so its revision already changes whenever the
				// transcript grows; re-reading every transcript hourly would only
				// spend the shared budget. Active remote sessions refresh each poll.
				refresh := !old.Pending && ((d.Name == "remote-sessions" && active) || (d.Name != "local-sessions" && p.now().Sub(old.LastCompleted) >= time.Hour))
				if !exists || old.Revision != w.Revision || refresh {
					// A child that is already stored keeps the namespace it
					// owns; a new one is allocated the lowest free slot before
					// it is stored, so its ownership is durable before the
					// first request that can write into it.
					w.Slot, w.StateKey = old.Slot, old.StateKey
					if e := bindSlot(&w); e != nil {
						return e
					}
					w.Pending = true
					w.LastCompleted = old.LastCompleted
					if exists {
						w.LastAttempt = old.LastAttempt
					} else {
						// A never-attempted item defaults its LastAttempt to the zero
						// value, which would otherwise sort ahead of every item that has
						// already been attempted (see the scheduling sort below). Seed it
						// to the discovery time instead so a fresh arrival is queued
						// fairly relative to older work rather than jumping ahead of it.
						w.LastAttempt = p.now().UTC()
					}
					// Failures reflects the outcome of this plugin's own requests
					// against this child resource, not the freshness of the
					// parent's content, so it must survive a revision change --
					// otherwise a permanently-failing child whose parent content
					// keeps changing (e.g. a live member_count) can never reach
					// maxChildFailures and becomes permanently ineligible for the
					// stuck-pending eviction fallback above. RetryAt is content-
					// scoped, not request-scoped, so it still resets on a revision
					// change to let genuinely new content retry promptly.
					w.Failures = old.Failures
					w.ManifestEntries = old.ManifestEntries
					w.ManifestMeasured = old.ManifestMeasured
					if old.Revision == w.Revision {
						w.RetryAt = old.RetryAt
					}
					if tracked {
						w.SeenThisScan, w.AbsentStreak = true, 0
					}
					list.Items[k] = w
					dirty = true
				}
			}
			return nil
		}}
	}
	// Drain already-scheduled child work before listing the root inventory
	// again. This preserves the bounded queue while avoiding a complete root
	// re-list for every Max-Children chunk. A continuously progressing child
	// cannot suppress parent discovery indefinitely: the root is forced at
	// least once per configured poll interval.
	now := p.now().UTC()
	rootScanned := !healthyWorkReady(list, now) || rootScanDue(list, now, time.Duration(p.conf.Request_Interval)*time.Second)
	var rootCont *hosted.Continuation
	var rootErr error
	rootPending := false
	rootCapacityBlocked := false
	if rootScanned {
		rootCont, rootErr = p.handleOne(ctx, rt, discover(p.conf))
		var syncErr *deliverySyncError
		if errors.As(rootErr, &syncErr) {
			return nil, rootErr
		}
		rootPending = rootCont != nil && rootCont.Delay == 0
		if rootErr == nil && len(list.Items) > 0 {
			list.LastRootScan = now
			dirty = true
		}
		if capacityBackpressure(rootErr) {
			rootCapacityBlocked = true
			rt.Warn("Compliance root discovery deferred because child-work capacity is full", log.KV("dataset", p.conf.dataset.Name))
			// The rejected page was neither written nor checkpointed. Restore
			// the last durable worklist before draining its existing children;
			// carrying the page's in-memory additions forward would defeat the
			// capacity deferral and could make a later persist inconsistent.
			if e = reload(); e != nil {
				return nil, e
			}
			rootPending = len(readyWorkKeys(list, p.now())) > 0
			rootErr = nil
		}
	} else if cp, cpErr := loadCheckpoint(rt, stanza); cpErr != nil {
		// Unknown state is never authorization. A checkpoint that cannot be read
		// cannot show that the root inventory was ever finished, so this is
		// propagated rather than treated as "no stored walk".
		return nil, cpErr
	} else if cp.Walk != nil {
		// Skipping the root is ordinarily just a deferral of an *already
		// complete* traversal while healthy discovered children drain, and that
		// alone must not hold back reclamation. A stored mid-cursor walk is the
		// opposite: the root inventory of this namespace stopped at Max-Pages or
		// the per-cycle record bound and has not been read to its end, so
		// children discovered from the pages it did read can all drain and leave
		// this cycle with nothing pending and no continuation of its own while
		// the replacement's own inventory is still unfinished. Reclaiming the
		// superseded namespace there would destroy a lower bound the rest of
		// that walk -- and reverting the credential -- still needs, so the
		// unfinished walk marks the shared proof incomplete (see Handle).
		p.sourceUse.observeIncomplete()
	}
	if e = persistOwned(); e != nil {
		return nil, e
	}
	// A stored work item whose parameter is oversized can never make
	// progress: its "_parent" value will permanently fail to encode as an
	// enumerated value. Retire it safely instead of letting it cycle through
	// RetryAt backoff forever. Validation now rejects an oversized configured
	// Parameter outright, so this is defensive handling of pre-release stored
	// state and carries no tombstone: an oversized value is not evidence that
	// the vendor deleted anything, and tombstoning it would suppress a
	// perfectly valid parent forever.
	for _, k := range sortedWorkKeys(list.Items) {
		w := list.Items[k]
		name, n, bad := oversizedParameter(w)
		if !bad {
			continue
		}
		rt.Warn("Compliance discovered work item has an oversized parameter; retiring", log.KV("dataset", w.Dataset), log.KV("parameter", name), log.KV("length", n))
		// Only a stored state key can be reclaimed; an item that never owned a
		// namespace simply leaves the queue.
		if e = stage(k, w, false); e != nil {
			return nil, e
		}
		delete(list.Items, k)
		dirty = true
	}
	// A work item from a removed or renamed catalog entry cannot ever bind.
	// Retire only those permanent catalog mismatches. childConfig also verifies
	// credentials and the rest of the bound configuration; a transient failure
	// there must preserve the work item and its checkpoint for a later cycle.
	for _, k := range sortedWorkKeys(list.Items) {
		w := list.Items[k]
		ce := childCatalogStatus(p.conf, w)
		if ce == nil {
			continue
		}
		if !errors.Is(ce, errUnknownChild) && !errors.Is(ce, errCrossFamilyChild) {
			continue
		}
		rt.Warn("Compliance stored child work no longer matches the catalog; retiring", log.KV("dataset", w.Dataset))
		if e = stage(k, w, false); e != nil {
			return nil, e
		}
		delete(list.Items, k)
		dirty = true
	}
	if e = persistOwned(); e != nil {
		return nil, e
	}
	// Bounded absence-based retirement: only for parent kinds with no vendor
	// deletion signal, and only when this cycle's root scan was a genuinely
	// complete, unwindowed pass -- never for a partial Max-Pages/record-limit
	// continuation, a request/rate-limit/cancellation error, or a malformed
	// page. rootCont is nil on every error path out of handleOne (including
	// the capacity conversion above, which leaves rootCont untouched), and
	// rootCont.Delay is 0 for every chunked-continuation path, so this
	// condition can only be true after a full, clean pass.
	if rootScanned && rootErr == nil && rootCont != nil && rootCont.Delay != 0 && absenceTrackedParents[p.conf.dataset.Name] {
		retire := func(w work) error {
			return stage(w.Dataset+"/"+strings.Join(w.Parameter, "/"), w, false)
		}
		if e = retireAbsentChildren(p.conf, &list, &dirty, retire); e != nil {
			return nil, e
		}
		if e = persistOwned(); e != nil {
			return nil, e
		}
	}
	keys := readyWorkKeys(list, p.now())
	for i, k := range keys {
		if i >= p.conf.Max_Children {
			break
		}
		protected[k] = struct{}{}
	}
	var errs []error
	if rootErr != nil {
		errs = append(errs, rootErr)
	}
	for i, k := range keys {
		if i >= p.conf.Max_Children {
			break
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		w, exists := list.Items[k]
		if !exists {
			continue
		}
		if w.StateKey == "" {
			if e = bindSlot(&w); e != nil {
				return nil, e
			}
			list.Items[k] = w
			dirty = true
		}
		childConf, ce := childConfig(p.conf, w)
		if ce != nil {
			return nil, ce
		}
		c := *childConf
		// Reserve this child's manifest allowance in the durable worklist
		// before its checkpoint can grow. A crash can therefore leave only an
		// overestimate, never an unaccounted manifest. Steady-state
		// reconciliation gives every child a fair share under pressure; the
		// active child may use otherwise-free capacity, and that reservation
		// survives reconciliation rather than being trimmed back before the
		// request it was made for even runs.
		accounted := 0
		for _, item := range list.Items {
			accounted += item.ManifestEntries
		}
		free := maxAggregateManifestEntries - (accounted - w.ManifestEntries)
		share := max(1, maxAggregateManifestEntries/max(1, len(list.Items)))
		// The floor is one page, or the equal share when that is larger. A
		// *measured* child additionally gets one page beyond what it was last
		// measured to retain: a resource larger than one page can only become
		// completely remembered if its allowance grows, and a fixed
		// one-page-or-share reservation left such a child forgetting most of
		// itself on every run and re-delivering the remainder on every
		// unwindowed refresh forever. Each completed run replaces the
		// reservation with the count actually retained, so this converges on the
		// child's real identity count and stops there instead of inflating
		// without limit. Growth is only ever granted out of genuinely free
		// aggregate capacity, so the stanza-wide budget still holds at every
		// persisted boundary, and the per-dataset ceiling still bounds one
		// child. An unmeasured child keeps whatever overestimate a crash left
		// it: that is the safe direction, and reconciliation above has already
		// measured every child the stored worklist held.
		target := max(w.ManifestEntries, max(c.Page_Size, share))
		if w.ManifestMeasured {
			target = max(target, w.ManifestEntries+c.Page_Size)
		}
		reservation := min(defaultMaxManifestEntries, max(1, min(free, target)))
		w.ManifestEntries = reservation
		w.ManifestMeasured = true
		list.Items[k] = w
		dirty = true
		if e = reconcileManifestBudget(rt, &list, &dirty, maxAggregateManifestEntries, k); e != nil {
			return nil, e
		}
		w = list.Items[k]
		if e = persistMutation(); e != nil {
			return nil, e
		}
		child := *p
		child.conf = &c
		child.maxManifestEntries = max(1, w.ManifestEntries)
		w.LastAttempt = p.now().UTC()
		childCont, childErr := child.handleOne(ctx, rt, discover(&c))
		var syncErr *deliverySyncError
		if errors.As(childErr, &syncErr) {
			// The reservation was durably written before the request. Retain that
			// safe overestimate and the prior scheduling fields so the same child
			// is retried without rotating through the queue or multiplying the
			// in-memory delivery ledger across child prefixes.
			return nil, childErr
		}
		entries, countErr := checkpointManifestEntries(rt, w.StateKey)
		if countErr != nil {
			return nil, countErr
		}
		w.ManifestEntries = entries
		w.ManifestMeasured = true
		list.Items[k] = w
		dirty = true
		if e = reconcileManifestBudget(rt, &list, &dirty, maxAggregateManifestEntries, ""); e != nil {
			return nil, e
		}
		w = list.Items[k]
		if capacityBackpressure(childErr) {
			rt.Warn("Compliance child discovery deferred because child-work capacity is full", log.KV("dataset", w.Dataset))
			// As with a root-page deferral, discard only the uncommitted
			// discovery mutations. Earlier page boundaries are already in the
			// durable worklist and are recovered by reload.
			if e = reload(); e != nil {
				return nil, e
			}
			var ok bool
			if w, ok = list.Items[k]; !ok {
				rootPending = true
				continue
			}
			w.LastAttempt = p.now().UTC()
			w.Pending = true
			w.Failures = 0
			w.RetryAt = w.LastAttempt.Add(pendingRetryDelay(list, w.LastAttempt, time.Duration(p.conf.Request_Interval)*time.Second))
			list.Items[k] = w
			dirty = true
			if e = persistMutation(); e != nil {
				return nil, e
			}
			delete(protected, k)
			// Capacity cannot change until another child completes or reaches
			// its retry time. Back off this child instead of immediately
			// replaying the same page and rewriting the same worklist.
			continue
		}
		// Cancellation belongs to the runner lifecycle, not the child. Return
		// it without persisting a failure count or retry delay against an
		// otherwise healthy resource.
		if childErr != nil && ctx.Err() != nil {
			return nil, childErr
		}
		// A resource 404 is authoritative only after a successful root listing
		// in this same cycle, and only for a deleted parent's final drain. The
		// vendor also uses a bare 404 for unauthenticated requests, so a 404
		// while the root was skipped must remain a failure -- that failure forces
		// a root scan on the next cycle before the child can be completed or
		// tombstoned -- and an ordinary child's 404 stays a bounded-backoff
		// failure even after a clean root listing, because listing authorization
		// does not prove transcript authorization.
		if childErr != nil && rootScanned && rootErr == nil && w.RetireAfterDrain && httpStatus(childErr) == http.StatusNotFound {
			rt.Warn("Compliance deleted parent's child resource is gone; completing its final drain", log.KV("dataset", w.Dataset))
			childCont, childErr = p.conf.ContinueAfterInterval(), nil
		}
		if childErr != nil {
			w.Failures = min(w.Failures+1, maxChildFailures)
			w.RetryAt = w.LastAttempt.Add(time.Minute * time.Duration(1<<(w.Failures-1)))
			list.Items[k] = w
			dirty = true
			if pe := persistMutation(); pe != nil {
				return nil, pe
			}
			delete(protected, k)
			errs = append(errs, fmt.Errorf("pending %s: %w", w.Dataset, childErr))
			continue
		}
		// Any successful child request proves the resource is making forward
		// progress, even when Max-Pages or the record cap leaves a continuation.
		// Clear its old stuck classification before the next root scan can
		// consider it for capacity eviction.
		w.Failures = 0
		if childCont != nil && childCont.Delay == 0 {
			w.Pending = true
			w.RetryAt = time.Time{}
			list.Items[k] = w
			dirty = true
			rootPending = true
			if e = persistMutation(); e != nil {
				return nil, e
			}
			delete(protected, k)
			continue
		}
		if w.RetireAfterDrain {
			if e = stage(k, w, true); e != nil {
				return nil, e
			}
			delete(list.Items, k)
			dirty = true
			if e = persistMutation(); e != nil {
				return nil, e
			}
			delete(protected, k)
			continue
		}
		// A discovered parent owns the same absence evidence as a configured
		// parent once its traversal completes cleanly. Consume that evidence
		// before completing the work item so grandchildren that disappeared
		// from an authoritative inventory do not retain worklist and checkpoint
		// capacity forever. Continuations, failures and drain work all exit
		// above and therefore cannot advance absence retirement.
		if absenceTrackedParents[c.dataset.Name] {
			if e = retireAbsentChildren(&c, &list, &dirty, func(item work) error {
				return stage(item.Dataset+"/"+strings.Join(item.Parameter, "/"), item, false)
			}); e != nil {
				return nil, e
			}
		}
		w.Pending = false
		w.LastCompleted = p.now().UTC()
		w.RetryAt = time.Time{}
		w.Failures = 0
		list.Items[k] = w
		dirty = true
		if e = persistMutation(); e != nil {
			return nil, e
		}
		delete(protected, k)
	}
	// Whatever this cycle still owes is what stops it from authorizing
	// reclamation of the namespace a credential or origin change superseded. A
	// continuation left by the root or by a child, a page deferred for bounded
	// capacity, and a child persisted Pending with a future RetryAt are all the
	// same thing from the handover's point of view: the replacement has not yet
	// been shown able to collect the whole of what the superseded namespace
	// holds. Marking it here, rather than reading the worklist back inside
	// Handle, keeps the decision on the one list this cycle actually owns and
	// shares it through the proof every dataset and child copy already holds.
	if rootCapacityBlocked || rootPending || pendingWorkRemains(list) {
		p.sourceUse.observeIncomplete()
	}
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		if len(readyWorkKeys(list, p.now())) > 0 {
			// Preserve the error state for observability while allowing the runner
			// to drain another ready sibling after a bounded short delay. The failed
			// child has a durable RetryAt and will not be selected again until its
			// backoff ends.
			return nil, &deferredRemoteError{err: joined, delay: minimumDeferredRetry}
		}
		return nil, joined
	}
	if rootCapacityBlocked {
		if len(readyWorkKeys(list, p.now())) > 0 {
			return hosted.ContinueNow(), nil
		}
		return hosted.ContinueAfter(pendingRetryDelay(list, p.now(), time.Duration(p.conf.Request_Interval)*time.Second)), nil
	}
	return p.conf.PendingOrInterval(rootPending || len(readyWorkKeys(list, p.now())) > 0), nil
}

// deletedChildWork returns every child of a parent the vendor reports as
// deleted through deleted_at (chats and projects). The caller drains each
// child before compacting and tombstoning it under the deleted revision.
func deletedChildWork(d Dataset, inherited []string, raw []byte) ([]work, bool, error) {
	if d.Name != "chats" && d.Name != "projects" {
		return nil, false, nil
	}
	v, e := selectedFields(raw, "deleted_at", "id")
	if e != nil {
		return nil, false, e
	}
	deleted := v["deleted_at"]
	if len(deleted) == 0 || string(deleted) == "null" {
		return nil, false, nil
	}
	var id string
	_ = json.Unmarshal(v["id"], &id)
	if id == "" {
		return nil, false, fmt.Errorf("%w: %s parent missing id", errMissingParentID, d.Name)
	}
	if len(id) > maxDiscoveredParameterLen {
		return nil, false, fmt.Errorf("%w: %s id is %d bytes", errOversizedIdentity, d.Name, len(id))
	}
	if invalidOpaqueID(id) {
		return nil, false, fmt.Errorf("%w: %s id", errInvalidIdentity, d.Name)
	}
	h := sha256.Sum256(raw)
	var result []work
	for _, s := range childSpecs(d.Name) {
		params := slices.Concat(inherited, []string{s.param + ":" + id})
		sort.Strings(params)
		result = append(result, work{Dataset: s.name, Parameter: params, Revision: fmt.Sprintf("%x", h)})
	}
	return result, true, nil
}

// jsonString returns a top-level string field of a compact record, or "".
func jsonString(raw []byte, field string) string {
	var s string
	if v, err := selectedFields(raw, field); err == nil {
		_ = json.Unmarshal(v[field], &s)
	}
	return s
}

func childConfig(parent *Config, w work) (*Config, error) {
	if err := childCatalogStatus(parent, w); err != nil {
		return nil, err
	}
	c := *parent
	c.Parameter = append([]string(nil), w.Parameter...)
	c.parentContext = append(append([]string(nil), parent.parentContext...), parameterDifference(w.Parameter, parent.Parameter)...)
	c.Follow_Children = "disabled"
	// The owning root allocated and durably recorded this finite checkpoint
	// slot before scheduling the child. Carry it through bind so the child can
	// never derive a namespace from its vendor-supplied identity.
	c.discovered = true
	c.stateKey = w.StateKey
	return c.bind(w.Dataset)
}

func childCatalogStatus(parent *Config, w work) error {
	d, ok := Datasets[w.Dataset]
	if !ok {
		return errUnknownChild
	}
	if d.Tag != parent.dataset.Tag {
		return errCrossFamilyChild
	}
	return nil
}

// parameterDifference returns the entries added by a discovered child while
// preserving their stable order. Parent entries may include operator-supplied
// path parameters; those remain request inputs but are not discovery context.
func parameterDifference(all, parent []string) []string {
	remaining := make(map[string]int, len(parent))
	for _, p := range parent {
		remaining[p]++
	}
	var result []string
	for _, p := range all {
		if remaining[p] > 0 {
			remaining[p]--
			continue
		}
		result = append(result, p)
	}
	return result
}

// spec names a child dataset a parent record can spawn, and the parameter
// key its own identity is inherited under.
type spec struct{ name, param string }

// childSpecs returns the child dataset specs a parent dataset kind can
// produce. It depends only on the parent dataset's name, not on any single
// record's content, so it also serves as the family membership table used
// by retireAbsentChildren to recognize direct children of a given parent.
func childSpecs(name string) []spec {
	switch name {
	case "organizations":
		return []spec{{"organization-users", "organization_id"}, {"organization-roles", "organization_id"}}
	case "organization-roles":
		return []spec{{"role-permissions", "role_id"}}
	case "groups":
		return []spec{{"group-members", "group_id"}}
	case "chats":
		return []spec{{"chat-messages", "chat_id"}}
	case "projects":
		return []spec{{"project-attachments", "project_id"}, {"project-collaborators", "project_id"}}
	case "local-sessions":
		return []spec{{"local-session-messages", "session_id"}}
	case "remote-sessions":
		return []spec{{"remote-session-messages", "session_id"}}
	}
	return nil
}

// childParents is the inverse of childSpecs: it maps each discovered child
// dataset to the one parent dataset the catalog lets produce it and to the
// parameter key the child inherits from that parent. Because the relationship
// is one-to-one in that direction, a child's immediate parent work item is
// reconstructable from the child's own parameters alone.
var childParents = func() map[string]spec {
	parents := map[string]spec{}
	for name := range Datasets {
		for _, s := range childSpecs(name) {
			parents[s.name] = spec{name: name, param: s.param}
		}
	}
	return parents
}()

// parentWorkKey returns the worklist key of w's immediate catalog parent. It
// removes exactly the parameter w inherited from that parent, which leaves the
// parent's own parameter set, so the result names one specific parent identity
// and never a sibling under a different one.
func parentWorkKey(w work) (string, bool) {
	rel, ok := childParents[w.Dataset]
	if !ok {
		return "", false
	}
	params := make([]string, 0, len(w.Parameter))
	removed := false
	for _, p := range w.Parameter {
		if k, _, _ := strings.Cut(p, ":"); !removed && k == rel.param {
			removed = true
			continue
		}
		params = append(params, p)
	}
	if !removed {
		return "", false
	}
	return rel.name + "/" + strings.Join(params, "/"), true
}

// deeperDescendants returns every dataset reachable from name through the
// catalog relationship below its own direct children. A direct child is
// anchored by the root scan itself, which never appears in the worklist;
// anything deeper is anchored by a work item that does.
func deeperDescendants(name string) map[string]bool {
	out := map[string]bool{}
	frontier := make([]string, 0, 4)
	for _, s := range childSpecs(name) {
		frontier = append(frontier, s.name)
	}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		for _, s := range childSpecs(current) {
			if out[s.name] {
				continue
			}
			out[s.name] = true
			frontier = append(frontier, s.name)
		}
	}
	return out
}

// carriesAll reports whether all contains every entry of required.
func carriesAll(all, required []string) bool {
	remaining := make(map[string]int, len(required))
	for _, p := range required {
		remaining[p]++
	}
	for _, p := range all {
		if remaining[p] > 0 {
			remaining[p]--
		}
	}
	for _, n := range remaining {
		if n > 0 {
			return false
		}
	}
	return true
}

func childWork(d Dataset, inherited []string, raw []byte) ([]work, error) {
	specs := childSpecs(d.Name)
	if len(specs) == 0 {
		return nil, nil
	}
	idKey := "id"
	if d.Name == "organizations" {
		idKey = "uuid"
	}
	v, e := selectedFields(raw, idKey, "deleted_at", "status")
	if e != nil {
		return nil, e
	}
	var id string
	_ = json.Unmarshal(v[idKey], &id)
	if d.Name == "chats" || d.Name == "projects" {
		if s := v["deleted_at"]; len(s) > 0 && string(s) != "null" {
			return nil, nil
		}
	}
	// A pending remote session has no transcript yet and its messages
	// endpoint returns 404; its revision changes when it starts.
	if d.Name == "remote-sessions" && jsonString(raw, "status") == "pending" {
		return nil, nil
	}
	if id == "" {
		return nil, fmt.Errorf("%w: %s parent missing %s", errMissingParentID, d.Name, idKey)
	}
	if len(id) > maxDiscoveredParameterLen {
		return nil, fmt.Errorf("%w: %s %s is %d bytes", errOversizedIdentity, d.Name, idKey, len(id))
	}
	if invalidOpaqueID(id) {
		return nil, fmt.Errorf("%w: %s %s", errInvalidIdentity, d.Name, idKey)
	}
	var result []work
	h := sha256.Sum256(raw)
	for _, s := range specs {
		params := slices.Concat(inherited, []string{s.param + ":" + id})
		sort.Strings(params)
		result = append(result, work{Dataset: s.name, Parameter: params, Revision: fmt.Sprintf("%x", h), Pending: true})
	}
	return result, nil
}

// oversizedParameter reports the first Parameter entry of w whose value
// exceeds maxDiscoveredParameterLen, identified only by its parameter name
// and measured length -- never its value -- so callers can log and retire
// it safely.
func oversizedParameter(w work) (name string, length int, bad bool) {
	for _, p := range w.Parameter {
		k, v, ok := strings.Cut(p, ":")
		if ok && len(v) > maxDiscoveredParameterLen {
			return k, len(v), true
		}
	}
	return "", 0, false
}

// isDirectChildOf reports whether w is exactly one discovery level below
// parent: its dataset matches one of parent's child specs, and its
// Parameter set is parent's own Parameter plus exactly one more entry.
func isDirectChildOf(parent *Config, specs []spec, w work) bool {
	for _, s := range specs {
		if w.Dataset != s.name || len(w.Parameter) != len(parent.Parameter)+1 {
			continue
		}
		if carriesAll(w.Parameter, parent.Parameter) {
			return true
		}
	}
	return false
}

// retireAbsentChildren runs only after handleOne reports that this cycle's
// scan of an absence-tracked parent dataset was a complete, unwindowed,
// error-free pass (see the call site in Handle). Every existing direct
// child of parent that was touched during that pass (SeenThisScan) has its
// AbsentStreak reset to zero; every one that was not accrues one miss. A
// child missed on absentRetirementThreshold consecutive complete passes is
// retired through the durable cleanup path, but without a tombstone:
// absence is not a content-derived signal, so an unchanged parent could
// reappear under the very same revision, and tombstoning it would suppress a
// still-valid parent forever.
//
// Work already committed to the drain path is excluded outright. A parent the
// vendor reported deleted hands its child to RetireAfterDrain, and that path is
// the single owner of the child's end of life: it finishes the traversal
// already in flight (or accepts an authoritative 404), and only then compacts
// the checkpoint *and* writes the deletion tombstone that stops the deleted
// parent from being rediscovered. Absence retirement deliberately writes no
// tombstone, because absence is not content-derived -- so letting it reach
// drain work would substitute the weaker retirement for the stronger one and
// discard the confirmed-deletion evidence on nothing more than the parent
// having also dropped out of the listing. Such work stays bounded without this
// pass: it drains, it takes an authoritative 404, or it exhausts bounded
// retries and becomes eligible for the stuck-pending capacity fallback (see
// capacityVictim). Its SeenThisScan and AbsentStreak are left untouched here
// and are reset by discovery if the parent ever returns as a live record.
//
// Retirement then cascades. A grandchild is discovered only while its own
// parent work item is being collected, so a descendant whose immediate parent
// the worklist no longer owns can never be refreshed or re-confirmed: it would
// otherwise back off against a 404 forever and hold a bounded queue slot.
// Because the anchor is the exact parameter set of that one parent identity,
// an unrelated sibling under a different parent keeps its work untouched. The
// cascade is anchored on ownership rather than on absence counting, and no
// dataset that can carry a deletion tombstone has a descendant below its own
// children (see childSpecs), so it never reaches drain work either.
func retireAbsentChildren(parent *Config, list *worklist, dirty *bool, retire func(work) error) error {
	specs := childSpecs(parent.dataset.Name)
	if len(specs) == 0 {
		return nil
	}
	for _, k := range sortedWorkKeys(list.Items) {
		w := list.Items[k]
		if !isDirectChildOf(parent, specs, w) || w.RetireAfterDrain {
			continue
		}
		if w.SeenThisScan {
			w.SeenThisScan, w.AbsentStreak = false, 0
			list.Items[k] = w
			*dirty = true
			continue
		}
		w.AbsentStreak++
		if w.AbsentStreak < absentRetirementThreshold {
			list.Items[k] = w
			*dirty = true
			continue
		}
		if e := retire(w); e != nil {
			return e
		}
		delete(list.Items, k)
		*dirty = true
	}
	deeper := deeperDescendants(parent.dataset.Name)
	if len(deeper) == 0 {
		return nil
	}
	for _, k := range sortedWorkKeys(list.Items) {
		w := list.Items[k]
		if !deeper[w.Dataset] || len(w.Parameter) <= len(parent.Parameter) ||
			!carriesAll(w.Parameter, parent.Parameter) {
			continue
		}
		owner, resolvable := parentWorkKey(w)
		if !resolvable {
			continue
		}
		if _, owned := list.Items[owner]; owned {
			continue
		}
		if e := retire(w); e != nil {
			return e
		}
		delete(list.Items, k)
		*dirty = true
	}
	return nil
}
