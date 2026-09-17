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
}
type worklist struct{ Items map[string]work }

var (
	errPendingCapacity = errors.New("Compliance pending child-work capacity reached")
	errMissingParentID = errors.New("Compliance parent identity is missing")
)

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
				return e
			} else if ok {
				k := retired.Dataset + "/" + strings.Join(retired.Parameter, "/")
				if old, exists := list.Items[k]; exists {
					retired.LastCompleted = old.LastCompleted
					retired.StateKey = old.StateKey
				}
				if e = pruneCheckpoint(rt, parent, retired); e != nil {
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
				return e
			}
			for _, w := range children {
				k := w.Dataset + "/" + strings.Join(w.Parameter, "/")
				old, exists := list.Items[k]
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
					// Completed history is expendable; pending work is never evicted.
					victim := ""
					for candidate, item := range list.Items {
						if !item.Pending && (victim == "" || item.LastCompleted.Before(list.Items[victim].LastCompleted) || (item.LastCompleted.Equal(list.Items[victim].LastCompleted) && candidate < victim)) {
							victim = candidate
						}
					}
					if victim == "" {
						return errPendingCapacity
					}
					if e = pruneCheckpoint(rt, parent, list.Items[victim]); e != nil {
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
					w.LastAttempt = old.LastAttempt
					if old.Revision == w.Revision {
						w.RetryAt, w.Failures = old.RetryAt, old.Failures
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
	keys := []string{}
	for k, w := range list.Items {
		if w.Pending && !p.now().Before(w.RetryAt) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := list.Items[keys[i]].LastAttempt, list.Items[keys[j]].LastAttempt
		if a.Equal(b) {
			return keys[i] < keys[j]
		}
		return a.Before(b)
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
			w.Failures = min(w.Failures+1, 10)
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

func pruneCheckpoint(rt hosted.Runtime, parent *Config, w work) error {
	key := w.StateKey
	if key == "" {
		c, e := childConfig(parent, w)
		if e != nil {
			return e
		}
		key = c.key()
	}
	st := state{Since: w.LastCompleted, Manifest: map[string]string{}}
	if b, e := rt.Get(key); e == nil {
		if e = json.Unmarshal(b, &st); e != nil {
			return errors.New("invalid Compliance child checkpoint")
		}
	} else if !errors.Is(e, storage.ErrStorageNotFound) {
		return e
	}
	st.Manifest = map[string]string{}
	st.Traversal = nil
	st.Retired = w.Revision
	b, e := json.Marshal(st)
	if e != nil {
		return e
	}
	return rt.Put(key, b)
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
	type spec struct{ name, param string }
	var specs []spec
	switch d.Name {
	case "organizations":
		specs = []spec{{"organization-users", "organization_id"}, {"organization-roles", "organization_id"}}
	case "organization-roles":
		specs = []spec{{"role-permissions", "role_id"}}
	case "groups":
		specs = []spec{{"group-members", "group_id"}}
	case "chats":
		if s := v["deleted_at"]; len(s) > 0 && string(s) != "null" {
			return nil, nil
		}
		specs = []spec{{"chat-messages", "chat_id"}}
	case "projects":
		specs = []spec{{"project-attachments", "project_id"}, {"project-collaborators", "project_id"}}
	case "local-sessions":
		specs = []spec{{"local-session-messages", "session_id"}}
	case "remote-sessions":
		specs = []spec{{"remote-session-messages", "session_id"}}
	}
	if len(specs) == 0 {
		return nil, nil
	}
	if id == "" {
		return nil, fmt.Errorf("%w: %s parent missing %s", errMissingParentID, d.Name, idKey)
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
