package claudecompliance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
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
	Dataset       string
	Parameter     []string
	Revision      string
	StateKey      string `json:",omitempty"`
	Pending       bool
	LastCompleted time.Time
	LastAttempt   time.Time
	RetryAt       time.Time
	Failures      uint
	// RetireAfterDrain keeps a deleted parent's child readable until an
	// already-started (or final best-effort) traversal completes or returns
	// 404. Only then is its checkpoint compacted and tombstoned.
	RetireAfterDrain bool `json:",omitempty"`
	// SeenThisScan and AbsentStreak support bounded retirement of child work
	// whose parent kind has no vendor deletion signal (see
	// absenceTrackedParents). Their zero values -- never seen, never missed --
	// are the correct starting state for a newly discovered item.
	SeenThisScan bool `json:",omitempty"`
	AbsentStreak uint `json:",omitempty"`
}
type worklist struct{ Items map[string]work }

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

var (
	errPendingCapacity   = errors.New("Compliance pending child-work capacity reached")
	errPendingBytes      = errors.New("Compliance pending child-work byte capacity reached")
	errMissingParentID   = errors.New("Compliance parent identity is missing")
	errOversizedIdentity = errors.New("Compliance discovered identity exceeds safe parameter length")
	errInvalidIdentity   = errors.New("Compliance discovered identity contains an invalid character")
	errUnknownChild      = errors.New("unknown Compliance child dataset")
	errCrossFamilyChild  = errors.New("Compliance cross-family discovery requires an explicit tag mapping")
)

// maxDiscoveredParameterLen bounds a vendor-supplied id/uuid before it can
// become a child Parameter value or a persisted worklist identity. Real
// vendor identities are short opaque tokens; this is a defensive ceiling far
// below entry.MaxEvDataLength (the hard limit on a single enumerated value),
// so a pathological or corrupted vendor id cannot make the derived "_parent"
// enumerated value fail to encode.
const (
	maxDiscoveredParameterLen = 512
	maxPersistedWorklistBytes = 32 << 20
)

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
func reconcileWorklistCapacity(rt hosted.Runtime, parent *Config, list *worklist, protected map[string]struct{}, dirty *bool) error {
	for len(list.Items) > parent.Max_Pending {
		victim := capacityVictim(*list, protected)
		if victim == "" {
			return nil
		}
		if err := pruneCheckpoint(rt, parent, list.Items[victim], false); err != nil {
			return err
		}
		delete(list.Items, victim)
		*dirty = true
	}
	return nil
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
// hourly, and nested parents such as "organization-roles", which are only
// ever visited as a discovered child and never as the root scan whose
// completion this package can observe.
var absenceTrackedParents = map[string]bool{
	"organizations":  true,
	"groups":         true,
	"projects":       true,
	"local-sessions": true,
}

// Handle runs one collection cycle for every dataset this stanza selects.
// Each is collected against its own bound config and its own checkpoint, so
// one stanza can cover a whole family without a config stanza per endpoint.
func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
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
			errs = append(errs, fmt.Errorf("dataset %s: %w", name, e))
			continue
		}
		pending = pending || (cont != nil && cont.Delay == 0)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return p.conf.PendingOrInterval(pending), nil
}

func (p *Plugin) handleDataset(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if p.conf.Follow_Children == "disabled" {
		return p.handleOne(ctx, rt, nil)
	}
	key := p.conf.key() + keyChildren
	list := worklist{Items: map[string]work{}}
	b, e := rt.Get(key)
	if e == nil {
		if e = json.Unmarshal(b, &list); e != nil || list.Items == nil {
			return nil, errors.New("invalid Compliance child-work state")
		}
	} else if !errors.Is(e, storage.ErrStorageNotFound) {
		return nil, e
	}
	dirty := false
	reload := func() error {
		stored := worklist{Items: map[string]work{}}
		b, e := rt.Get(key)
		if e == nil {
			if e = json.Unmarshal(b, &stored); e != nil || stored.Items == nil {
				return errors.New("invalid Compliance child-work state")
			}
		} else if !errors.Is(e, storage.ErrStorageNotFound) {
			return e
		}
		list = stored
		dirty = false
		return nil
	}
	// A child can discover grandchildren while this Handle call is consuming
	// an already-sorted worklist. Protect the keys selected for this cycle so
	// capacity eviction cannot invalidate the schedule or delete the item that
	// the outer loop will later write back.
	protected := map[string]struct{}{}
	persist := func() error {
		if !dirty {
			return nil
		}
		candidate := worklist{Items: make(map[string]work, len(list.Items))}
		for k, w := range list.Items {
			candidate.Items[k] = w
		}
		var pruned []work
		for {
			b, e := json.Marshal(candidate)
			if e != nil {
				return e
			}
			if len(b) <= maxPersistedWorklistBytes {
				if e = rt.Put(key, b); e != nil {
					return e
				}
				list = candidate
				dirty = false
				// The worklist is the ownership record for child checkpoints.
				// Persist its removals first, then compact those now-orphaned
				// checkpoints. A failed worklist write therefore leaves both the
				// stored list and every checkpoint untouched.
				for _, item := range pruned {
					if e = pruneCheckpoint(rt, p.conf, item, false); e != nil {
						return e
					}
				}
				return nil
			}

			// Max-Pending bounds the number of work items, while this bound
			// limits their serialized bytes. Vendor identifiers can make the
			// byte bound tighter. Shed the same safe victims used for capacity
			// eviction until the state fits, compacting their checkpoints
			// without tombstones so unchanged parents may rediscover them.
			remaining := len(b) - maxPersistedWorklistBytes
			for remaining > 0 {
				victim := capacityVictim(candidate, protected)
				if victim == "" {
					return errPendingBytes
				}
				item := candidate.Items[victim]
				encoded, e := json.Marshal(map[string]work{victim: item})
				if e != nil {
					return e
				}
				removed := len(encoded) - 2
				if len(candidate.Items) > 1 {
					removed++ // comma separating this item from another map entry
				}
				pruned = append(pruned, item)
				delete(candidate.Items, victim)
				remaining -= removed
			}
		}
	}
	if e = reconcileWorklistCapacity(rt, p.conf, &list, protected, &dirty); e != nil {
		return nil, e
	}
	if e = persist(); e != nil {
		return nil, e
	}
	discover := func(parent *Config) collector {
		return collectorFuncs{flushFn: persist, recordFn: func(d Dataset, raw []byte) error {
			if retiredItems, ok, e := deletedChildWork(d, parent.Parameter, raw); e != nil {
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
					if retiring.StateKey == "" {
						child, ce := childConfig(parent, retiring)
						if ce != nil {
							return ce
						}
						retiring.StateKey = child.key()
					}
					if !exists {
						retired, ce := retiredCheckpoint(rt, retiring.StateKey, retiring.Revision)
						if ce != nil {
							return ce
						}
						if retired {
							continue
						}
						if len(list.Items) >= p.conf.Max_Pending {
							victim := capacityVictim(list, protected)
							if victim == "" {
								return errPendingCapacity
							}
							if e = pruneCheckpoint(rt, parent, list.Items[victim], false); e != nil {
								return e
							}
							delete(list.Items, victim)
						}
						retiring.LastAttempt = p.now().UTC()
					} else {
						if old.StateKey != "" {
							retiring.StateKey = old.StateKey
						}
						retiring.LastCompleted = old.LastCompleted
						retiring.LastAttempt = old.LastAttempt
						retiring.RetryAt = old.RetryAt
						retiring.Failures = old.Failures
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
				if w.StateKey == "" {
					child, e := childConfig(parent, w)
					if e != nil {
						return e
					}
					w.StateKey = child.key()
				}
				if !exists {
					retired, e := retiredCheckpoint(rt, w.StateKey, w.Revision)
					if e != nil {
						return e
					}
					if retired {
						continue
					}
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
					if e = pruneCheckpoint(rt, parent, list.Items[victim], false); e != nil {
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
	// re-list for every Max-Children chunk. A root with no ready child work is
	// still polled normally, including while failed children wait in backoff.
	rootScanned := !healthyWorkReady(list, p.now())
	var rootCont *hosted.Continuation
	var rootErr error
	rootPending := false
	if rootScanned {
		rootCont, rootErr = p.handleOne(ctx, rt, discover(p.conf))
		rootPending = rootCont != nil && rootCont.Delay == 0
		if errors.Is(rootErr, errPendingBytes) {
			// The rejected page was neither written nor checkpointed. Restore
			// the last durable worklist before draining its existing children;
			// carrying the page's in-memory additions forward would defeat the
			// capacity deferral and could make a later persist inconsistent.
			if e = reload(); e != nil {
				return nil, e
			}
			rootPending = len(readyWorkKeys(list, p.now())) > 0
			rootErr = nil
		} else if errors.Is(rootErr, errPendingCapacity) {
			rootPending, rootErr = true, nil
		}
	}
	if e = persist(); e != nil {
		return nil, e
	}
	// A stored work item whose parameter is oversized can never make
	// progress: its "_parent" value will permanently fail to encode as an
	// enumerated value. Retire it safely instead of letting it cycle through
	// RetryAt backoff forever.
	for k, w := range list.Items {
		name, n, bad := oversizedParameter(w)
		if !bad {
			continue
		}
		rt.Warn("Compliance discovered work item has an oversized parameter; retiring", log.KV("dataset", w.Dataset), log.KV("parameter", name), log.KV("length", n))
		if e = pruneCheckpoint(rt, p.conf, w, true); e != nil {
			return nil, e
		}
		delete(list.Items, k)
		dirty = true
	}
	// A work item from a removed or renamed catalog entry cannot ever bind.
	// Retire only those permanent catalog mismatches. childConfig also verifies
	// credentials and the rest of the bound configuration; a transient failure
	// there must preserve the work item and its checkpoint for a later cycle.
	for k, w := range list.Items {
		ce := childCatalogStatus(p.conf, w)
		if ce == nil {
			continue
		}
		if !errors.Is(ce, errUnknownChild) && !errors.Is(ce, errCrossFamilyChild) {
			continue
		}
		rt.Warn("Compliance stored child work no longer matches the catalog; retiring", log.KV("dataset", w.Dataset))
		if w.StateKey != "" {
			if e = compact(rt, w.StateKey, ""); e != nil {
				return nil, e
			}
		}
		delete(list.Items, k)
		dirty = true
	}
	if e = persist(); e != nil {
		return nil, e
	}
	// Bounded absence-based retirement: only for parent kinds with no vendor
	// deletion signal, and only when this cycle's root scan was a genuinely
	// complete, unwindowed pass -- never for a partial Max-Pages/record-limit
	// continuation, a request/rate-limit/cancellation error, or a malformed
	// page. rootCont is nil on every error path out of handleOne (including
	// the errPendingCapacity conversion above, which leaves rootCont
	// untouched), and rootCont.Delay is 0 for every chunked-continuation
	// path, so this condition can only be true after a full, clean pass.
	if rootScanned && rootErr == nil && rootCont != nil && rootCont.Delay != 0 && absenceTrackedParents[p.conf.dataset.Name] {
		if e = retireAbsentChildren(rt, p.conf, &list, &dirty); e != nil {
			return nil, e
		}
		if e = persist(); e != nil {
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
		childConf, ce := childConfig(p.conf, w)
		if ce != nil {
			return nil, ce
		}
		c := *childConf
		if w.StateKey == "" {
			w.StateKey = c.key()
		}
		child := *p
		child.conf = &c
		w.LastAttempt = p.now().UTC()
		childCont, childErr := child.handleOne(ctx, rt, discover(&c))
		if errors.Is(childErr, errPendingBytes) {
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
			childCont, childErr = hosted.ContinueNow(), nil
		} else if errors.Is(childErr, errPendingCapacity) {
			childCont, childErr = hosted.ContinueNow(), nil
		}
		// Cancellation belongs to the runner lifecycle, not the child. Return
		// it without persisting a failure count or retry delay against an
		// otherwise healthy resource.
		if childErr != nil && ctx.Err() != nil {
			return nil, childErr
		}
		// A resource 404 after a successful root listing, or while draining
		// work from such a listing without re-listing the root, means the
		// vendor has removed it (deleted, retained out, or a remote session
		// that is not yet running). Complete it rather than fail it, so it is
		// requeued only by a parent revision change or a later refresh. A 404
		// remains a failure when this cycle also attempted and failed the root.
		if childErr != nil && (!rootScanned || rootErr == nil) && httpStatus(childErr) == http.StatusNotFound {
			rt.Warn("Compliance child resource was not found; marking it complete until its parent changes", log.KV("dataset", w.Dataset))
			childCont, childErr = p.conf.ContinueAfterInterval(), nil
		}
		if childErr != nil {
			w.Failures = min(w.Failures+1, maxChildFailures)
			w.RetryAt = w.LastAttempt.Add(time.Minute * time.Duration(1<<(w.Failures-1)))
			list.Items[k] = w
			dirty = true
			if pe := persist(); pe != nil {
				return nil, pe
			}
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
			if e = persist(); e != nil {
				return nil, e
			}
			continue
		}
		if w.RetireAfterDrain {
			if e = pruneCheckpoint(rt, p.conf, w, true); e != nil {
				return nil, e
			}
			delete(list.Items, k)
			dirty = true
			if e = persist(); e != nil {
				return nil, e
			}
			continue
		}
		w.Pending = false
		w.LastCompleted = p.now().UTC()
		w.RetryAt = time.Time{}
		w.Failures = 0
		list.Items[k] = w
		dirty = true
		if e = persist(); e != nil {
			return nil, e
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
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
	var v map[string]json.RawMessage
	if e := json.Unmarshal(raw, &v); e != nil {
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
	if strings.ContainsAny(id, "/\\?#") {
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
	var v map[string]json.RawMessage
	var s string
	if json.Unmarshal(raw, &v) == nil {
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
	c.discovered = true
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

func retiredCheckpoint(rt hosted.Runtime, prefix, revision string) (bool, error) {
	retired, e := getString(rt, prefix+keyRetired)
	if e != nil {
		return false, e
	}
	return retired != "" && retired == revision, nil
}

// pruneCheckpoint always compacts a child's checkpoint (clearing Manifest
// and Traversal to reclaim space) and, when tombstone is true, additionally
// marks it Retired under the current revision. The tombstone must be
// reserved for a signal that is actually tied to the parent's content --
// today, only confirmed vendor deletion (a deleted_at flip changes the raw
// JSON, and therefore the sha256 revision, so an undelete is guaranteed to
// present a different revision and bypass the tombstone). Max-Pending
// capacity eviction and absence-based retirement remove a child for reasons
// unrelated to the parent's content, so an unchanged parent would reappear
// with the *same* revision; tombstoning there would be indistinguishable
// from "still deleted" and would suppress a valid parent forever.
func pruneCheckpoint(rt hosted.Runtime, parent *Config, w work, tombstone bool) error {
	key := w.StateKey
	if key == "" {
		c, e := childConfig(parent, w)
		if e != nil {
			return e
		}
		key = c.key()
	}
	retired := ""
	if tombstone {
		retired = w.Revision
	}
	return compact(rt, key, retired)
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

func childWork(d Dataset, inherited []string, raw []byte) ([]work, error) {
	var v map[string]json.RawMessage
	if e := json.Unmarshal(raw, &v); e != nil {
		return nil, e
	}
	idKey := "id"
	if d.Name == "organizations" {
		idKey = "uuid"
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
	specs := childSpecs(d.Name)
	if len(specs) == 0 {
		return nil, nil
	}
	if id == "" {
		return nil, fmt.Errorf("%w: %s parent missing %s", errMissingParentID, d.Name, idKey)
	}
	if len(id) > maxDiscoveredParameterLen {
		return nil, fmt.Errorf("%w: %s %s is %d bytes", errOversizedIdentity, d.Name, idKey, len(id))
	}
	if strings.ContainsAny(id, "/\\?#") {
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
		matched := 0
		for _, pp := range parent.Parameter {
			for _, wp := range w.Parameter {
				if wp == pp {
					matched++
					break
				}
			}
		}
		if matched == len(parent.Parameter) {
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
// child missed on absentRetirementThreshold consecutive complete passes has
// its checkpoint compacted through the same pruning path used for an
// explicitly deleted chat, but without a tombstone: absence is not a
// content-derived signal, so an unchanged parent could reappear under the
// very same revision, and tombstoning it would suppress a still-valid
// parent forever.
func retireAbsentChildren(rt hosted.Runtime, parent *Config, list *worklist, dirty *bool) error {
	specs := childSpecs(parent.dataset.Name)
	if len(specs) == 0 {
		return nil
	}
	for k, w := range list.Items {
		if !isDirectChildOf(parent, specs, w) {
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
		if e := pruneCheckpoint(rt, parent, w, false); e != nil {
			return e
		}
		delete(list.Items, k)
		*dirty = true
	}
	return nil
}
