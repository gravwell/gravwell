/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/server"
)

// Store is an in-memory implementation of server.Store.
//
// Nothing is persisted.  This is a rig for exercising the protocol and the interface, and a
// rig that forgets everything when it stops is the honest shape for that: a fresh process
// is a fresh fleet, and the state an operator is looking at is always the state this run
// produced rather than something left over from a build two weeks ago.  It also means there
// is no schema to migrate every time a wire type grows a field, and no database file to go
// stale against the code that reads it.
//
// The rules it has to honour are documented on server.Store and checked by server.TestStore,
// which TestStoreConformance runs.  The comments here explain only how this particular
// backend goes about them, and two of those are worth reading before changing anything:
//
//   - Definitions are deep copied through JSON on the way in and on the way out.  That is
//     not paranoia about aliasing, though it handles that too.  A definition reaches a real
//     server as JSON and is kept as JSON, so every whole number in one is a float64 by the
//     time it is rendered to a config file.  Handing back the caller's own Go values would
//     make this rig the one place in the system where that is not true, and it would hide
//     exactly the class of bug that only shows up against a real webserver.  See
//     TestIniWholeNumberAfterJSON in the dynamic package.
//   - Kind and Name uniqueness is enforced by hand.  The SQL version of this store got it
//     free from a unique index, which made it invisible; here it is a check that can be
//     deleted by accident, so it has a name and a test.
type Store struct {
	mtx sync.RWMutex

	// ingesters is what each ingester said about itself when it last registered
	ingesters map[uuid.UUID]ingesterRecord

	// kinds is what each ingester says it can run, by ingester then kind.  Keyed by
	// ingester because two of them may advertise one kind from different builds and a
	// server has to be able to tell them apart.
	kinds map[uuid.UUID]map[string][]byte

	// runners is every configured runner, by its own UUID
	runners map[uuid.UUID][]byte

	// statuses is what each ingester last said about each runner, by ingester then runner.
	// Nested that way round because a report replaces one ingester's whole set.
	statuses map[uuid.UUID]map[uuid.UUID]server.StatusRow
}

// ingesterRecord is one ingester as this store remembers it.  registered orders the
// most-recent-wins tiebreak in Kinds, and is the same instant as LastSeen rather than a
// second clock that could disagree with it.
type ingesterRecord struct {
	class      string
	registered time.Time
}

var _ server.Store = (*Store)(nil)

// NewStore builds an empty store.
func NewStore() *Store {
	return &Store{
		ingesters: map[uuid.UUID]ingesterRecord{},
		kinds:     map[uuid.UUID]map[string][]byte{},
		runners:   map[uuid.UUID][]byte{},
		statuses:  map[uuid.UUID]map[uuid.UUID]server.StatusRow{},
	}
}

// Close exists so a caller can treat this like anything else that holds resources.  It
// holds none.
func (s *Store) Close() error { return nil }

// encode and decode are the JSON round trip every definition makes, see the note on Store.
func encode(rd dynamic.RunnerDefinition) ([]byte, error) {
	blob, err := json.Marshal(rd)
	if err != nil {
		return nil, fmt.Errorf("failed to encode definition %w", err)
	}
	return blob, nil
}

func decode(blob []byte) (rd dynamic.RunnerDefinition, err error) {
	if err = json.Unmarshal(blob, &rd); err != nil {
		return rd, fmt.Errorf("failed to decode a stored definition %w", err)
	}
	return
}

// ReplaceKinds records the complete set one ingester says it can run, and stamps that
// ingester's class and last-seen.  The whole set is replaced rather than merged, which is
// what lets a kind dropped from a build actually disappear.
func (s *Store) ReplaceKinds(ingester uuid.UUID, class string, kinds []dynamic.RunnerDefinition) error {
	if ingester == uuid.Nil() {
		return errors.New("registration has no ingester UUID")
	}
	// build the replacement before touching anything, so a bad kind half way down the list
	// leaves the previous registration intact rather than half of a new one
	next := make(map[string][]byte, len(kinds))
	for _, rd := range kinds {
		rd, err := server.NormalizeKind(rd)
		if err != nil {
			return err
		}
		blob, err := encode(rd)
		if err != nil {
			return err
		}
		next[rd.Kind] = blob
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.kinds[ingester] = next
	// remember the ingester itself, which is what lets the interface offer a real list of
	// UUIDs and classes to assign to rather than asking an operator to type them
	s.ingesters[ingester] = ingesterRecord{class: class, registered: time.Now()}
	return nil
}

// IngesterKinds lists what one ingester says it can run.
func (s *Store) IngesterKinds(ingester uuid.UUID) (r []dynamic.RunnerDefinition, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.definitionsLocked(s.kinds[ingester])
}

// definitionsLocked decodes a kind set in name order.  The caller holds the lock.
func (s *Store) definitionsLocked(set map[string][]byte) (r []dynamic.RunnerDefinition, err error) {
	names := make([]string, 0, len(set))
	for kind := range set {
		names = append(names, kind)
	}
	sort.Strings(names)
	for _, kind := range names {
		rd, derr := decode(set[kind])
		if derr != nil {
			return nil, derr
		}
		r = append(r, rd)
	}
	return
}

// Kinds lists the distinct kinds across every ingester, which is what the interface offers
// to configure.  Two ingesters advertising the same kind collapse to one entry and the most
// recently registered wins, because the operator is choosing a kind to configure rather
// than choosing an ingester.  A consumer that needs to tell two builds apart reads
// IngesterKinds instead.
func (s *Store) Kinds() (r []dynamic.RunnerDefinition, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	type entry struct {
		blob []byte
		when time.Time
	}
	best := map[string]entry{}
	for ingester, set := range s.kinds {
		when := s.ingesters[ingester].registered
		for kind, blob := range set {
			if cur, ok := best[kind]; ok && !when.After(cur.when) {
				continue
			}
			best[kind] = entry{blob: blob, when: when}
		}
	}
	flat := make(map[string][]byte, len(best))
	for kind, e := range best {
		flat[kind] = e.blob
	}
	return s.definitionsLocked(flat)
}

// Kind fetches one registration, preferring the most recently registered where several
// ingesters have it, the same rule Kinds applies.
func (s *Store) Kind(kind string) (rd dynamic.RunnerDefinition, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	var blob []byte
	var when time.Time
	for ingester, set := range s.kinds {
		cur, ok := set[kind]
		if !ok {
			continue
		}
		if at := s.ingesters[ingester].registered; blob == nil || at.After(when) {
			blob, when = cur, at
		}
	}
	if blob == nil {
		return rd, fmt.Errorf("kind %s %w", kind, server.ErrNotFound)
	}
	return decode(blob)
}

// KindMetadata is the description each kind ships with, keyed by kind.  A kind with no
// metadata is absent from the map rather than present and empty, so a caller can tell "this
// plugin describes itself" from "this plugin does not".
func (s *Store) KindMetadata() (r map[string]*dynamic.RunnerMetadata, err error) {
	kinds, err := s.Kinds()
	if err != nil {
		return nil, err
	}
	r = make(map[string]*dynamic.RunnerMetadata, len(kinds))
	for _, rd := range kinds {
		if rd.Metadata != nil {
			r[rd.Kind] = rd.Metadata
		}
	}
	return
}

// Ingesters lists every ingester that has ever registered, newest first, with the kinds
// each one advertised.
func (s *Store) Ingesters() (r []server.Ingester, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for id := range s.ingesters {
		r = append(r, s.ingesterLocked(id))
	}
	sortIngesters(r)
	return
}

// KindIngesters lists the ingesters that have registered a given kind, which is the set an
// operator may pin a configuration of that kind to.  Pinning to an ingester that cannot run
// the kind would produce a configuration that is never delivered.
func (s *Store) KindIngesters(kind string) (r []server.Ingester, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for id, set := range s.kinds {
		if _, ok := set[kind]; !ok {
			continue
		}
		if _, known := s.ingesters[id]; !known {
			continue
		}
		r = append(r, s.ingesterLocked(id))
	}
	sortIngesters(r)
	return
}

// ingesterLocked assembles one ingester with the kind names it advertised.  The caller
// holds the lock.
func (s *Store) ingesterLocked(id uuid.UUID) server.Ingester {
	rec := s.ingesters[id]
	ing := server.Ingester{UUID: id, Class: rec.class, LastSeen: rec.registered}
	for kind := range s.kinds[id] {
		ing.Kinds = append(ing.Kinds, kind)
	}
	sort.Strings(ing.Kinds)
	return ing
}

// sortIngesters puts the newest first, with the UUID as the tiebreak so that two ingesters
// registered in the same instant do not swap places between two polls of the same page.
func sortIngesters(r []server.Ingester) {
	sort.Slice(r, func(i, j int) bool {
		if !r[i].LastSeen.Equal(r[j].LastSeen) {
			return r[i].LastSeen.After(r[j].LastSeen)
		}
		return r[i].UUID.String() < r[j].UUID.String()
	})
}

// Classes lists the distinct classes that have been seen, so the interface can offer them
// rather than asking an operator to remember what they called things.
func (s *Store) Classes() (r []string, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	seen := map[string]bool{}
	for _, rec := range s.ingesters {
		if rec.class == `` || seen[rec.class] {
			continue
		}
		seen[rec.class] = true
		r = append(r, rec.class)
	}
	sort.Strings(r)
	return
}

// ForgetIngester removes the registrations, the statuses and the ingester record held under
// one UUID.  Runners keep their pins, see server.Store.
//
// The cleanup happens whether or not there was an ingester record, and ErrNotFound is
// decided after it rather than instead of it: rows left under a UUID with no record are
// exactly what an operator reaches for this to clear, so a path that reported "unknown" and
// left them behind would fail at the one job that mattered.
func (s *Store) ForgetIngester(id uuid.UUID) error {
	if id == uuid.Nil() {
		return errors.New("no ingester UUID")
	}
	s.mtx.Lock()
	defer s.mtx.Unlock()
	_, known := s.ingesters[id]
	delete(s.kinds, id)
	delete(s.statuses, id)
	delete(s.ingesters, id)
	if !known {
		return fmt.Errorf("ingester %v %w", id, server.ErrNotFound)
	}
	return nil
}

// PutRunner records a configured runner.  The UUID is the identity, so saving an edit
// updates in place while a new UUID is a new runner.
//
// Metadata is dropped rather than stored.  An icon and a version describe a plugin, not one
// configuration of it, and the registration already holds them: keeping a copy on every
// runner would store the same SVG once per configured instance and ship it back down to
// every ingester on every poll, to say something they already know.  The interface looks a
// runner's icon up through its kind, see KindMetadata.
func (s *Store) PutRunner(rd dynamic.RunnerDefinition) error {
	if rd.Kind == `` {
		return errors.New("runner has no kind")
	} else if rd.Name == `` {
		return errors.New("runner has no name")
	} else if rd.UUID == uuid.Nil() {
		return errors.New("runner has no UUID")
	}
	rd.Metadata = nil // a copy, the caller's definition is untouched
	blob, err := encode(rd)
	if err != nil {
		return err
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	if err = s.nameIsFreeLocked(rd); err != nil {
		return err
	}
	s.runners[rd.UUID] = blob
	return nil
}

// nameIsFreeLocked is the kind-and-name uniqueness rule, which a SQL backend gets from a
// unique index and this one has to check.  It is the same rule the ingester's own manager
// applies: two runners of one kind sharing a name would render to one config file name and
// each would overwrite the other.  The caller holds the lock.
func (s *Store) nameIsFreeLocked(rd dynamic.RunnerDefinition) error {
	for id, blob := range s.runners {
		if id == rd.UUID {
			continue // this is the runner being edited
		}
		other, err := decode(blob)
		if err != nil {
			return err
		}
		if other.Kind == rd.Kind && other.Name == rd.Name {
			return fmt.Errorf("failed to store runner %s/%s, %v already has that name",
				rd.Kind, rd.Name, id)
		}
	}
	return nil
}

// Runners lists every configured runner, ordered by kind then name so the UI order is
// stable across reloads.
func (s *Store) Runners() (r []dynamic.RunnerDefinition, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, blob := range s.runners {
		rd, derr := decode(blob)
		if derr != nil {
			return nil, derr
		}
		r = append(r, rd)
	}
	sort.Slice(r, func(i, j int) bool {
		if r[i].Kind != r[j].Kind {
			return r[i].Kind < r[j].Kind
		}
		if r[i].Name != r[j].Name {
			return r[i].Name < r[j].Name
		}
		return r[i].UUID.String() < r[j].UUID.String()
	})
	return
}

// Runner fetches one configured runner by UUID.
func (s *Store) Runner(id uuid.UUID) (rd dynamic.RunnerDefinition, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	blob, ok := s.runners[id]
	if !ok {
		return rd, fmt.Errorf("runner %v %w", id, server.ErrNotFound)
	}
	return decode(blob)
}

// DeleteRunner removes a configured runner, and with it everything any ingester had to say
// about it.  Leaving the statuses behind would strand an error against a runner that no
// longer exists, and the next report cannot clear it because the ingester will not mention
// a runner it was never handed.
func (s *Store) DeleteRunner(id uuid.UUID) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.runners[id]; !ok {
		return fmt.Errorf("runner %v %w", id, server.ErrNotFound)
	}
	delete(s.runners, id)
	// both halves or neither, which a single lock makes trivial here
	for _, byRunner := range s.statuses {
		delete(byRunner, id)
	}
	return nil
}

// ReplaceStatuses records what one ingester currently makes of its configurations.
//
// The whole set is replaced rather than merged, the same way registrations are.  That is
// what makes a runner that has come good clear itself: it arrives reported clean and
// overwrites the error that was there, and a runner that is no longer assigned to this
// ingester simply stops arriving and its row goes.  Nothing has to remember to send a
// retraction, which is the kind of thing that gets forgotten and leaves a stale red mark on
// a screen forever.
//
// The since carry-forward is server.MergeStatuses' to decide, not this store's.
func (s *Store) ReplaceStatuses(ingester uuid.UUID, statuses []dynamic.RunnerStatus) error {
	if ingester == uuid.Nil() {
		return errors.New("status report has no ingester UUID")
	}
	s.mtx.Lock()
	defer s.mtx.Unlock()

	existing := make([]server.StatusRow, 0, len(s.statuses[ingester]))
	for _, row := range s.statuses[ingester] {
		existing = append(existing, row)
	}
	next := map[uuid.UUID]server.StatusRow{}
	for _, row := range server.MergeStatuses(existing, statuses, ingester, time.Now()) {
		next[row.Runner] = row
	}
	s.statuses[ingester] = next
	return nil
}

// Statuses is every status row, newest report first, for the interface to draw from.
func (s *Store) Statuses() (r []server.StatusRow, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, byRunner := range s.statuses {
		for _, row := range byRunner {
			r = append(r, row)
		}
	}
	sort.Slice(r, func(i, j int) bool {
		if !r[i].Updated.Equal(r[j].Updated) {
			return r[i].Updated.After(r[j].Updated)
		}
		return r[i].Runner.String() < r[j].Runner.String()
	})
	return
}

// RunnerStatuses is what every ingester has said about one runner.  An error first ordering
// puts the thing an operator opened the page to read at the top.
func (s *Store) RunnerStatuses(id uuid.UUID) (r []server.StatusRow, err error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, byRunner := range s.statuses {
		if row, ok := byRunner[id]; ok {
			r = append(r, row)
		}
	}
	sort.Slice(r, func(i, j int) bool {
		if r[i].OK() != r[j].OK() {
			return !r[i].OK() // failures first
		}
		if !r[i].Updated.Equal(r[j].Updated) {
			return r[i].Updated.After(r[j].Updated)
		}
		return r[i].Ingester.String() < r[j].Ingester.String()
	})
	return
}
