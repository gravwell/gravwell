/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package server

// TestStore is the conformance suite for the Store interface, and lives in a regular file
// rather than a _test.go one because a _test.go file is not importable: the whole point is
// that a backend in another package runs it.  Go's own testing package registers its flags
// from testing.Init rather than from an init function, so importing it here costs an
// importer nothing but the dependency.

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// TestStore runs every rule the Store interface states against one implementation.
//
// newStore must hand back an empty store, ready to use, and register whatever cleanup it
// needs with t.Cleanup.  It is called several times in one run, so each call has to be
// independent of the last: a SQLite backend wants a fresh file per call, not a fresh
// handle on the same one.
//
// Two of the checks cover things no single backend would think to get wrong on its own but
// that a second backend gets wrong immediately, and they are the reason this exists:
// replace-not-merge on both write paths, and the since carry-forward. Both are the
// difference between a screen an operator can trust and one with a stale red mark on it.
//
// Two behaviours are deliberately NOT asserted here, because they are the backend's to
// state rather than the interface's:
//
//   - Which definition Kinds returns when two ingesters have registered the same kind.
//     The suite only requires that exactly one comes back per kind. A consumer that needs
//     to know which reads IngesterKinds instead.
//   - Ordering beyond what the interface documents. Runners and Kinds are checked as sets.
func TestStore(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	for _, tc := range []struct {
		name string
		fn   func(*testing.T, Store)
	}{
		{`Empty`, testStoreEmpty},
		{`ReplaceKindsReplaces`, testStoreReplaceKindsReplaces},
		{`ReplaceKindsStampsIngester`, testStoreReplaceKindsStampsIngester},
		{`KindLookup`, testStoreKindLookup},
		{`KindMetadata`, testStoreKindMetadata},
		{`KindIngesters`, testStoreKindIngesters},
		{`RunnerRoundTrip`, testStoreRunnerRoundTrip},
		{`RunnerNameUniqueness`, testStoreRunnerNameUniqueness},
		{`DeleteRunner`, testStoreDeleteRunner},
		{`ReplaceStatusesReplaces`, testStoreReplaceStatusesReplaces},
		{`StatusSince`, testStoreStatusSince},
		{`ForgetIngester`, testStoreForgetIngester},
		{`ForgetIngesterUnknown`, testStoreForgetIngesterUnknown},
		{`ForgetIngesterOrphans`, testStoreForgetIngesterOrphans},
		{`ForgetIngesterThenReRegister`, testStoreForgetIngesterThenReRegister},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

// --- fixtures -------------------------------------------------------------------------

// kindDef is a registration prototype: a kind, no identity.
func kindDef(kind string) dynamic.RunnerDefinition {
	return dynamic.RunnerDefinition{
		Kind: kind,
		Variables: []dynamic.Variable{
			// the ValueType constants are unexported, so the literal is spelled out.
			// Storage only has to round-trip it, it is not interpreted here.
			{Name: `Target`, Type: dynamic.ValueType(`string`)},
		},
	}
}

// runnerDef is a configured runner: a kind, a name and an identity.
func runnerDef(kind, name string, id uuid.UUID) dynamic.RunnerDefinition {
	rd := kindDef(kind)
	rd.Name, rd.UUID = name, id
	return rd
}

func mustUUID(_ *testing.T) uuid.UUID { return uuid.New() }

// kindNames is the kinds a definition list covers, as a set, so a check does not depend on
// an ordering the interface never promised.
func kindNames(set []dynamic.RunnerDefinition) map[string]int {
	r := map[string]int{}
	for _, rd := range set {
		r[rd.Kind]++
	}
	return r
}

// statusOf is what one ingester says about one runner.
func statusOf(t *testing.T, s Store, runner, ingester uuid.UUID) (StatusRow, bool) {
	t.Helper()
	rows, err := s.RunnerStatuses(runner)
	if err != nil {
		t.Fatalf("failed to read the statuses of %v: %v", runner, err)
	}
	for _, row := range rows {
		if row.Ingester == ingester {
			return row, true
		}
	}
	return StatusRow{}, false
}

// --- the suite ------------------------------------------------------------------------

// testStoreEmpty covers a store nobody has written to. Every list is empty and nothing
// errors: a management interface draws this page before any ingester has ever connected.
func testStoreEmpty(t *testing.T, s Store) {
	if kinds, err := s.Kinds(); err != nil {
		t.Errorf("Kinds on an empty store failed: %v", err)
	} else if len(kinds) != 0 {
		t.Errorf("an empty store holds %d kinds", len(kinds))
	}
	if runners, err := s.Runners(); err != nil {
		t.Errorf("Runners on an empty store failed: %v", err)
	} else if len(runners) != 0 {
		t.Errorf("an empty store holds %d runners", len(runners))
	}
	if ings, err := s.Ingesters(); err != nil {
		t.Errorf("Ingesters on an empty store failed: %v", err)
	} else if len(ings) != 0 {
		t.Errorf("an empty store holds %d ingesters", len(ings))
	}
	if classes, err := s.Classes(); err != nil {
		t.Errorf("Classes on an empty store failed: %v", err)
	} else if len(classes) != 0 {
		t.Errorf("an empty store holds %d classes", len(classes))
	}
	if rows, err := s.Statuses(); err != nil {
		t.Errorf("Statuses on an empty store failed: %v", err)
	} else if len(rows) != 0 {
		t.Errorf("an empty store holds %d statuses", len(rows))
	}
	if md, err := s.KindMetadata(); err != nil {
		t.Errorf("KindMetadata on an empty store failed: %v", err)
	} else if len(md) != 0 {
		t.Errorf("an empty store holds metadata for %d kinds", len(md))
	}
}

// testStoreReplaceKindsReplaces is the rule a second backend gets wrong first: the second
// registration is the complete set, not an addition to the first. A kind dropped from a
// build has to disappear, otherwise it is offered as configurable forever and anything
// configured against it is never delivered.
func testStoreReplaceKindsReplaces(t *testing.T, s Store) {
	id := mustUUID(t)
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{
		kindDef(`tester`), kindDef(`retired`),
	}); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{
		kindDef(`tester`), kindDef(`sqs`),
	}); err != nil {
		t.Fatalf("failed to re-register: %v", err)
	}

	kinds, err := s.IngesterKinds(id)
	if err != nil {
		t.Fatalf("failed to read the kinds of %v: %v", id, err)
	}
	got := kindNames(kinds)
	if len(got) != 2 || got[`tester`] != 1 || got[`sqs`] != 1 {
		t.Fatalf("after re-registering, %v advertises %v, want tester and sqs", id, got)
	}
	if _, ok := got[`retired`]; ok {
		t.Error("a kind dropped from the build survived a re-registration, the set was merged rather than replaced")
	}
	if _, err = s.Kind(`retired`); !errors.Is(err, ErrNotFound) {
		t.Errorf("Kind(retired) after it was dropped: %v, want ErrNotFound", err)
	}

	// one entry per kind in the catalog, whichever definition that is
	if kinds, err = s.Kinds(); err != nil {
		t.Fatalf("failed to list kinds: %v", err)
	}
	for kind, n := range kindNames(kinds) {
		if n != 1 {
			t.Errorf("the catalog lists kind %s %d times, want once", kind, n)
		}
	}
}

// testStoreReplaceKindsStampsIngester covers the other half of a registration: the
// ingester itself is remembered, with its class and the time it was last heard from, so an
// interface can offer real assignment targets with the fleet switched off.
func testStoreReplaceKindsStampsIngester(t *testing.T, s Store) {
	id := mustUUID(t)
	before := time.Now()
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	ings, err := s.Ingesters()
	if err != nil {
		t.Fatalf("failed to list ingesters: %v", err)
	}
	if len(ings) != 1 {
		t.Fatalf("registering one ingester left %d of them", len(ings))
	}
	ing := ings[0]
	if ing.UUID != id {
		t.Errorf("stored ingester %v, want %v", ing.UUID, id)
	}
	if ing.Class != `hosted` {
		t.Errorf("stored class %q, want hosted", ing.Class)
	}
	if ing.LastSeen.Before(before.Add(-time.Second)) || ing.LastSeen.IsZero() {
		t.Errorf("last seen %v is not from this registration (started %v)", ing.LastSeen, before)
	}
	if len(ing.Kinds) != 1 || ing.Kinds[0] != `tester` {
		t.Errorf("the ingester advertises %v, want [tester]", ing.Kinds)
	}
	if classes, err := s.Classes(); err != nil {
		t.Errorf("failed to list classes: %v", err)
	} else if len(classes) != 1 || classes[0] != `hosted` {
		t.Errorf("classes are %v, want [hosted]", classes)
	}

	// a class that changes is followed, an ingester can be moved between them
	if err = s.ReplaceKinds(id, `http`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("failed to re-register under a new class: %v", err)
	}
	if ings, err = s.Ingesters(); err != nil {
		t.Fatalf("failed to list ingesters: %v", err)
	}
	if len(ings) != 1 {
		t.Fatalf("re-registering made a second ingester, %d in all", len(ings))
	} else if ings[0].Class != `http` {
		t.Errorf("class after the move is %q, want http", ings[0].Class)
	}

	// identity is required, a registration with no UUID is not filed under "nobody"
	if err = s.ReplaceKinds(uuid.Nil(), `hosted`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err == nil {
		t.Error("a registration with a nil ingester UUID was accepted")
	}
	// and so is a kind, it is the key everything else hangs off
	if err = s.ReplaceKinds(mustUUID(t), `hosted`, []dynamic.RunnerDefinition{{}}); err == nil {
		t.Error("a registration with no kind was accepted")
	}
}

// testStoreKindLookup covers Kind: a registration comes back with its identity stripped,
// and an unknown kind is ErrNotFound rather than a zero value a caller has to inspect.
func testStoreKindLookup(t *testing.T, s Store) {
	id := mustUUID(t)
	// identity on a registration is noise and must not be stored, see NormalizeKind
	dirty := kindDef(`tester`)
	dirty.Name, dirty.UUID = `should-not-persist`, mustUUID(t)
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{dirty}); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	rd, err := s.Kind(`tester`)
	if err != nil {
		t.Fatalf("failed to read kind tester: %v", err)
	}
	if rd.Kind != `tester` {
		t.Errorf("read back kind %q, want tester", rd.Kind)
	}
	if rd.Name != `` {
		t.Errorf("a stored registration kept the name %q", rd.Name)
	}
	if rd.UUID != uuid.Nil() {
		t.Errorf("a stored registration kept the UUID %v", rd.UUID)
	}
	if len(rd.Variables) != 1 || rd.Variables[0].Name != `Target` {
		t.Errorf("variables did not survive storage: %+v", rd.Variables)
	}
	if _, err = s.Kind(`nonesuch`); !errors.Is(err, ErrNotFound) {
		t.Errorf("Kind on an unregistered kind: %v, want ErrNotFound", err)
	}
}

// testStoreKindMetadata covers the absent-versus-empty rule: a kind that describes itself
// is in the map, a kind that does not is missing from it rather than present and empty.
// An interface uses that to tell "no icon" from "no metadata at all".
func testStoreKindMetadata(t *testing.T, s Store) {
	described := kindDef(`described`)
	described.Metadata = &dynamic.RunnerMetadata{
		Icon:          `<svg xmlns="http://www.w3.org/2000/svg"><rect width="8" height="8"/></svg>`,
		Documentation: []dynamic.DocLink{{Name: `guide`, Link: `https://example.invalid/guide`}},
		Version:       dynamic.Version(`1.2.3`),
	}
	if err := s.ReplaceKinds(mustUUID(t), `hosted`, []dynamic.RunnerDefinition{
		described, kindDef(`bare`),
	}); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	md, err := s.KindMetadata()
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	if _, ok := md[`bare`]; ok {
		t.Error("a kind with no metadata is present in the map, a caller cannot tell it apart from one that describes nothing")
	}
	got, ok := md[`described`]
	if !ok {
		t.Fatal("a kind that describes itself is missing from the metadata map")
	}
	if got == nil {
		t.Fatal("metadata for a described kind is nil")
	}
	if got.Icon == `` {
		t.Error("the icon did not survive storage")
	}
	if len(got.Documentation) != 1 || got.Documentation[0].Name != `guide` {
		t.Errorf("documentation did not survive storage: %+v", got.Documentation)
	}
	if want := dynamic.Version(`1.2.3`); got.Version != want {
		t.Errorf("version came back %v, want %v", got.Version, want)
	}
}

// testStoreKindIngesters covers the legal set of pin targets. Pinning a runner to an
// ingester that never registered the kind produces a configuration that is never
// delivered, so an interface offering targets has to ask which ingesters have it.
func testStoreKindIngesters(t *testing.T, s Store) {
	a, b := mustUUID(t), mustUUID(t)
	if err := s.ReplaceKinds(a, `hosted`, []dynamic.RunnerDefinition{
		kindDef(`tester`), kindDef(`sqs`),
	}); err != nil {
		t.Fatalf("failed to register %v: %v", a, err)
	}
	if err := s.ReplaceKinds(b, `http`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("failed to register %v: %v", b, err)
	}

	set, err := s.KindIngesters(`tester`)
	if err != nil {
		t.Fatalf("failed to list the ingesters for tester: %v", err)
	}
	seen := map[uuid.UUID]string{}
	for _, ing := range set {
		seen[ing.UUID] = ing.Class
	}
	if len(seen) != 2 || seen[a] != `hosted` || seen[b] != `http` {
		t.Errorf("tester is offered by %v, want both ingesters with their classes", seen)
	}

	if set, err = s.KindIngesters(`sqs`); err != nil {
		t.Fatalf("failed to list the ingesters for sqs: %v", err)
	}
	if len(set) != 1 || set[0].UUID != a {
		t.Errorf("sqs is offered by %v, want only %v", set, a)
	}
	if set, err = s.KindIngesters(`nonesuch`); err != nil {
		t.Errorf("listing the ingesters for an unregistered kind failed: %v", err)
	} else if len(set) != 0 {
		t.Errorf("an unregistered kind is offered by %d ingesters", len(set))
	}
}

// testStoreRunnerRoundTrip covers storing a configured runner: the UUID is the identity so
// an edit updates in place, metadata is dropped because it describes the plugin rather than
// this configuration of it, and an unknown UUID is ErrNotFound.
func testStoreRunnerRoundTrip(t *testing.T, s Store) {
	id := mustUUID(t)
	pinned := mustUUID(t)
	rd := runnerDef(`tester`, `first`, id)
	rd.Assigned = &dynamic.Assignment{UUIDs: []uuid.UUID{pinned}, Classes: []string{`hosted`}, Group: `east`}
	rd.Metadata = &dynamic.RunnerMetadata{Icon: `<svg xmlns="http://www.w3.org/2000/svg"/>`}
	if err := s.PutRunner(rd); err != nil {
		t.Fatalf("failed to store a runner: %v", err)
	}

	got, err := s.Runner(id)
	if err != nil {
		t.Fatalf("failed to read the runner back: %v", err)
	}
	if got.Kind != `tester` || got.Name != `first` || got.UUID != id {
		t.Errorf("read back %s/%s %v, want tester/first %v", got.Kind, got.Name, got.UUID, id)
	}
	if got.Metadata != nil {
		t.Error("metadata was stored on a runner, it belongs to the kind and would be shipped to every ingester on every poll")
	}
	if got.Assigned == nil {
		t.Fatal("the assignment did not survive storage")
	}
	if len(got.Assigned.UUIDs) != 1 || got.Assigned.UUIDs[0] != pinned {
		t.Errorf("pinned UUIDs came back %v, want [%v]", got.Assigned.UUIDs, pinned)
	}
	if len(got.Assigned.Classes) != 1 || got.Assigned.Classes[0] != `hosted` {
		t.Errorf("pinned classes came back %v, want [hosted]", got.Assigned.Classes)
	}
	if got.Assigned.Group != `east` {
		t.Errorf("pinned group came back %q, want east", got.Assigned.Group)
	}

	// the same UUID is an edit, not a second runner
	if err = s.PutRunner(runnerDef(`tester`, `renamed`, id)); err != nil {
		t.Fatalf("failed to update a runner: %v", err)
	}
	runners, err := s.Runners()
	if err != nil {
		t.Fatalf("failed to list runners: %v", err)
	}
	if len(runners) != 1 {
		t.Fatalf("editing a runner left %d of them", len(runners))
	}
	if runners[0].Name != `renamed` {
		t.Errorf("the edit did not take, the runner is still called %q", runners[0].Name)
	}

	if _, err = s.Runner(mustUUID(t)); !errors.Is(err, ErrNotFound) {
		t.Errorf("Runner on an unknown UUID: %v, want ErrNotFound", err)
	}

	// kind, name and UUID are all required, a runner missing any of them cannot be
	// addressed, delivered or told apart from another
	for _, bad := range []struct {
		why string
		rd  dynamic.RunnerDefinition
	}{
		{`no kind`, runnerDef(``, `nameless-kind`, mustUUID(t))},
		{`no name`, runnerDef(`tester`, ``, mustUUID(t))},
		{`no UUID`, runnerDef(`tester`, `no-uuid`, uuid.Nil())},
	} {
		if err = s.PutRunner(bad.rd); err == nil {
			t.Errorf("a runner with %s was accepted", bad.why)
		}
	}
}

// testStoreRunnerNameUniqueness covers the rule the interface states and a SQLite backend
// gets for free from an index: one name per kind. It is the same rule the ingester's own
// manager applies, and two runners of one kind sharing a name would write the same
// configuration file twice.
func testStoreRunnerNameUniqueness(t *testing.T, s Store) {
	first, second := mustUUID(t), mustUUID(t)
	if err := s.PutRunner(runnerDef(`tester`, `beat`, first)); err != nil {
		t.Fatalf("failed to store the first runner: %v", err)
	}
	if err := s.PutRunner(runnerDef(`tester`, `beat`, second)); err == nil {
		t.Error("two runners of one kind were allowed to share the name beat")
	}
	// the same name under a different kind is fine, kinds have their own namespaces
	if err := s.PutRunner(runnerDef(`sqs`, `beat`, second)); err != nil {
		t.Errorf("a name reused under another kind was refused: %v", err)
	}
	// and the original is still there, a refused write must not have disturbed it
	if got, err := s.Runner(first); err != nil {
		t.Errorf("the first runner is gone after a refused write: %v", err)
	} else if got.Name != `beat` {
		t.Errorf("the first runner is now called %q", got.Name)
	}
}

// testStoreDeleteRunner covers a deletion taking the statuses with it. Leaving them behind
// strands an error against a runner that no longer exists and nothing can ever clear it:
// an ingester only reports on runners it was handed, so it will never mention this one
// again.
func testStoreDeleteRunner(t *testing.T, s Store) {
	runner, ingester := mustUUID(t), mustUUID(t)
	if err := s.PutRunner(runnerDef(`tester`, `beat`, runner)); err != nil {
		t.Fatalf("failed to store a runner: %v", err)
	}
	if err := s.ReplaceStatuses(ingester, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `tester`, Name: `beat`, Error: `missing unit in duration "3"`},
	}); err != nil {
		t.Fatalf("failed to report a status: %v", err)
	}
	if rows, err := s.Statuses(); err != nil {
		t.Fatalf("failed to list statuses: %v", err)
	} else if len(rows) != 1 {
		t.Fatalf("one report left %d status rows", len(rows))
	}

	if err := s.DeleteRunner(runner); err != nil {
		t.Fatalf("failed to delete the runner: %v", err)
	}
	if rows, err := s.Statuses(); err != nil {
		t.Errorf("failed to list statuses after the deletion: %v", err)
	} else if len(rows) != 0 {
		t.Errorf("deleting a runner left %d of its statuses behind, and nothing can ever clear them", len(rows))
	}
	if _, err := s.Runner(runner); !errors.Is(err, ErrNotFound) {
		t.Errorf("the deleted runner reads back as %v, want ErrNotFound", err)
	}
	if err := s.DeleteRunner(runner); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting an unknown runner: %v, want ErrNotFound", err)
	}
}

// testStoreReplaceStatusesReplaces is the rule that makes a runner which has come good
// clear itself. The report is the complete set, so a clean report overwrites the error that
// was there and a runner no longer assigned to this ingester simply stops arriving. Nothing
// has to remember to send a retraction, which is the kind of thing that gets forgotten and
// leaves a stale red mark on a screen forever.
func testStoreReplaceStatusesReplaces(t *testing.T, s Store) {
	inA, inB := mustUUID(t), mustUUID(t)
	one, two := mustUUID(t), mustUUID(t)

	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{
		{UUID: one, Kind: `tester`, Name: `beat`, Error: `missing unit in duration "3"`},
		{UUID: two, Kind: `tester`, Name: `other`},
	}); err != nil {
		t.Fatalf("failed to report: %v", err)
	}
	if row, ok := statusOf(t, s, one, inA); !ok {
		t.Fatal("the failing runner has no status")
	} else if row.OK() {
		t.Error("a runner reported with an error reads back as OK")
	} else if row.Kind != `tester` || row.Name != `beat` {
		t.Errorf("the status names %s/%s, want tester/beat", row.Kind, row.Name)
	}

	// the runner comes good, and the report no longer mentions the other one at all
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{
		{UUID: one, Kind: `tester`, Name: `beat`},
	}); err != nil {
		t.Fatalf("failed to re-report: %v", err)
	}
	if row, ok := statusOf(t, s, one, inA); !ok {
		t.Fatal("the recovered runner lost its status entirely")
	} else if !row.OK() {
		t.Errorf("a runner reported clean still carries the error %q, the set was merged rather than replaced", row.Error)
	}
	if _, ok := statusOf(t, s, two, inA); ok {
		t.Error("a runner left out of the report kept its row, the set was merged rather than replaced")
	}

	// a second ingester's report is its own, replacing one must not touch the other
	if err := s.ReplaceStatuses(inB, []dynamic.RunnerStatus{
		{UUID: one, Kind: `tester`, Name: `beat`, Error: `cannot reach the endpoint`},
	}); err != nil {
		t.Fatalf("failed to report from the second ingester: %v", err)
	}
	if row, ok := statusOf(t, s, one, inA); !ok || !row.OK() {
		t.Error("the first ingester's verdict was disturbed by the second ingester reporting")
	}
	if row, ok := statusOf(t, s, one, inB); !ok {
		t.Error("the second ingester's report was not recorded")
	} else if row.OK() {
		t.Error("the second ingester's error was recorded as OK")
	}

	// identity is required here too
	if err := s.ReplaceStatuses(uuid.Nil(), nil); err == nil {
		t.Error("a status report with a nil ingester UUID was accepted")
	}
	// a status with no runner UUID has nothing to key it on and is dropped, not stored
	// and not an error: the ingester has already logged it
	if err := s.ReplaceStatuses(inA, []dynamic.RunnerStatus{{Kind: `tester`, Name: `keyless`}}); err != nil {
		t.Errorf("a report holding a keyless status failed outright: %v", err)
	}
	for _, row := range mustStatuses(t, s) {
		if row.Runner == uuid.Nil() {
			t.Error("a status with no runner UUID was stored")
		}
	}
}

// testStoreStatusSince covers the carry-forward. A row has to answer "how long has this
// been broken" and not just "is it broken now", so since survives a report that says the
// same thing and restarts on one that does not. Both stamps are the server's: an operator
// comparing two ingesters needs one clock rather than two that disagree by however wrong
// those hosts are.
func testStoreStatusSince(t *testing.T, s Store) {
	ingester, runner := mustUUID(t), mustUUID(t)
	const boom = `missing unit in duration "3"`

	report := func(msg string) StatusRow {
		t.Helper()
		if err := s.ReplaceStatuses(ingester, []dynamic.RunnerStatus{
			{UUID: runner, Kind: `tester`, Name: `beat`, Error: msg},
		}); err != nil {
			t.Fatalf("failed to report: %v", err)
		}
		row, ok := statusOf(t, s, runner, ingester)
		if !ok {
			t.Fatal("the report left no status row")
		}
		return row
	}

	first := report(boom)
	if first.Since.IsZero() || first.Updated.IsZero() {
		t.Fatalf("a fresh status has no stamps: since %v, updated %v", first.Since, first.Updated)
	}
	if first.Updated.Before(first.Since) {
		t.Errorf("updated %v is before since %v", first.Updated, first.Since)
	}

	// the same error again: the clock keeps running, but the report is fresh
	same := report(boom)
	if !same.Since.Equal(first.Since) {
		t.Errorf("an unchanged error restarted since: %v then %v, so a row cannot say how long it has been broken",
			first.Since, same.Since)
	}
	if same.Updated.Before(first.Updated) {
		t.Errorf("updated went backwards: %v then %v", first.Updated, same.Updated)
	}

	// a different error is a different state, the clock restarts
	changed := report(`cannot reach the endpoint`)
	if changed.Since.Equal(same.Since) {
		t.Error("a different error kept the old since, so the row dates a problem that has already been replaced")
	}

	// and so is coming good
	recovered := report(``)
	if !recovered.OK() {
		t.Fatalf("a clean report reads back with the error %q", recovered.Error)
	}
	if recovered.Since.Equal(changed.Since) {
		t.Error("recovering kept the since of the failure it replaced")
	}

	// the carry-forward has to survive whatever the backend does between reports, it is
	// stored rather than remembered in a process
	held := report(boom)
	if again := report(boom); !again.Since.Equal(held.Since) {
		t.Errorf("since was not carried forward across reports: %v then %v", held.Since, again.Since)
	}
}

// testStoreForgetIngester covers the whole of forgetting: registrations, reported statuses
// and the ingester record all go, and runners do not. A runner pinned to the forgotten
// UUID keeps its pin, because forgetting a box that has gone away must not quietly rewrite
// what an operator asked for.
func testStoreForgetIngester(t *testing.T, s Store) {
	gone, stays := mustUUID(t), mustUUID(t)
	runner := mustUUID(t)

	if err := s.ReplaceKinds(gone, `hosted`, []dynamic.RunnerDefinition{
		kindDef(`tester`), kindDef(`sqs`),
	}); err != nil {
		t.Fatalf("failed to register %v: %v", gone, err)
	}
	if err := s.ReplaceKinds(stays, `http`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("failed to register %v: %v", stays, err)
	}
	pinned := runnerDef(`tester`, `beat`, runner)
	pinned.Assigned = &dynamic.Assignment{UUIDs: []uuid.UUID{gone}}
	if err := s.PutRunner(pinned); err != nil {
		t.Fatalf("failed to store a pinned runner: %v", err)
	}
	if err := s.ReplaceStatuses(gone, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `tester`, Name: `beat`, Error: `no`},
	}); err != nil {
		t.Fatalf("failed to report from %v: %v", gone, err)
	}
	if err := s.ReplaceStatuses(stays, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `tester`, Name: `beat`},
	}); err != nil {
		t.Fatalf("failed to report from %v: %v", stays, err)
	}

	if err := s.ForgetIngester(gone); err != nil {
		t.Fatalf("failed to forget %v: %v", gone, err)
	}

	// the ingester record
	for _, ing := range mustIngesters(t, s) {
		if ing.UUID == gone {
			t.Error("the forgotten ingester is still listed")
		}
	}
	// its registrations, both through the per-ingester view and the catalog
	if kinds, err := s.IngesterKinds(gone); err != nil {
		t.Errorf("failed to read the kinds of a forgotten ingester: %v", err)
	} else if len(kinds) != 0 {
		t.Errorf("the forgotten ingester still advertises %d kinds", len(kinds))
	}
	if _, err := s.Kind(`sqs`); !errors.Is(err, ErrNotFound) {
		t.Errorf("a kind only the forgotten ingester registered is still in the catalog: %v", err)
	}
	for _, ing := range mustKindIngesters(t, s, `tester`) {
		if ing.UUID == gone {
			t.Error("the forgotten ingester is still offered as a target for tester")
		}
	}
	// and the statuses it reported
	for _, row := range mustStatuses(t, s) {
		if row.Ingester == gone {
			t.Error("a status reported by the forgotten ingester survived")
		}
	}

	// nothing belonging to the other ingester was touched
	if kinds, err := s.IngesterKinds(stays); err != nil {
		t.Errorf("failed to read the surviving ingester's kinds: %v", err)
	} else if len(kinds) != 1 || kinds[0].Kind != `tester` {
		t.Errorf("the surviving ingester now advertises %v", kindNames(kinds))
	}
	if _, ok := statusOf(t, s, runner, stays); !ok {
		t.Error("the surviving ingester's status was removed along with the forgotten one's")
	}

	// the runner and its pin are exactly as the operator left them
	got, err := s.Runner(runner)
	if err != nil {
		t.Fatalf("the runner was removed along with the ingester it was pinned to: %v", err)
	}
	if got.Assigned == nil || len(got.Assigned.UUIDs) != 1 || got.Assigned.UUIDs[0] != gone {
		t.Errorf("the pin to the forgotten ingester was rewritten to %+v", got.Assigned)
	}
}

// testStoreForgetIngesterUnknown covers the ErrNotFound path on a store that holds nothing
// under the UUID at all.
func testStoreForgetIngesterUnknown(t *testing.T, s Store) {
	if err := s.ForgetIngester(mustUUID(t)); !errors.Is(err, ErrNotFound) {
		t.Errorf("forgetting an unknown ingester: %v, want ErrNotFound", err)
	}
	if err := s.ForgetIngester(uuid.Nil()); err == nil {
		t.Error("forgetting the nil UUID was accepted")
	}
}

// testStoreForgetIngesterOrphans is the case ErrNotFound is easiest to get wrong. A UUID
// with rows and no ingester record is exactly what an operator reaches for this to clear,
// so the cleanup has to be kept even though the call reports that it knew of no such
// ingester. An implementation that returns early under a deferred rollback throws away the
// one piece of work that mattered.
func testStoreForgetIngesterOrphans(t *testing.T, s Store) {
	// a status report does not create an ingester record, only a registration does, so
	// reporting from a UUID that never registered is how an orphan is made through the
	// interface itself
	orphan, runner := mustUUID(t), mustUUID(t)
	if err := s.ReplaceStatuses(orphan, []dynamic.RunnerStatus{
		{UUID: runner, Kind: `tester`, Name: `beat`, Error: `stranded`},
	}); err != nil {
		t.Fatalf("failed to report from an unregistered ingester: %v", err)
	}
	if _, ok := statusOf(t, s, runner, orphan); !ok {
		t.Skip("this backend does not accept a report from an ingester it has no record of, so it cannot hold orphans")
	}

	if err := s.ForgetIngester(orphan); !errors.Is(err, ErrNotFound) {
		t.Errorf("forgetting a UUID with no ingester record: %v, want ErrNotFound", err)
	}
	for _, row := range mustStatuses(t, s) {
		if row.Ingester == orphan {
			t.Error("the orphaned status survived, the cleanup was rolled back on the way to ErrNotFound")
		}
	}
}

// testStoreForgetIngesterThenReRegister covers forgetting not being a ban. The next
// registration puts all of it back, and a runner that kept its pin starts being delivered
// again without anyone editing it.
func testStoreForgetIngesterThenReRegister(t *testing.T, s Store) {
	id := mustUUID(t)
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("failed to register: %v", err)
	}
	if err := s.ForgetIngester(id); err != nil {
		t.Fatalf("failed to forget: %v", err)
	}
	if err := s.ReplaceKinds(id, `hosted`, []dynamic.RunnerDefinition{kindDef(`tester`)}); err != nil {
		t.Fatalf("a forgotten ingester could not register again: %v", err)
	}
	ings := mustIngesters(t, s)
	if len(ings) != 1 || ings[0].UUID != id {
		t.Fatalf("after re-registering the fleet is %+v, want just %v", ings, id)
	}
	if len(ings[0].Kinds) != 1 || ings[0].Kinds[0] != `tester` {
		t.Errorf("the re-registered ingester advertises %v, want [tester]", ings[0].Kinds)
	}
	if _, err := s.Kind(`tester`); err != nil {
		t.Errorf("the kind did not come back into the catalog: %v", err)
	}
}

// --- small readers, so a check reads as the thing it is checking -----------------------

func mustStatuses(t *testing.T, s Store) []StatusRow {
	t.Helper()
	rows, err := s.Statuses()
	if err != nil {
		t.Fatalf("failed to list statuses: %v", err)
	}
	return rows
}

func mustIngesters(t *testing.T, s Store) []Ingester {
	t.Helper()
	ings, err := s.Ingesters()
	if err != nil {
		t.Fatalf("failed to list ingesters: %v", err)
	}
	return ings
}

func mustKindIngesters(t *testing.T, s Store, kind string) []Ingester {
	t.Helper()
	set, err := s.KindIngesters(kind)
	if err != nil {
		t.Fatalf("failed to list the ingesters for %s: %v", kind, err)
	}
	return set
}
