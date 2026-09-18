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
	"golang.org/x/time/rate"
	"sort"
	"strings"
	"sync"
	"time"
)

var budgets = struct {
	sync.Mutex
	m map[string]*rate.Limiter
}{m: map[string]*rate.Limiter{}}

func sharedLimiter(scope string, rpm int) *rate.Limiter {
	if rpm < 1 {
		rpm = 30
	}
	budgets.Lock()
	defer budgets.Unlock()
	limit := rate.Every(time.Minute / time.Duration(rpm))
	if l := budgets.m[scope]; l != nil {
		if limit < l.Limit() {
			l.SetLimit(limit)
		}
		return l
	}
	l := rate.NewLimiter(limit, 1)
	budgets.m[scope] = l
	return l
}

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
	// SeenThisScan and AbsentStreak support bounded retirement of child work
	// whose parent kind has no vendor deletion signal (see
	// absenceTrackedParents). Both are zero-valued (and therefore absent from
	// the wire encoding) on any work item persisted before this existed, which
	// is exactly the correct starting state: never yet confirmed absent.
	SeenThisScan bool `json:",omitempty"`
	AbsentStreak uint `json:",omitempty"`
}
type worklist struct{ Items map[string]work }

var (
	errPendingCapacity   = errors.New("Compliance pending child-work capacity reached")
	errMissingParentID   = errors.New("Compliance parent identity is missing")
	errOversizedIdentity = errors.New("Compliance discovered identity exceeds safe parameter length")
)

// maxDiscoveredParameterLen bounds a vendor-supplied id/uuid before it can
// become a child Parameter value or a persisted worklist identity. Real
// vendor identities are short opaque tokens; this is a defensive ceiling far
// below entry.MaxEvDataLength (the hard limit on a single enumerated value),
// so a pathological or corrupted vendor id cannot make the derived "_parent"
// enumerated value fail to encode.
const maxDiscoveredParameterLen = 512

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
// This list intentionally excludes "remote-sessions" (refreshed every poll
// rather than hourly) and nested parents such as "organization-roles" (only
// ever visited as a discovered child, never as the root scan whose
// completion this package can observe): both are out of scope for this
// repair.
var absenceTrackedParents = map[string]bool{
	"organizations":  true,
	"groups":         true,
	"projects":       true,
	"local-sessions": true,
}

func (p *Plugin) Handle(ctx context.Context, rt hosted.Runtime) (*hosted.Continuation, error) {
	if p.conf.Follow_Children == "disabled" {
		return p.handleOne(ctx, rt)
	}
	key := p.conf.key() + "/child-work-v1"
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
	persist := func() error {
		if !dirty {
			return nil
		}
		b, e := json.Marshal(list)
		if e != nil {
			return e
		}
		if len(b) > 32<<20 {
			return errors.New("child worklist exceeds size bound")
		}
		if e = rt.Put(key, b); e != nil {
			return e
		}
		dirty = false
		return nil
	}
	discover := func(parent *Config) func(Dataset, []byte) error {
		return func(d Dataset, raw []byte) error {
			if retired, ok, e := deletedChildWork(d, parent.Parameter, raw); e != nil {
				if errors.Is(e, errMissingParentID) {
					rt.Warn("Compliance deleted parent is missing its child-discovery identity; child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				if errors.Is(e, errOversizedIdentity) {
					rt.Warn("Compliance deleted parent identity exceeds safe parameter length; child checkpoint cannot be compacted", log.KV("dataset", d.Name))
					return nil
				}
				return e
			} else if ok {
				k := retired.Dataset + "/" + strings.Join(retired.Parameter, "/")
				if old, exists := list.Items[k]; exists {
					retired.LastCompleted = old.LastCompleted
					retired.StateKey = old.StateKey
				}
				if e = pruneCheckpoint(rt, parent, retired, true); e != nil {
					return e
				}
				delete(list.Items, k)
				dirty = true
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
				return e
			}
			tracked := absenceTrackedParents[d.Name]
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
					victim := ""
					for candidate, item := range list.Items {
						if !item.Pending && (victim == "" || item.LastCompleted.Before(list.Items[victim].LastCompleted) || (item.LastCompleted.Equal(list.Items[victim].LastCompleted) && candidate < victim)) {
							victim = candidate
						}
					}
					if victim == "" {
						for candidate, item := range list.Items {
							if item.Pending && item.Failures >= maxChildFailures && (victim == "" || item.LastAttempt.Before(list.Items[victim].LastAttempt) || (item.LastAttempt.Equal(list.Items[victim].LastAttempt) && candidate < victim)) {
								victim = candidate
							}
						}
					}
					if victim == "" {
						return errPendingCapacity
					}
					if e = pruneCheckpoint(rt, parent, list.Items[victim], false); e != nil {
						return e
					}
					delete(list.Items, victim)
					dirty = true
				}
				// Membership/content can change without a parent revision. Refresh all
				// child families hourly; active remote sessions refresh each poll.
				refresh := !old.Pending && (d.Name == "remote-sessions" || p.now().Sub(old.LastCompleted) >= time.Hour)
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
		}
	}
	p.onRecord = discover(p.conf)
	p.flushRecords = persist
	defer func() { p.onRecord, p.flushRecords = nil, nil }()
	rootCont, rootErr := p.handleOne(ctx, rt)
	rootPending := rootCont != nil && rootCont.Delay == 0
	if errors.Is(rootErr, errPendingCapacity) {
		rootPending, rootErr = true, nil
	}
	if e = persist(); e != nil {
		return nil, e
	}
	// A work item persisted before the discovery-side oversized-identity guard
	// existed (or one that otherwise slipped through) can never make progress:
	// its "_parent" value will permanently fail to encode as an enumerated
	// value. Retire it safely now instead of letting it cycle through
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
	if rootErr == nil && rootCont != nil && rootCont.Delay != 0 && absenceTrackedParents[p.conf.dataset.Name] {
		if e = retireAbsentChildren(rt, p.conf, &list, &dirty); e != nil {
			return nil, e
		}
		if e = persist(); e != nil {
			return nil, e
		}
	}
	keys := []string{}
	for k, w := range list.Items {
		if w.Pending && !p.now().Before(w.RetryAt) {
			keys = append(keys, k)
		}
	}
	// Order the oldest-attempted work first. A zero LastAttempt is reserved
	// for work items persisted before this fairness fix existed; treat it as
	// the lowest priority (rather than the highest, which is what an
	// unqualified time-zero comparison would do) so a legacy item is not
	// stuck perpetually cutting ahead of everything else. Once such an item
	// is actually attempted it gets a real LastAttempt and sorts normally.
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
		w := list.Items[k]
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
		child.onRecord = discover(&c)
		child.flushRecords = persist
		w.LastAttempt = p.now().UTC()
		childCont, childErr := child.handleOne(ctx, rt)
		if errors.Is(childErr, errPendingCapacity) {
			childCont, childErr = hosted.ContinueNow(), nil
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
	return p.conf.PendingOrInterval(rootPending || len(keys) > p.conf.Max_Children), nil
}

func deletedChildWork(d Dataset, inherited []string, raw []byte) (work, bool, error) {
	if d.Name != "chats" {
		return work{}, false, nil
	}
	var v map[string]json.RawMessage
	if e := json.Unmarshal(raw, &v); e != nil {
		return work{}, false, e
	}
	deleted := v["deleted_at"]
	if len(deleted) == 0 || string(deleted) == "null" {
		return work{}, false, nil
	}
	var id string
	_ = json.Unmarshal(v["id"], &id)
	if id == "" {
		return work{}, false, fmt.Errorf("%w: chats parent missing id", errMissingParentID)
	}
	if len(id) > maxDiscoveredParameterLen {
		return work{}, false, fmt.Errorf("%w: chats id is %d bytes", errOversizedIdentity, len(id))
	}
	params := append(append([]string(nil), inherited...), "chat_id:"+id)
	sort.Strings(params)
	h := sha256.Sum256(raw)
	return work{Dataset: "chat-messages", Parameter: params, Revision: fmt.Sprintf("%x", h)}, true, nil
}

func childConfig(parent *Config, w work) (*Config, error) {
	c := *parent
	c.Dataset = w.Dataset
	c.Parameter = append([]string(nil), w.Parameter...)
	c.Follow_Children = "disabled"
	c.discovered = true
	d, ok := lookup(c.Dataset)
	if !ok {
		return nil, errors.New("unknown Compliance child dataset")
	}
	if d.Tag != parent.dataset.Tag {
		return nil, errors.New("Compliance cross-family discovery requires an explicit tag mapping")
	}
	if d.Limit > 0 {
		c.Page_Size = min(c.Page_Size, d.Limit)
	}
	if e := c.Verify(); e != nil {
		return nil, e
	}
	return &c, nil
}

func retiredCheckpoint(rt hosted.Runtime, key, revision string) (bool, error) {
	b, e := rt.Get(key)
	if errors.Is(e, storage.ErrStorageNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var st state
	if e = json.Unmarshal(b, &st); e != nil {
		return false, errors.New("invalid Compliance child checkpoint")
	}
	return st.Retired != "" && st.Retired == revision, nil
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
	st := state{Since: w.LastCompleted, Manifest: manifest{}}
	if b, e := rt.Get(key); e == nil {
		if e = json.Unmarshal(b, &st); e != nil {
			return errors.New("invalid Compliance child checkpoint")
		}
	} else if !errors.Is(e, storage.ErrStorageNotFound) {
		return e
	}
	st.Manifest = manifest{}
	st.Traversal = nil
	if tombstone {
		st.Retired = w.Revision
	}
	b, e := json.Marshal(st)
	if e != nil {
		return e
	}
	return rt.Put(key, b)
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
	if d.Name == "chats" {
		if s := v["deleted_at"]; len(s) > 0 && string(s) != "null" {
			return nil, nil
		}
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
	var result []work
	h := sha256.Sum256(raw)
	for _, s := range specs {
		params := append(append([]string(nil), inherited...), s.param+":"+id)
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
