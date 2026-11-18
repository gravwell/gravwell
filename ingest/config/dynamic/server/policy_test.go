/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package server

import (
	"testing"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// The since rule used to be checked through a database, which meant every case needed two
// reports, a real clock and a tolerance.  Checked here it needs neither: the clock is an
// argument, so "carried forward" is an equality rather than a comparison that passes on a
// fast machine.  TestStore still covers it through storage, because a backend can get the
// rule right in the merge and lose it on the way to disk.

var (
	t0 = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Minute)
)

func TestMergeStatusesFresh(t *testing.T) {
	ing, runner := uuid.New(), uuid.New()
	out := MergeStatuses(nil, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `tester`, Name: `beat`, Error: `boom`},
	}, ing, t0)

	if len(out) != 1 {
		t.Fatalf("one report produced %d rows", len(out))
	}
	row := out[0]
	if row.Runner != runner || row.Ingester != ing {
		t.Errorf("row is %v from %v, want %v from %v", row.Runner, row.Ingester, runner, ing)
	}
	if row.Kind != `tester` || row.Name != `beat` || row.Error != `boom` {
		t.Errorf("row carries %s/%s %q", row.Kind, row.Name, row.Error)
	}
	if !row.Since.Equal(t0) || !row.Updated.Equal(t0) {
		t.Errorf("a first report is stamped since %v updated %v, want both %v", row.Since, row.Updated, t0)
	}
	if row.OK() {
		t.Error("a row carrying an error reports OK")
	}
}

// TestMergeStatusesSince is the whole reason this function exists: since answers "how long
// has this been broken" and only a report that says the same thing may keep it.
func TestMergeStatusesSince(t *testing.T) {
	ing, runner := uuid.New(), uuid.New()
	prior := []StatusRow{{Runner: runner, Ingester: ing, Error: `boom`, Since: t0, Updated: t0}}

	for _, tc := range []struct {
		name  string
		now   string
		since time.Time
	}{
		{`unchanged error keeps the clock running`, `boom`, t0},
		{`a different error restarts it`, `bang`, t1},
		{`coming good restarts it`, ``, t1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := MergeStatuses(prior, []dynamic.RunnerStatus{
				{UUID: runner, Error: tc.now},
			}, ing, t1)
			if len(out) != 1 {
				t.Fatalf("got %d rows", len(out))
			}
			if !out[0].Since.Equal(tc.since) {
				t.Errorf("since is %v, want %v", out[0].Since, tc.since)
			}
			if !out[0].Updated.Equal(t1) {
				t.Errorf("updated is %v, want the report's clock %v", out[0].Updated, t1)
			}
		})
	}
}

// TestMergeStatusesReplaces covers the set semantics: what comes back is the report, not
// the report added to what was held.  A runner no longer assigned to this ingester stops
// being mentioned and its row simply goes, with nothing having to send a retraction.
func TestMergeStatusesReplaces(t *testing.T) {
	ing, kept, dropped := uuid.New(), uuid.New(), uuid.New()
	prior := []StatusRow{
		{Runner: kept, Ingester: ing, Error: `boom`, Since: t0, Updated: t0},
		{Runner: dropped, Ingester: ing, Error: `boom`, Since: t0, Updated: t0},
	}
	out := MergeStatuses(prior, []dynamic.RunnerStatus{{UUID: kept, Error: `boom`}}, ing, t1)
	if len(out) != 1 {
		t.Fatalf("a one-runner report produced %d rows, the prior set was merged in", len(out))
	}
	if out[0].Runner != kept {
		t.Errorf("the surviving row is %v, want %v", out[0].Runner, kept)
	}
	// an empty report empties the set, which is how an ingester that has been handed
	// nothing stops showing stale verdicts
	if out = MergeStatuses(prior, nil, ing, t1); len(out) != 0 {
		t.Errorf("an empty report produced %d rows", len(out))
	}
}

// TestMergeStatusesKeyless covers a status with no runner UUID: there is nothing to key it
// on, the ingester has already logged it, and it is dropped rather than stored under the
// nil UUID where it would collide with every other keyless report.
func TestMergeStatusesKeyless(t *testing.T) {
	ing := uuid.New()
	out := MergeStatuses(nil, []dynamic.RunnerStatus{
		{Kind: `tester`, Name: `keyless`},
		{UUID: uuid.New(), Kind: `tester`, Name: `real`},
	}, ing, t0)
	if len(out) != 1 {
		t.Fatalf("got %d rows, want only the keyed one", len(out))
	}
	if out[0].Name != `real` {
		t.Errorf("the surviving row is %q", out[0].Name)
	}
}

// TestMergeStatusesIngester covers the identity coming from the argument rather than from
// the report.  The caller has it from an authenticated session, and a status that could
// name its own ingester would let one plant a failure against another's name.
func TestMergeStatusesIngester(t *testing.T) {
	ing := uuid.New()
	out := MergeStatuses(nil, []dynamic.RunnerStatus{{UUID: uuid.New()}}, ing, t0)
	if len(out) != 1 {
		t.Fatalf("got %d rows", len(out))
	}
	if out[0].Ingester != ing {
		t.Errorf("row attributed to %v, want %v", out[0].Ingester, ing)
	}
}

func TestNormalizeKind(t *testing.T) {
	if _, err := NormalizeKind(dynamic.RunnerDefinition{}); err == nil {
		t.Error("a registration with no kind was accepted")
	}
	in := dynamic.RunnerDefinition{Kind: `tester`, Name: `stray`, UUID: uuid.New()}
	out, err := NormalizeKind(in)
	if err != nil {
		t.Fatalf("failed to normalize: %v", err)
	}
	if out.Kind != `tester` {
		t.Errorf("kind became %q", out.Kind)
	}
	if out.Name != `` || out.UUID != uuid.Nil() {
		t.Errorf("identity survived normalizing: name %q uuid %v", out.Name, out.UUID)
	}
	// the caller's copy is untouched, it may still be holding something it needs
	if in.Name != `stray` || in.UUID == uuid.Nil() {
		t.Error("normalizing mutated the caller's definition")
	}
}
