/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"time"

	"github.com/gravwell/jsonparser"
)

// userState is the part of a user record that decides whether it is emitted again: lastUpdated
// moves when the user changes, lastLogin when they log in. Okta does not let a search filter on
// lastLogin, so changes are found by comparing each listing with the last one.
type userState struct {
	LastUpdated string
	LastLogin   string // empty for a user that has never logged in, Okta sends null
}

var userPaths = [][]string{{`id`}, {`lastUpdated`}, {`lastLogin`}}

// parseUser reads the id and state of a user record in place: the record is ingested as is, so it
// is only scanned for these top level keys, never decoded
func parseUser(raw []byte) (id string, st userState) {
	jsonparser.EachKey(raw, func(idx int, v []byte, vt jsonparser.ValueType, err error) {
		if err != nil || vt != jsonparser.String {
			return // a null lastLogin stays empty
		}
		s, err := jsonparser.ParseString(v)
		if err != nil {
			return
		}
		switch idx {
		case 0:
			id = s
		case 1:
			st.LastUpdated = s
		case 2:
			st.LastLogin = s
		}
	}, userPaths...)
	return
}

// userTracker remembers the state of every user last emitted. It is owned by the user routine and
// is not safe for concurrent use. It lives in memory only: after a restart it starts empty, and until
// a listing completes, a user it has not seen counts as changed only if it changed after since.
type userTracker struct {
	seen    map[string]userState
	visited map[string]struct{} // users listed in the current pass
	since   time.Time           // zero once a listing has completed, every unseen user is then new
	dumped  map[string]struct{} // users written by the dump in progress, nil when none is
}

func newUserTracker() *userTracker {
	return &userTracker{seen: map[string]userState{}}
}

// begin starts a listing pass
func (t *userTracker) begin() {
	t.visited = make(map[string]struct{}, len(t.seen))
}

// check reads a user record and reports whether it is new or changed since it was last recorded.
// A record without an id cannot be tracked and is always reported as changed.
func (t *userTracker) check(raw []byte) (id string, st userState, changed bool) {
	if id, st = parseUser(raw); id == `` {
		return ``, userState{}, true
	}
	t.visited[id] = struct{}{}
	old, ok := t.seen[id]
	if !ok {
		return id, st, t.since.IsZero() || st.after(t.since)
	}
	return id, st, old != st
}

// after reports whether the user was updated or logged in after t. A time that does not parse
// counts as after, an unneeded emit is better than a missed one.
func (s userState) after(t time.Time) bool {
	for _, v := range []string{s.LastUpdated, s.LastLogin} {
		if v == `` {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, v); err != nil || ts.After(t) {
			return true
		}
	}
	return false
}

// startDump starts a dump, or carries on with one that failed part way
func (t *userTracker) startDump() {
	if t.dumped == nil {
		t.dumped = map[string]struct{}{}
	}
}

// endDump ends a dump that completed
func (t *userTracker) endDump() {
	t.dumped = nil
}

// wanted reports whether a listed user is to be written: a new or changed one always is, and during a
// dump so is every user the dump has not written yet, including across retries of a failed dump
func (t *userTracker) wanted(id string, changed bool) bool {
	if changed || id == `` {
		return true
	} else if t.dumped == nil {
		return false
	}
	_, done := t.dumped[id]
	return !done
}

// record notes the state of a user, once written, or once found unchanged
func (t *userTracker) record(id string, st userState) {
	if id == `` {
		return
	}
	t.seen[id] = st
	if t.dumped != nil {
		t.dumped[id] = struct{}{}
	}
}

// prune forgets the users a complete listing did not include, such as deleted users. Every user
// has now been seen, so the restart window is no longer needed.
func (t *userTracker) prune() {
	t.since = time.Time{}
	for id := range t.seen {
		if _, ok := t.visited[id]; !ok {
			delete(t.seen, id)
		}
	}
}
