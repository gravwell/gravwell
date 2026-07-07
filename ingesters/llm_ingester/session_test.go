/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSessionNewConversation(t *testing.T) {
	s, err := newSessionStore(time.Hour, "")
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	id, isNew := s.Resolve("k1", []string{"sys", "user1"})
	if !isNew {
		t.Error("first request should mint a new session")
	}
	if id == "" {
		t.Error("empty session id")
	}
}

func TestSessionContinuation(t *testing.T) {
	s, _ := newSessionStore(time.Hour, "")
	// turn 1: system + user1
	id1, new1 := s.Resolve("k1", []string{"sys", "user1"})
	if !new1 {
		t.Fatal("turn 1 should be new")
	}
	// turn 2: system + user1 + assistant1 + user2 — should match turn 1's stored prefix
	id2, new2 := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if new2 {
		t.Error("turn 2 should match existing session")
	}
	if id1 != id2 {
		t.Errorf("session id changed across turns: %q -> %q", id1, id2)
	}
	// turn 3: continues the chain
	id3, new3 := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2", "asst2", "user3"})
	if new3 {
		t.Error("turn 3 should match existing session")
	}
	if id3 != id1 {
		t.Errorf("session id drifted over multiple turns: %q vs %q", id3, id1)
	}
}

func TestSessionDifferentClientsIsolated(t *testing.T) {
	s, _ := newSessionStore(time.Hour, "")
	id1, _ := s.Resolve("client1", []string{"sys", "user1"})
	id2, isNew := s.Resolve("client2", []string{"sys", "user1", "asst1", "user2"})
	if !isNew {
		t.Error("different client with overlapping content must still mint a new session")
	}
	if id1 == id2 {
		t.Error("sessions across different clients must not share an id")
	}
}

func TestSessionBranchingMintsNew(t *testing.T) {
	s, _ := newSessionStore(time.Hour, "")
	s.Resolve("k1", []string{"sys", "user1"})
	// A divergent conversation starting with a different system prompt should mint a new session.
	id2, isNew := s.Resolve("k1", []string{"otherSys", "userA"})
	if !isNew {
		t.Error("divergent conversation should be a new session")
	}
	if id2 == "" {
		t.Error("empty id")
	}
}

func TestSessionTTLEviction(t *testing.T) {
	s, _ := newSessionStore(10*time.Millisecond, "")
	s.Resolve("k1", []string{"sys", "user1"})
	time.Sleep(50 * time.Millisecond)
	_, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if !isNew {
		t.Error("session should be evicted after TTL and turn 2 should be new")
	}
}

func TestSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "sessions.state")

	s1, err := newSessionStore(time.Hour, statePath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	id1, _ := s1.Resolve("k1", []string{"sys", "user1"})
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Reload — should find the prior session.
	s2, err := newSessionStore(time.Hour, statePath)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	id2, isNew := s2.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if isNew {
		t.Error("session should have been restored from disk")
	}
	if id1 != id2 {
		t.Errorf("session id changed across restart: %q vs %q", id1, id2)
	}
}
