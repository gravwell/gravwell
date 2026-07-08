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
	s, err := newSessionStore(time.Hour, 0, "")
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
	s, _ := newSessionStore(time.Hour, 0, "")
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
	s, _ := newSessionStore(time.Hour, 0, "")
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
	s, _ := newSessionStore(time.Hour, 0, "")
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
	s, _ := newSessionStore(10*time.Millisecond, 0, "")
	s.Resolve("k1", []string{"sys", "user1"})
	time.Sleep(50 * time.Millisecond)
	_, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if !isNew {
		t.Error("session should be evicted after TTL and turn 2 should be new")
	}
}

// Clients are not obligated to alternate user/assistant turns. The OpenAI
// completions spec permits consecutive user messages, so a follow-up request
// that appends a second user turn (no assistant reply in between) must still
// continue the prior session rather than mint a new one.
func TestSessionConsecutiveUserMessages(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	id1, new1 := s.Resolve("k1", []string{"sys", "user1"})
	if !new1 {
		t.Fatal("turn 1 should be new")
	}
	// turn 2: a second user message stacked on the first with no assistant reply.
	id2, new2 := s.Resolve("k1", []string{"sys", "user1", "user2"})
	if new2 {
		t.Error("consecutive user messages should continue the session")
	}
	if id1 != id2 {
		t.Errorf("session id changed across consecutive user turns: %q -> %q", id1, id2)
	}
	// turn 3: yet another stacked user message.
	id3, new3 := s.Resolve("k1", []string{"sys", "user1", "user2", "user3"})
	if new3 {
		t.Error("third stacked user message should continue the session")
	}
	if id3 != id1 {
		t.Errorf("session id drifted across stacked user turns: %q vs %q", id3, id1)
	}
}

// The tracker must not assume exactly one new message was appended since the
// previous request. A client that interrupts and resends, or that stacks a
// tool result plus a follow-up user turn, appends several messages at once —
// all of which should still continue the prior session.
func TestSessionMultipleAppendedMessages(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	id1, _ := s.Resolve("k1", []string{"sys", "user1"})
	// Three new messages arrive in a single follow-up request.
	id2, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "tool1", "user2"})
	if isNew {
		t.Error("appending several messages at once should still continue the session")
	}
	if id1 != id2 {
		t.Errorf("session id changed when multiple messages were appended: %q -> %q", id1, id2)
	}
}

// The match window bounds how far back the tracker looks, but a normal
// turn-by-turn continuation (which only extends the tail) is still matched even
// with a small window, because the prior request's trailing messages are
// carried forward unchanged.
func TestSessionMatchWindowContinuation(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 2, "")
	id1, _ := s.Resolve("k1", []string{"sys", "user1"})
	id2, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if isNew || id1 != id2 {
		t.Fatalf("small window should still track a continuation (%q vs %q, new=%v)", id1, id2, isNew)
	}
	// And a genuinely different conversation still mints a new session.
	id3, isNew := s.Resolve("k1", []string{"otherSys", "userA"})
	if !isNew || id3 == id1 {
		t.Errorf("divergent conversation should mint a new session even with a small window")
	}
}

// A client may open a conversation that does not begin with a user message —
// e.g. replaying an existing transcript that starts with an assistant turn or
// leading with a tool result. The tracker should treat these like any other
// prefix and continue the session on follow-up.
func TestSessionNonUserFirstMessage(t *testing.T) {
	cases := [][]string{
		{"asst0", "user1"}, // starts with an assistant message
		{"tool0", "user1"}, // starts with a tool result
	}
	for _, first := range cases {
		s, _ := newSessionStore(time.Hour, 0, "")
		id1, new1 := s.Resolve("k1", first)
		if !new1 {
			t.Fatalf("%v: first request should mint a new session", first)
		}
		cont := append(append([]string{}, first...), "asst1", "user2")
		id2, new2 := s.Resolve("k1", cont)
		if new2 {
			t.Errorf("%v: follow-up should continue the session", first)
		}
		if id1 != id2 {
			t.Errorf("%v: session id changed across turns: %q -> %q", first, id1, id2)
		}
	}
}

// Distinct single-message requests are independent conversations and must each
// mint their own session. An identical single-message request, on the other
// hand, is indistinguishable from a retry of the first, so it resolves to the
// same session.
func TestSessionSingleMessage(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	id1, new1 := s.Resolve("k1", []string{"user1"})
	if !new1 {
		t.Error("first single-message request should be new")
	}
	// A different single message is a different conversation.
	id2, new2 := s.Resolve("k1", []string{"userX"})
	if !new2 {
		t.Error("a different single-message request should mint a new session")
	}
	if id1 == id2 {
		t.Error("distinct single-message requests must not share a session id")
	}
	// An identical repeat is treated as a retry of the first request.
	id3, new3 := s.Resolve("k1", []string{"user1"})
	if new3 {
		t.Error("identical single-message repeat should resolve to the existing session")
	}
	if id3 != id1 {
		t.Errorf("retry should reuse the original session id: %q vs %q", id3, id1)
	}
}

// An empty message list (a client sending no messages at all) must not panic
// and should always mint a new session.
func TestSessionEmptyHashesMints(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	id1, new1 := s.Resolve("k1", nil)
	if !new1 || id1 == "" {
		t.Error("empty request should mint a new session")
	}
	id2, new2 := s.Resolve("k1", []string{})
	if !new2 || id2 == "" {
		t.Error("empty-slice request should mint a new session")
	}
}

// A client that resubmits an already-seen conversation whose tail is an
// assistant turn (rather than a fresh user message) should still be recognized
// as the same session — the stored prefix is fully contained in the request.
func TestSessionResendEndingInAssistant(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	id1, _ := s.Resolve("k1", []string{"sys", "user1"})
	// The client re-sends the whole convo including the assistant reply, with
	// no new user turn appended.
	id2, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1"})
	if isNew {
		t.Error("resend of the same convo should continue the session")
	}
	if id1 != id2 {
		t.Errorf("session id changed on resend: %q -> %q", id1, id2)
	}
}

// The longest matching stored prefix should win when a client has several
// overlapping conversations open, so branches don't steal each other's ids.
func TestSessionLongestPrefixWins(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	// Two turns deep on one conversation.
	short, _ := s.Resolve("k1", []string{"sys", "user1"})
	long, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2"})
	if isNew || short != long {
		t.Fatalf("expected turn 2 to continue the same session (%q vs %q)", short, long)
	}
	// A new turn that extends the deeper conversation should match the longer
	// stored prefix, keeping the same id.
	id, isNew := s.Resolve("k1", []string{"sys", "user1", "asst1", "user2", "asst2", "user3"})
	if isNew {
		t.Error("deep continuation should not mint a new session")
	}
	if id != short {
		t.Errorf("deep continuation drifted to a different session: %q vs %q", id, short)
	}
}

// Minting past the per-client cap drops the oldest sessions so a single client
// cannot grow memory without bound.
func TestSessionMaxPerClientCapOnMint(t *testing.T) {
	s, _ := newSessionStore(time.Hour, 0, "")
	s.maxPerClient = 4
	for i := range 10 {
		// Each single-message request mints a distinct session.
		s.Resolve("k1", []string{"m" + string(rune('a'+i))})
	}
	if got := len(s.byClient["k1"]); got != s.maxPerClient {
		t.Errorf("expected bucket capped at %d, got %d", s.maxPerClient, got)
	}
}

// The same cap must be applied when restoring from disk so a large or tampered
// state file cannot restore an unbounded per-client bucket.
func TestSessionMaxPerClientCapOnLoad(t *testing.T) {
	s := &sessionStore{
		byClient:     map[string][]*sessionEntry{},
		ttl:          time.Hour,
		maxPerClient: 4,
		matchWindow:  defaultMatchWindow,
	}
	now := time.Now()
	p := &persistedSessions{Now: now}
	for i := range 20 {
		p.Entries = append(p.Entries, persistedEntry{
			Client:   "k1",
			ID:       "id" + string(rune('a'+i)),
			Tail:     []string{"h" + string(rune('a'+i))},
			Len:      1,
			LastSeen: now,
		})
	}
	s.load(p)
	if got := len(s.byClient["k1"]); got != s.maxPerClient {
		t.Errorf("expected loaded bucket capped at %d, got %d", s.maxPerClient, got)
	}
	// The newest entries (the tail) should be the ones retained.
	last := s.byClient["k1"][s.maxPerClient-1]
	if last.ID != "id"+string(rune('a'+19)) {
		t.Errorf("cap should keep the newest entries; got tail id %q", last.ID)
	}
}

func TestSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "sessions.state")

	s1, err := newSessionStore(time.Hour, 0, statePath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	id1, _ := s1.Resolve("k1", []string{"sys", "user1"})
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Reload — should find the prior session.
	s2, err := newSessionStore(time.Hour, 0, statePath)
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
