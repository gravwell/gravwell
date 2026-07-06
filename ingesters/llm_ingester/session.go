/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

// sessionEntry is one tracked conversation, identified by the sequence of
// message hashes the client most recently sent. We only store hashes (not
// content) to keep memory bounded and to avoid persisting prompt text to disk.
type sessionEntry struct {
	ID       string
	Hashes   []string
	LastSeen time.Time
}

// sessionStore performs auto-derived session matching for stateless chat APIs.
// Sessions are scoped by API-key hash so traffic from different keys never
// cross-matches.
type sessionStore struct {
	mu      sync.Mutex
	byKey   map[string][]*sessionEntry
	ttl     time.Duration
	persist *utils.State

	// MatchResult / cap configuration
	maxPerKey int
}

// persistedSessions is the on-disk form. We can change the in-memory layout
// later without breaking the on-disk schema by serialising this fixed shape.
type persistedSessions struct {
	Version int
	Now     time.Time
	Entries []persistedEntry
}

type persistedEntry struct {
	APIKey   string
	ID       string
	Hashes   []string
	LastSeen time.Time
}

const sessionStoreVersion = 1

func newSessionStore(ttl time.Duration, statePath string) (*sessionStore, error) {
	s := &sessionStore{
		byKey:     map[string][]*sessionEntry{},
		ttl:       ttl,
		maxPerKey: 256,
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
		entry := &sessionEntry{
			ID:       e.ID,
			Hashes:   append([]string(nil), e.Hashes...),
			LastSeen: e.LastSeen,
		}
		s.byKey[e.APIKey] = append(s.byKey[e.APIKey], entry)
	}
}

// Resolve looks up or mints a session ID for the incoming request.
// hashes is the canonical per-message hash sequence (latest message last).
// It returns the session ID and a boolean indicating whether this was a new
// session (no matching prefix found).
func (s *sessionStore) Resolve(apiKeyHash string, hashes []string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictLocked()
	if len(hashes) == 0 {
		// nothing to match on; mint
		return s.mintLocked(apiKeyHash, hashes), true
	}
	// We compare against hashes[:n-1] (everything except the just-added user
	// turn). The previous request's stored sequence should be a prefix of that.
	prefix := hashes[:len(hashes)-1]
	candidates := s.byKey[apiKeyHash]
	var best *sessionEntry
	bestLen := -1
	for _, c := range candidates {
		n := matchLen(c.Hashes, prefix)
		// require at least one matching message; otherwise this is a new
		// conversation that happens to start the same way as the API key's
		// first ever turn — still mint a new session.
		if n > 0 && n >= len(c.Hashes) && n > bestLen {
			best = c
			bestLen = n
		}
	}
	if best != nil {
		best.Hashes = append([]string(nil), hashes...)
		best.LastSeen = time.Now()
		return best.ID, false
	}
	return s.mintLocked(apiKeyHash, hashes), true
}

// matchLen returns the length of the common prefix between a (a stored session
// hash list) and b (the incoming request prefix), but only counts as "matching"
// if a is entirely consumed (i.e. a is a prefix of b).
func matchLen(a, b []string) int {
	if len(a) > len(b) {
		return 0
	}
	for i := range a {
		if a[i] != b[i] {
			return 0
		}
	}
	return len(a)
}

func (s *sessionStore) mintLocked(apiKeyHash string, hashes []string) string {
	id := uuid.New().String()
	entry := &sessionEntry{
		ID:       id,
		Hashes:   append([]string(nil), hashes...),
		LastSeen: time.Now(),
	}
	bucket := s.byKey[apiKeyHash]
	bucket = append(bucket, entry)
	if len(bucket) > s.maxPerKey {
		// drop the oldest
		bucket = bucket[len(bucket)-s.maxPerKey:]
	}
	s.byKey[apiKeyHash] = bucket
	return id
}

// UpdateAfterResponse records the full message-hash sequence (request hashes
// plus a synthetic assistant-turn hash, optional) so the next request from the
// same conversation can match. The simplest correct implementation is to do
// nothing here — Resolve already saved the incoming prefix, and the next
// request will arrive carrying the assistant turn appended by the client,
// which we'll match as a prefix of the next "everything but the latest".
// We keep this method as a hook for protocols where the assistant's reply
// hashes need to be reconstructed server-side.
func (s *sessionStore) UpdateAfterResponse(_ string, _ []string) {}

// evictLocked drops entries older than the TTL. Caller holds s.mu.
func (s *sessionStore) evictLocked() {
	if s.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-s.ttl)
	for k, bucket := range s.byKey {
		filtered := bucket[:0]
		for _, e := range bucket {
			if !e.LastSeen.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(s.byKey, k)
		} else {
			s.byKey[k] = filtered
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
	s.evictLocked()
	p := persistedSessions{
		Version: sessionStoreVersion,
		Now:     time.Now(),
	}
	for k, bucket := range s.byKey {
		for _, e := range bucket {
			p.Entries = append(p.Entries, persistedEntry{
				APIKey:   k,
				ID:       e.ID,
				Hashes:   e.Hashes,
				LastSeen: e.LastSeen,
			})
		}
	}
	return s.persist.Write(&p)
}
