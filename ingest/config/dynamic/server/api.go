/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/rpc"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

// pushTimeout bounds a call down to an ingester.  A wedged ingester must not hold an HTTP
// handler open.
const pushTimeout = 10 * time.Second

// API is the server side of the dynamic config protocol.  It owns the store and the set
// of connected ingesters.
type API struct {
	store ProtocolStore
	// full is the same store when it implements everything, which is how ForgetIngester
	// reaches storage without NewAPI demanding sixteen methods from a caller that only
	// wants to serve ingesters.  Resolved once here rather than asserted on every call.
	full Store
	// groups answers what groups an ingester is in, see WithGroupMembership.  Nil means
	// nobody is in any group.
	groups func(uuid.UUID) ([]string, error)
	lgr    *log.Logger

	mtx       sync.RWMutex
	ingesters map[uuid.UUID]*rpc.Session
	classes   map[uuid.UUID]string
}

// NewAPI builds the server half.  It takes only ProtocolStore: serving ingesters needs
// four methods, and a caller that is not also serving a management interface should not
// have to implement the rest to get there.
// Option adjusts an API at construction.  Anything that varies by deployment goes through
// one of these rather than growing NewAPI another parameter that most callers pass nil to.
type Option func(*API)

// WithGroupMembership teaches the API what groups an ingester belongs to, which is what
// lets a configuration be pinned to a group.  fn is asked about the authenticated
// ingester's UUID and nothing else.
//
// Without it no ingester is in any group, so a runner pinned to one reaches nobody rather
// than everybody.  That is the safe direction, and it is what the test server does: it has
// no membership data to answer from.
//
// Membership is never taken off the wire.  An ingester that could name its own groups in a
// poll body would be claiming every configuration pinned to any group it cared to name,
// which is why listRunners discards what the body said and asks this instead.
func WithGroupMembership(fn func(uuid.UUID) ([]string, error)) Option {
	return func(a *API) { a.groups = fn }
}

func NewAPI(store ProtocolStore, lgr *log.Logger, opts ...Option) *API {
	if lgr == nil {
		lgr = log.NewDiscardLogger()
	}
	full, _ := store.(Store)
	a := &API{
		store:     store,
		full:      full,
		lgr:       lgr,
		ingesters: map[uuid.UUID]*rpc.Session{},
		classes:   map[uuid.UUID]string{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// groupsOf is the groups the server says an ingester belongs to.
func (a *API) groupsOf(id uuid.UUID) (groups []string, err error) {
	if a.groups == nil || id == uuid.Nil() {
		return nil, nil
	}
	if groups, err = a.groups(id); err != nil {
		return nil, fmt.Errorf("failed to read the groups of %v %w", id, err)
	}
	return
}

// Mux builds the method set we expose to ingesters.
func (a *API) Mux() (m *rpc.Mux, err error) {
	m = rpc.NewMux()
	if err = m.Register(dynamic.MethodRegisterKinds, a.registerKinds); err != nil {
		return
	}
	if err = m.Register(dynamic.MethodListRunners, a.listRunners); err != nil {
		return
	}
	err = m.Register(dynamic.MethodReportStatus, a.reportStatus)
	return
}

// OnSession tracks a connected ingester for the life of its session, so the UI has
// somewhere to push a config change.
func (a *API) OnSession(s *rpc.Session) {
	id := s.ID()
	a.mtx.Lock()
	// a reconnect replaces the old handle, the old session is already dead
	a.ingesters[id] = s
	a.mtx.Unlock()
	a.lgr.Info("ingester connected",
		log.KV("id", id),
		log.KV("class", s.Class()),
		log.KV("remote", s.RemoteAddr()))

	<-s.Done()

	a.mtx.Lock()
	// only drop it if it is still ours, a reconnect may have replaced us already
	if cur, ok := a.ingesters[id]; ok && cur == s {
		delete(a.ingesters, id)
	}
	a.mtx.Unlock()
	a.lgr.Info("ingester disconnected", log.KV("id", id), log.KVErr(s.Err()))
}

// ErrIngesterConnected is returned when an operation requires an ingester that is not
// currently talking to this server.
var ErrIngesterConnected = errors.New("ingester is connected")

// connected reports whether one ingester has a session here right now.
func (a *API) connected(id uuid.UUID) bool {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	_, ok := a.ingesters[id]
	return ok
}

// ForgetIngester drops everything held about an ingester this server is not currently
// talking to: its registrations, the statuses it reported, and the ingester record.
//
// A connected one is refused, and the reason is worth spelling out because it is invisible
// from the outside.  The two delivery paths take an ingester's kinds from different places:
// listRunners takes them from the request body, so a forgotten ingester keeps being handed
// configurations on every poll, while targeted takes them from the store, so every push
// silently stops reaching it.  Forgetting a live ingester therefore leaves it half
// connected — still receiving work, no longer receiving pushes, absent from the catalog
// until it happens to reconnect — and nothing anywhere reports that.
//
// The guard lives here rather than in each caller so that a webserver and the test server
// cannot disagree about it.  A store knows nothing about sessions and cannot enforce this.
//
// For the case the feature exists for, a decommissioned box, the refusal costs nothing:
// such a box is not connected.
func (a *API) ForgetIngester(id uuid.UUID) error {
	if a.full == nil {
		return errors.New("this store serves the protocol only, it cannot forget an ingester")
	}
	if a.connected(id) {
		return fmt.Errorf("%w %v, stop or disconnect it first", ErrIngesterConnected, id)
	}
	if err := a.full.ForgetIngester(id); err != nil {
		return err
	}
	a.lgr.Info("forgot ingester", log.KV("ingester", id))
	return nil
}

// Connected lists the ingesters we can currently push to.
func (a *API) Connected() (r []*rpc.Session) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	for _, s := range a.ingesters {
		r = append(r, s)
	}
	return
}

// registerKinds stores everything an ingester says it can run, keyed by that ingester.
//
// The identity comes from the session rather than from the body.  The handshake is what
// proves who the peer is, so a body that claims a different UUID is ignored rather than
// believed, otherwise any authenticated ingester could overwrite another's registrations.
func (a *API) registerKinds(ctx context.Context, params json.RawMessage) (any, error) {
	id, ok := sessionID(ctx)
	if !ok {
		return nil, fmt.Errorf("no session identity")
	}
	var req dynamic.RegisterKindsRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("bad registration %w", err)
	}
	// the class comes from the session too, for the same reason the UUID does
	class := req.Class
	if sess, ok := rpc.SessionFrom(ctx); ok && sess.Class() != `` {
		class = sess.Class()
	}
	if err := a.store.ReplaceKinds(id, class, req.Kinds); err != nil {
		return nil, err
	}
	a.mtx.Lock()
	a.classes[id] = class
	a.mtx.Unlock()
	a.lgr.Info("registered kinds",
		log.KV("ingester", id),
		log.KV("class", class),
		log.KV("count", len(req.Kinds)))
	return map[string]any{`ok`: true}, nil
}

// listRunners hands an ingester the configurations it should be running: the ones whose
// kind it can actually run and whose assignment names it, its class, or nobody.
func (a *API) listRunners(ctx context.Context, params json.RawMessage) (any, error) {
	var q dynamic.RunnerQuery
	if len(params) > 0 {
		if err := json.Unmarshal(params, &q); err != nil {
			return nil, fmt.Errorf("bad query %w", err)
		}
	}
	// the identity is the session's, not whatever the body asked for.  A configuration
	// pinned to specific UUIDs or classes would be trivially reachable otherwise, an
	// ingester would just claim to be someone it is not.
	if sess, ok := rpc.SessionFrom(ctx); ok {
		q.ID = sess.ID()
		q.Class = sess.Class()
	}
	// groups are the same story and are overwritten rather than defaulted, so a body that
	// named some does not keep them, see WithGroupMembership
	groups, err := a.groupsOf(q.ID)
	if err != nil {
		return nil, err
	}
	q.Groups = groups
	all, err := a.store.Runners()
	if err != nil {
		return nil, err
	}
	var set dynamic.RunnerSet
	for _, rd := range all {
		if q.Matches(rd) {
			set.Runners = append(set.Runners, rd)
		}
	}
	a.lgr.Info("listed runners",
		log.KV("ingester", q.ID),
		log.KV("class", q.Class),
		log.KV("kinds", len(q.Kinds)),
		log.KV("matched", len(set.Runners)))
	return set, nil
}

// reportStatus records what an ingester makes of the configurations it was handed.
//
// The identity comes from the session rather than the body, for the same reason
// registerKinds takes it from there: otherwise any authenticated ingester could plant a
// failure against another one's name, or clear a real one.
//
// The report is the complete set for that ingester, so it replaces rather than merges,
// which is what lets a runner that has come good clear itself.
func (a *API) reportStatus(ctx context.Context, params json.RawMessage) (any, error) {
	id, ok := sessionID(ctx)
	if !ok {
		return nil, fmt.Errorf("no session identity")
	}
	var req dynamic.ReportStatusRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("bad status report %w", err)
		}
	}
	if err := a.store.ReplaceStatuses(id, req.Statuses); err != nil {
		return nil, err
	}
	var bad int
	for _, rs := range req.Statuses {
		if !rs.OK() {
			bad++
		}
	}
	a.lgr.Info("recorded runner status",
		log.KV("ingester", id),
		log.KV("reported", len(req.Statuses)),
		log.KV("failing", bad))
	return map[string]any{`ok`: true}, nil
}

// Push sends a configuration down to the connected ingesters it is actually meant for,
// and reports which ones took it.
//
// The assignment is applied here, not just at poll time.  A push is how a change reaches
// an ingester promptly, so if it ignored the assignment a configuration pinned to one
// ingester would still land on every other one the moment it was saved, and the pinning
// would only work until the next save.  The rule is the same RunnerQuery.Matches the poll
// uses, so the two paths cannot disagree.
//
// With no ingester connected, or none that the configuration is meant for, it is simply
// stored and picked up on the next poll.  That is a normal state, not a failure.
func (a *API) Push(rd dynamic.RunnerDefinition) (delivered int, errs []string) {
	sessions := a.Connected()
	if len(sessions) == 0 {
		return
	}
	targets := make([]*rpc.Session, 0, len(sessions))
	for _, s := range sessions {
		if a.targeted(s, rd) {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		return
	}

	// Each ingester gets its own deadline and its own goroutine.  Sharing one timeout
	// across the loop meant the budget was spent rather than applied: with enough
	// ingesters the ones at the back were reported as having refused a configuration they
	// were never actually asked about, and one slow ingester was enough to do it.  Running
	// them together also keeps the handler that is waiting on this from being held open
	// for the sum of every ingester's timeout.
	type outcome struct {
		id  uuid.UUID
		err error
	}
	results := make(chan outcome, len(targets))
	var wg sync.WaitGroup
	for _, s := range targets {
		wg.Add(1)
		go func(s *rpc.Session) {
			defer wg.Done()
			ctx, cf := context.WithTimeout(context.Background(), pushTimeout)
			defer cf()
			results <- outcome{id: s.ID(), err: s.Call(ctx, dynamic.MethodApplyConfig, rd, nil)}
		}(s)
	}
	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			a.lgr.Error("failed to push config",
				log.KV("id", res.id),
				log.KV("kind", rd.Kind),
				log.KV("name", rd.Name), log.KVErr(res.err))
			errs = append(errs, fmt.Sprintf("%v: %v", res.id, res.err))
			continue
		}
		delivered++
	}
	// the map iteration behind Connected has no order, so give the operator a stable one
	sort.Strings(errs)
	return
}

// targeted reports whether a configuration is meant for one connected ingester, using the
// kinds that ingester registered and the identity its session authenticated with.
func (a *API) targeted(s *rpc.Session, rd dynamic.RunnerDefinition) bool {
	kinds, err := a.store.IngesterKinds(s.ID())
	if err != nil {
		a.lgr.Error("failed to read an ingester's kinds", log.KV("id", s.ID()), log.KVErr(err))
		return false // we cannot show it is meant for them, so do not send it
	}
	groups, err := a.groupsOf(s.ID())
	if err != nil {
		a.lgr.Error("failed to read an ingester's groups", log.KV("id", s.ID()), log.KVErr(err))
		return false // we cannot show it is meant for them, so do not send it
	}
	q := dynamic.RunnerQuery{ID: s.ID(), Class: s.Class(), Groups: groups}
	for _, k := range kinds {
		q.Kinds = append(q.Kinds, k.Kind)
	}
	return q.Matches(rd)
}

// sessionID pulls the calling ingester's UUID out of the handler context.  The identity
// comes from the authenticated session rather than from the request body, so an ingester
// cannot register kinds or claim configurations on behalf of another.
func sessionID(ctx context.Context) (id uuid.UUID, ok bool) {
	var s *rpc.Session
	if s, ok = rpc.SessionFrom(ctx); !ok {
		return
	}
	id = s.ID()
	ok = id != uuid.Nil()
	return
}
