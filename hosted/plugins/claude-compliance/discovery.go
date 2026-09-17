package claudecompliance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
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
	Pending       bool
	LastCompleted time.Time
	LastAttempt   time.Time
	RetryAt       time.Time
	Failures      uint
}
type worklist struct{ Items map[string]work }

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
	persist := func() error {
		b, e := json.Marshal(list)
		if e != nil {
			return e
		}
		if len(b) > 32<<20 {
			return errors.New("child worklist exceeds size bound")
		}
		return rt.Put(key, b)
	}
	discover := func(parent *Config) func(Dataset, []byte) error {
		return func(d Dataset, raw []byte) error {
			children, e := childWork(d, parent.Parameter, raw)
			if e != nil {
				return e
			}
			for _, w := range children {
				k := w.Dataset + "/" + strings.Join(w.Parameter, "/")
				old, exists := list.Items[k]
				if !exists && len(list.Items) >= p.conf.Max_Pending {
					// Completed history is expendable; pending work is never evicted.
					victim := ""
					for candidate, item := range list.Items {
						if !item.Pending && (victim == "" || item.LastCompleted.Before(list.Items[victim].LastCompleted) || (item.LastCompleted.Equal(list.Items[victim].LastCompleted) && candidate < victim)) {
							victim = candidate
						}
					}
					if victim == "" {
						return errors.New("Compliance pending child-work capacity reached; state retained; increase Max-Pending or reduce parent scope")
					}
					delete(list.Items, victim)
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
				}
			}
			return persist()
		}
	}
	p.onRecord = discover(p.conf)
	defer func() { p.onRecord = nil }()
	_, rootErr := p.handleOne(ctx, rt)
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
		c := *p.conf
		c.Dataset = w.Dataset
		c.Parameter = append([]string(nil), w.Parameter...)
		c.Follow_Children = "disabled"
		d, _ := lookup(c.Dataset)
		if d.Tag != p.conf.dataset.Tag {
			return nil, errors.New("Compliance cross-family discovery requires an explicit tag mapping")
		}
		if d.Limit > 0 {
			c.Page_Size = min(c.Page_Size, d.Limit)
		}
		if e = c.Verify(); e != nil {
			return nil, e
		}
		child := *p
		child.conf = &c
		child.onRecord = discover(&c)
		w.LastAttempt = p.now().UTC()
		if _, e = child.handleOne(ctx, rt); e != nil {
			w.Failures = min(w.Failures+1, 10)
			w.RetryAt = w.LastAttempt.Add(time.Minute * time.Duration(1<<(w.Failures-1)))
			list.Items[k] = w
			if pe := persist(); pe != nil {
				return nil, pe
			}
			errs = append(errs, fmt.Errorf("pending %s: %w", w.Dataset, e))
			continue
		}
		w.Pending = false
		w.LastCompleted = p.now().UTC()
		w.RetryAt = time.Time{}
		w.Failures = 0
		list.Items[k] = w
		if e = persist(); e != nil {
			return nil, e
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return p.conf.ContinueAfterInterval(), nil
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
		return nil, fmt.Errorf("%s parent missing %s", d.Name, idKey)
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
