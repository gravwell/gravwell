/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

// defaultMatchWindow is the number of most-recent messages compared when
// deciding whether a request continues an existing session. It bounds both the
// per-session memory footprint and the per-request comparison cost.
const defaultMatchWindow = 10

// sessionEntry is one tracked conversation. We identify a conversation by the
// most recent request we saw on it: its total message count (Len) plus the
// hashes of its trailing window of messages (Tail, at most matchWindow long).
// A later request continues this session when it carries those same trailing
// messages unchanged at the same positions. We store only hashes (never
// content) to keep memory bounded and to avoid persisting prompt text to disk.
type sessionEntry struct {
	ID       string
	Tail     []string // hashes of the last min(Len, window) messages of the most recent request
	Len      int      // total message count of that most recent request
	LastSeen time.Time
}

// sessionStore performs auto-derived session matching for stateless chat APIs.
// Sessions are partitioned by client IP so traffic from different clients
// never cross-matches.
type sessionStore struct {
	mu       sync.Mutex
	byClient map[string][]*sessionEntry
	ttl      time.Duration
	persist  *utils.State

	// matching / cap configuration
	maxPerClient int
	matchWindow  int
}

// persistedSessions is the on-disk form. We can change the in-memory layout
// later without breaking the on-disk schema by serialising this fixed shape.
type persistedSessions struct {
	Now     time.Time
	Entries []persistedEntry
}

type persistedEntry struct {
	Client   string
	ID       string
	Tail     []string
	Len      int
	LastSeen time.Time
}

func newSessionStore(ttl time.Duration, matchWindow int, statePath string) (*sessionStore, error) {
	if matchWindow <= 0 {
		matchWindow = defaultMatchWindow
	}
	s := &sessionStore{
		byClient:     map[string][]*sessionEntry{},
		ttl:          ttl,
		maxPerClient: 256,
		matchWindow:  matchWindow,
	}
	if statePath != "" {
		st, err := utils.NewState(statePath, 0600)
		if err != nil {
			return nil, err
		}
		s.persist = st
		var p persistedSessions
		if err := st.Read(&p); err == nil {
			s.load(&p)
		} else if err != utils.ErrNoState {
			return nil, err
		}
	}
	return s, nil
}

func (s *sessionStore) load(p *persistedSessions) {
	cutoff := time.Now().Add(-s.ttl)
	for _, e := range p.Entries {
		if e.LastSeen.Before(cutoff) {
			continue
		}
		// Resolve indexes hashes[c.Len-len(c.Tail):c.Len], which requires the
		// invariant len(Tail) <= Len (and Len > 0) that minting enforces. A
		// corrupted or tampered state file could violate it and panic Resolve,
		// so drop any entry that doesn't hold up.
		if e.Len <= 0 || len(e.Tail) > e.Len {
			continue
		}
		tail := e.Tail
		if s.matchWindow > 0 && len(tail) > s.matchWindow {
			tail = tail[len(tail)-s.matchWindow:]
		}
		entry := &sessionEntry{
			ID:       e.ID,
			Tail:     slices.Clone(tail),
			Len:      e.Len,
			LastSeen: e.LastSeen,
		}
		s.byClient[e.Client] = append(s.byClient[e.Client], entry)
	}
	// Enforce the same per-client cap used when minting so a large or tampered
	// state file can't restore unbounded per-client buckets. Entries are
	// persisted in append (roughly chronological) order, so keep the newest.
	for k, bucket := range s.byClient {
		if len(bucket) > s.maxPerClient {
			trimmed := bucket[len(bucket)-s.maxPerClient:]
			s.byClient[k] = slices.Clone(trimmed)
		}
	}
}

// Resolve looks up or mints a session ID for the incoming request.
// hashes is the canonical per-message hash sequence (oldest message first,
// latest message last). It returns the session ID and a boolean indicating
// whether this was a new session (no matching conversation found).
//
// A request continues an existing session when it carries that session's most
// recent request unchanged as a leading run: for a stored session that last
// saw a request of length L, the incoming request must be at least L messages
// long and its messages in [L-len(Tail), L) must equal the stored Tail. We make
// no assumption about how many (or what kind of) messages the client appended
// past that point — one new user turn, several stacked turns, tool results, or
// nothing at all (a retry) all match. When several sessions qualify, the one
// with the longest matched request wins as the most specific continuation.
func (s *sessionStore) Resolve(client string, hashes []string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictNolock()
	if len(hashes) == 0 {
		// nothing to match on; mint
		return s.mintNolock(client, hashes), true
	}
	candidates := s.byClient[client]
	var best *sessionEntry
	bestLen := -1
	for _, c := range candidates {
		if c.Len == 0 || len(hashes) < c.Len {
			continue
		}
		// The stored Tail covers absolute positions [c.Len-len(c.Tail), c.Len)
		// of the prior request. Those positions must be identical in the
		// incoming request for it to be a continuation.
		start := c.Len - len(c.Tail)
		if !slices.Equal(c.Tail, hashes[start:c.Len]) {
			continue
		}
		if c.Len > bestLen {
			best = c
			bestLen = c.Len
		}
	}
	if best != nil {
		best.Tail = s.tailWindow(hashes)
		best.Len = len(hashes)
		best.LastSeen = time.Now()
		return best.ID, false
	}
	return s.mintNolock(client, hashes), true
}

// tailWindow returns a clone of the trailing matchWindow hashes of the request
// (or all of them when the request is shorter than the window).
func (s *sessionStore) tailWindow(hashes []string) []string {
	start := 0
	if len(hashes) > s.matchWindow {
		start = len(hashes) - s.matchWindow
	}
	return slices.Clone(hashes[start:])
}

func (s *sessionStore) mintNolock(client string, hashes []string) string {
	id := uuid.New().String()
	entry := &sessionEntry{
		ID:       id,
		Tail:     s.tailWindow(hashes),
		Len:      len(hashes),
		LastSeen: time.Now(),
	}
	bucket := s.byClient[client]
	bucket = append(bucket, entry)
	if len(bucket) > s.maxPerClient {
		// drop the oldest
		bucket = bucket[len(bucket)-s.maxPerClient:]
	}
	s.byClient[client] = bucket
	return id
}

// evictNolock drops entries older than the TTL. Caller holds s.mu.
func (s *sessionStore) evictNolock() {
	if s.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-s.ttl)
	for k, bucket := range s.byClient {
		filtered := bucket[:0]
		for _, e := range bucket {
			if !e.LastSeen.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(s.byClient, k)
		} else {
			// Clear the tail we sliced off so the evicted *sessionEntry
			// pointers aren't retained via the backing array's capacity.
			clear(bucket[len(filtered):])
			s.byClient[k] = filtered
		}
	}
}

// Flush writes the in-memory state to disk (if persistence is enabled).
func (s *sessionStore) Flush() error {
	if s.persist == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictNolock()
	p := persistedSessions{
		Now: time.Now(),
	}
	for k, bucket := range s.byClient {
		for _, e := range bucket {
			p.Entries = append(p.Entries, persistedEntry{
				Client:   k,
				ID:       e.ID,
				Tail:     e.Tail,
				Len:      e.Len,
				LastSeen: e.LastSeen,
			})
		}
	}
	return s.persist.Write(&p)
}
