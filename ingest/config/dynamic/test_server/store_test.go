/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/server"
)

func newStore(_ *testing.T) *Store { return NewStore() }

// only returns the single status held for a runner, failing if there is not exactly one.
func only(t *testing.T, s *Store, runner uuid.UUID) server.StatusRow {
	t.Helper()
	rows, err := s.RunnerStatuses(runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("runner %v has %d statuses, want 1", runner, len(rows))
	}
	return rows[0]
}

// TestReplaceStatuses covers the replace semantics the whole design leans on: a report is
// the complete truth for one ingester, so clearing needs no retraction, and one ingester
// can never speak for another.
func TestReplaceStatuses(t *testing.T) {
	s := newStore(t)
	inA, inB := uuid.New(), uuid.New()
	r1, r2 := uuid.New(), uuid.New()

	// A reports one failing runner and one clean one
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{
		{UUID: r1, Kind: `Tester`, Name: `beat`, Error: `missing unit in duration "3"`},
		{UUID: r2, Kind: `Tester`, Name: `other`},
	}); err != nil {
		t.Fatal(err)
	}
	bad := only(t, s, r1)
	if bad.OK() || bad.Ingester != inA || bad.Name != `beat` {
		t.Fatalf("bad status not recorded: %+v", bad)
	}
	if only(t, s, r2).OK() != true {
		t.Error(`a clean runner should be recorded as clean, not left absent`)
	}
	firstSeen := bad.Since

	// the same failure again keeps its clock running, so a row can answer "for how long"
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{
		{UUID: r1, Kind: `Tester`, Name: `beat`, Error: `missing unit in duration "3"`},
		{UUID: r2, Kind: `Tester`, Name: `other`},
	}); err != nil {
		t.Fatal(err)
	}
	again := only(t, s, r1)
	if !again.Since.Equal(firstSeen) {
		t.Errorf("an unchanged failure restarted its clock: %v then %v", firstSeen, again.Since)
	}
	if !again.Updated.After(firstSeen) && again.Updated.Before(firstSeen) {
		t.Error(`the report time should move even when the state does not`)
	}

	// a different message is a different state and does start the clock again
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{
		{UUID: r1, Kind: `Tester`, Name: `beat`, Error: `something else entirely`},
	}); err != nil {
		t.Fatal(err)
	}
	if changed := only(t, s, r1); changed.Since.Equal(firstSeen) {
		t.Error(`a different failure should start the clock again`)
	}

	// r2 was not in that report, so A has stopped carrying it and its row is gone
	if rows, err := s.RunnerStatuses(r2); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Errorf("a runner left out of a report kept %d rows", len(rows))
	}

	// B reporting clean does not touch what A said, they are separate opinions
	if err := s.ReplaceStatuses(inB, []dynamic.RunnerStatus{{UUID: r1, Kind: `Tester`, Name: `beat`}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.RunnerStatuses(r1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("two ingesters reported, got %d rows", len(rows))
	}
	// the failing one sorts first, an operator opened the page to read that
	if rows[0].OK() || rows[0].Ingester != inA {
		t.Errorf("the failure should sort first, got %+v", rows[0])
	}
	if !rows[1].OK() || rows[1].Ingester != inB {
		t.Errorf("the clean report is wrong: %+v", rows[1])
	}

	// A comes good, and the error clears with nothing having to retract it
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{{UUID: r1, Kind: `Tester`, Name: `beat`}}); err != nil {
		t.Fatal(err)
	}
	if rows, err = s.RunnerStatuses(r1); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.OK() {
			t.Errorf("an error survived a clean report: %+v", row)
		}
	}

	// an ingester with nothing to say clears itself entirely
	if err := s.ReplaceStatuses(inA, nil); err != nil {
		t.Fatal(err)
	}
	if rows, err = s.RunnerStatuses(r1); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 || rows[0].Ingester != inB {
		t.Errorf("an empty report should drop only its own rows, got %+v", rows)
	}

	// a report from nobody is refused rather than filed under the nil UUID
	if err = s.ReplaceStatuses(uuid.Nil(), nil); err == nil {
		t.Error(`a status report with no ingester UUID should be refused`)
	}
}

// TestDeleteRunnerClearsStatuses covers the one case a report can never clean up after:
// once the runner is gone no ingester will ever mention it again, so a stale error would
// sit on the screen forever.
func TestDeleteRunnerClearsStatuses(t *testing.T) {
	s := newStore(t)
	ingester, runner := uuid.New(), uuid.New()
	rd, err := dynamic.MapRunnerDefinition(`Tester`, `beat`, struct {
		Ingester_UUID string
		Interval      string
	}{Ingester_UUID: runner.String(), Interval: `3`})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutRunner(rd); err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceStatuses(ingester, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `Tester`, Name: `beat`, Error: `boom`},
	}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.RunnerStatuses(runner); len(rows) != 1 {
		t.Fatalf("setup failed, got %d rows", len(rows))
	}
	if err = s.DeleteRunner(runner); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.RunnerStatuses(runner); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Errorf("deleting a runner left %d statuses behind", len(rows))
	}
}

// TestRollUp covers the reduction the list draws from.  One ingester failing is the whole
// runner failing, never having been reported on is not the same as being fine, and a
// report from a machine that is no longer connected is the last thing it said rather than
// news.
func TestRollUp(t *testing.T) {
	ok := server.StatusRow{}
	bad := server.StatusRow{Error: `missing unit in duration "3"`}

	// nothing registered can run it: not a silence to wait out, an assignment that points
	// at nothing
	if state, detail := rollUp(nil, 0, 0); state != stateUnknown || !strings.Contains(detail, `no registered ingester`) {
		t.Errorf("an unmatchable assignment rolled up to %q %q", state, detail)
	}
	// tasked but silent, which is what a runner created while the fleet is down looks
	// like.  The server knows where it is meant to go and says so.
	state, detail := rollUp(nil, 2, 0)
	if state != stateUnknown || !strings.Contains(detail, `tasked to 2 ingesters`) {
		t.Errorf("a tasked but unreported runner rolled up to %q %q", state, detail)
	}
	if !strings.Contains(detail, `none connected`) {
		t.Errorf("the detail does not say nothing is connected: %q", detail)
	}

	if state, _ = rollUp([]server.StatusRow{ok, ok}, 2, 2); state != stateOK {
		t.Errorf("two clean reports rolled up to %q", state)
	}
	// a report that is not backed by a live connection is still shown, but marked
	if _, detail = rollUp([]server.StatusRow{ok}, 1, 0); !strings.Contains(detail, `none connected`) {
		t.Errorf("a stale acceptance reads as live: %q", detail)
	}
	// partially reported
	if state, detail = rollUp([]server.StatusRow{ok}, 3, 3); state != stateOK || !strings.Contains(detail, `1 of 3`) {
		t.Errorf("a partly reported runner rolled up to %q %q", state, detail)
	}

	state, detail = rollUp([]server.StatusRow{bad}, 1, 1)
	if state != stateBad || !strings.Contains(detail, bad.Error) {
		t.Errorf("a single failure rolled up to %q %q, want the plugin's own words", state, detail)
	}
	// the mixed case is the one that matters, a green light here would be a lie
	if state, detail = rollUp([]server.StatusRow{ok, bad, ok}, 3, 3); state != stateBad {
		t.Errorf("one failure among three rolled up to %q, want %q", state, stateBad)
	} else if !strings.Contains(detail, `1 of 3 reports`) || !strings.Contains(detail, bad.Error) {
		t.Errorf("the mixed summary does not say who or why: %q", detail)
	}
}

// TestDeleteRunnerIsAtomic covers the pairing that has to hold: once the runner is gone
// no ingester will ever mention it again, so a status left behind can never be cleared by
// anything.  The two deletes therefore have to happen together or not at all.
func TestDeleteRunnerIsAtomic(t *testing.T) {
	s := newStore(t)
	ingester := uuid.New()
	live, other := uuid.New(), uuid.New()

	put := func(id uuid.UUID, name string) {
		t.Helper()
		rd, err := dynamic.MapRunnerDefinition(`Tester`, name, struct {
			Ingester_UUID string
			Interval      string
		}{Ingester_UUID: id.String(), Interval: `1s`})
		if err != nil {
			t.Fatal(err)
		}
		rd.UUID = id
		if err = s.PutRunner(rd); err != nil {
			t.Fatal(err)
		}
	}
	put(live, `live`)
	put(other, `other`)
	if err := s.ReplaceStatuses(ingester, []dynamic.RunnerStatus{
		{UUID: live, Kind: `Tester`, Name: `live`, Error: `boom`},
		{UUID: other, Kind: `Tester`, Name: `other`},
	}); err != nil {
		t.Fatal(err)
	}

	// deleting a runner that is not there must change nothing at all, not even partially
	if err := s.DeleteRunner(uuid.New()); err == nil {
		t.Error(`deleting an absent runner reported success`)
	}
	if rows, _ := s.RunnerStatuses(live); len(rows) != 1 {
		t.Errorf("a failed delete disturbed another runner's statuses: %d rows", len(rows))
	}
	if runners, _ := s.Runners(); len(runners) != 2 {
		t.Errorf("a failed delete removed a runner: %d left", len(runners))
	}

	// and a real delete takes the statuses with it, leaving the other one alone
	if err := s.DeleteRunner(live); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.RunnerStatuses(live); err != nil {
		t.Fatal(err)
	} else if len(rows) != 0 {
		t.Errorf("the deleted runner kept %d statuses, which nothing can ever clear", len(rows))
	}
	if rows, err := s.RunnerStatuses(other); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 {
		t.Errorf("deleting one runner took another's statuses: %d rows", len(rows))
	}
}

// TestStoreConformance runs the interface's own suite against the SQLite backing.  It is
// what keeps this store and the webserver's from drifting: a rule either backend gets
// wrong is caught here rather than six months later on somebody's status page.
func TestStoreConformance(t *testing.T) {
	server.TestStore(t, func(_ *testing.T) server.Store {
		// a fresh store per case, the suite requires each one to be independent of the
		// last and several of these check what a store holds in total
		return NewStore()
	})
}
