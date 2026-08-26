/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
)

// TestSyncChildrenRegisters covers the startup case, every configured plugin has to land on the
// muxer as a child with its own identifiers.
func TestSyncChildrenRegisters(t *testing.T) {
	reg := newTestRegistry()
	rm := newTestManager(reg, testRunner{kind: `okta`, name: `corp`}, testRunner{kind: `wiz`, name: `cloud`})

	rm.syncChildren()

	if len(reg.children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(reg.children))
	}
	st, ok := reg.children[`okta/corp`]
	if !ok {
		t.Fatalf("okta plugin was not registered, got %v", reg.keys())
	}
	if st.Name != `okta` || st.Label != `corp` {
		t.Fatalf("child identifiers are wrong: %q %q", st.Name, st.Label)
	}
	if _, ok = reg.children[`wiz/cloud`]; !ok {
		t.Fatalf("wiz plugin was not registered, got %v", reg.keys())
	}
}

// TestSyncChildrenReload covers a config reload, a plugin that went away has to be dropped from
// the muxer and the survivors have to have their state refreshed rather than left stale.
func TestSyncChildrenReload(t *testing.T) {
	reg := newTestRegistry()
	keep := testRunner{kind: `okta`, name: `corp`, entries: 5}
	rm := newTestManager(reg, keep, testRunner{kind: `wiz`, name: `cloud`})
	rm.syncChildren()

	// the wiz plugin is removed from the config and okta keeps ingesting
	for guid, wr := range rm.mp {
		if wr.kind == `wiz` {
			delete(rm.mp, guid)
		} else {
			wr.Runner = testRunner{kind: `okta`, name: `corp`, entries: 25}
			rm.mp[guid] = wr
		}
	}
	rm.syncChildren()

	if len(reg.children) != 1 {
		t.Fatalf("expected 1 child after reload, got %v", reg.keys())
	}
	st, ok := reg.children[`okta/corp`]
	if !ok {
		t.Fatalf("okta plugin lost its registration, got %v", reg.keys())
	}
	if st.Entries != 25 {
		t.Fatalf("child state was not refreshed, entries %d", st.Entries)
	}
	if reg.unregistered[`wiz/cloud`] != 1 {
		t.Fatalf("removed plugin was not unregistered, got %v", reg.unregistered)
	}
}

// TestStopUnregistersChildren makes sure a shutdown does not leave children hanging on the muxer.
func TestStopUnregistersChildren(t *testing.T) {
	reg := newTestRegistry()
	rm := newTestManager(reg, testRunner{kind: `okta`, name: `corp`})
	rm.syncChildren()
	rm.mp = nil // stop closes runners, we only care about the registrations here

	if err := rm.stop(); err != nil {
		t.Fatal(err)
	}
	if len(reg.children) != 0 {
		t.Fatalf("children left registered after stop: %v", reg.keys())
	}
	if len(rm.kids) != 0 {
		t.Fatalf("manager still tracking children after stop: %v", rm.kids)
	}
}

func newTestManager(reg childRegistry, runners ...testRunner) *runtimeManager {
	ctx, cf := context.WithCancel(context.Background())
	rm := &runtimeManager{
		ctx:  ctx,
		cf:   cf,
		reg:  reg,
		mp:   make(map[uuid.UUID]wrappedRunner),
		kids: make(map[string]struct{}),
	}
	for _, r := range runners {
		guid := uuid.New()
		r.guid = guid
		rm.mp[guid] = wrappedRunner{
			Runner:   r,
			kind:     r.kind,
			childKey: childKey(r.kind, r.name),
		}
	}
	return rm
}

// testRegistry stands in for the ingest muxer and just records what was registered
type testRegistry struct {
	children     map[string]ingest.IngesterState
	unregistered map[string]int
}

func newTestRegistry() *testRegistry {
	return &testRegistry{
		children:     map[string]ingest.IngesterState{},
		unregistered: map[string]int{},
	}
}

func (tr *testRegistry) RegisterChild(k string, v ingest.IngesterState) {
	tr.children[k] = v
}

func (tr *testRegistry) UnregisterChild(k string) {
	delete(tr.children, k)
	tr.unregistered[k]++
}

func (tr *testRegistry) keys() (r []string) {
	for k := range tr.children {
		r = append(r, k)
	}
	return
}

// testRunner is a minimal hosted.Runner, we only exercise the identity and state methods
type testRunner struct {
	kind    string
	name    string
	guid    uuid.UUID
	entries uint64
}

func (tr testRunner) Start() error                                  { return nil }
func (tr testRunner) Close() error                                  { return nil }
func (tr testRunner) Running() bool                                 { return true }
func (tr testRunner) ID() string                                    { return tr.kind + `.ingesters.gravwell.io` }
func (tr testRunner) Name() string                                  { return tr.name }
func (tr testRunner) Version() string                               { return `1.0.0` }
func (tr testRunner) UUID() uuid.UUID                               { return tr.guid }
func (tr testRunner) Config() any                                   { return &struct{}{} }
func (tr testRunner) Run(_ context.Context, _ hosted.Runtime) error { return nil }

func (tr testRunner) ChildState() ingest.IngesterState {
	return ingest.IngesterState{
		UUID:    tr.guid.String(),
		Name:    tr.ID(),
		Label:   tr.name,
		Version: tr.Version(),
		Entries: tr.entries,
	}
}
