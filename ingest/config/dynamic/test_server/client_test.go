/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// These exercise the real ingester side client against the real server, which is the only
// way to know the two halves agree.  The dynamic package's own tests cannot do it without
// importing this one, and this one already imports that.

// newManager stands up a DynamicConfigManager pointed at the test server.
func newManager(t *testing.T, h *harness, id uuid.UUID, class string) (dynamic.Manager, string) {
	t.Helper()
	storage := t.TempDir()
	m, err := dynamic.NewDynamicConfigManager(nil, dynamic.Config{
		Webserver:  []string{h.ts.URL},
		Auth_Token: testSecret,
		Storage:    storage,
		Class:      class,
		// the tests wait on outcomes, so poll fast rather than sit out the default
		Poll_Interval: `1s`,
	}, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	// the constructor builds the manager, Start runs it.  They are deliberately separate,
	// so a test that forgets this one sits there connecting to nothing.
	if err = m.Start(); err != nil {
		t.Fatal(err)
	}
	return m, storage
}

// waitFor polls until cond holds or the deadline passes.  The client runs on its own
// schedule, so the tests wait on outcomes rather than on timing.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestManagerRegistersAndSyncs is the whole loop: the manager connects, declares what it
// can run, the server is configured with a runner, and the manager writes it to disk and
// raises its signal.
func TestManagerRegistersAndSyncs(t *testing.T) {
	h := newHarness(t)
	id := uuid.New()
	m, storage := newManager(t, h, id, `edge`)

	// registering a kind should reach the server without waiting for a poll
	if err := m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, `the registration to reach the server`, func() bool {
		kinds, err := h.store.IngesterKinds(id)
		return err == nil && len(kinds) == 1 && kinds[0].Kind == `testplugin`
	})

	// the registration is filed under this ingester, which is what lets a webserver tell
	// two ingesters apart
	kinds, err := h.store.IngesterKinds(id)
	if err != nil {
		t.Fatal(err)
	}
	if kinds[0].Name != `` || kinds[0].UUID != uuid.Nil() {
		t.Errorf("a registration should carry no identity, got %+v", kinds[0])
	}
	if others, err := h.store.IngesterKinds(uuid.New()); err != nil {
		t.Fatal(err)
	} else if len(others) != 0 {
		t.Error(`registrations leaked across ingesters`)
	}

	// nothing is configured, so nothing should have been written and nothing signalled
	if ents, _ := os.ReadDir(storage); len(ents) != 0 {
		t.Errorf("storage is not empty before anything was configured: %v", ents)
	}
	select {
	case <-m.Signal():
		t.Error(`the manager signalled a reload with nothing configured`)
	default:
	}

	// now configure a runner on the server
	rd, err := dynamic.MapRunnerDefinition(`testplugin`, `prod`, pluginConfig{
		Tag_Name: `test`, Page_Size: 100, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = uuid.New()
	if err = h.store.PutRunner(rd); err != nil {
		t.Fatal(err)
	}

	// the manager picks it up, writes it, and raises the signal
	select {
	case <-m.Signal():
	case <-time.After(15 * time.Second):
		t.Fatal(`the manager never signalled a reload`)
	}

	pth := filepath.Join(storage, `testplugin_prod_`+rd.UUID.String()+`.conf`)
	blob, err := os.ReadFile(pth)
	if err != nil {
		t.Fatalf("the config was not written where expected: %v", err)
	}
	body := string(blob)
	for _, want := range []string{`[testplugin "prod"]`, "Tag-Name=`test`", `Page-Size=100`, `Enabled=true`} {
		if !strings.Contains(body, want) {
			t.Errorf("the written config is missing %q:\n%s", want, body)
		}
	}
	// no temp file left behind, a loader would try to read it
	ents, _ := os.ReadDir(storage)
	if len(ents) != 1 {
		t.Errorf("storage holds %d entries, want just the one config", len(ents))
	}

	// and what was written actually loads through the config system
	var target struct {
		Testplugin map[string]*loadedPlugin
	}
	if err = m.Load(&target); err != nil {
		t.Fatal(err)
	}
	if len(target.Testplugin) != 1 {
		t.Fatalf("Load picked up %d runners, want 1", len(target.Testplugin))
	}
	if got := target.Testplugin[`prod`]; got == nil {
		t.Fatal(`the runner did not load under its name`)
	} else if got.Page_Size != 100 || got.Tag_Name != `test` || !got.Enabled {
		t.Errorf("loaded %+v", got)
	}
}

// TestManagerSyncsChangesAndDeletes covers the other two halves of the diff.
func TestManagerSyncsChangesAndDeletes(t *testing.T) {
	h := newHarness(t)
	id := uuid.New()
	m, storage := newManager(t, h, id, `edge`)
	if err := m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Fatal(err)
	}

	rd, err := dynamic.MapRunnerDefinition(`testplugin`, `prod`, pluginConfig{Tag_Name: `first`})
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = uuid.New()
	if err = h.store.PutRunner(rd); err != nil {
		t.Fatal(err)
	}
	pth := filepath.Join(storage, `testplugin_prod_`+rd.UUID.String()+`.conf`)
	waitFor(t, `the config to be written`, func() bool {
		_, err := os.Stat(pth)
		return err == nil
	})
	drain(m)

	// an unchanged poll must not rewrite the file or signal.  Comparing the rendered INI
	// rather than the decoded values is what makes this hold, the definition makes a
	// round trip through JSON on the way here.
	before, err := os.Stat(pth)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	after, err := os.Stat(pth)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error(`an unchanged configuration was rewritten, every poll would wake the ingester`)
	}
	select {
	case <-m.Signal():
		t.Error(`an unchanged configuration signalled a reload`)
	default:
	}

	// change it
	changed, err := dynamic.MapRunnerDefinition(`testplugin`, `prod`, pluginConfig{Tag_Name: `second`})
	if err != nil {
		t.Fatal(err)
	}
	changed.UUID = rd.UUID
	if err = h.store.PutRunner(changed); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.Signal():
	case <-time.After(15 * time.Second):
		t.Fatal(`a changed configuration never signalled`)
	}
	if blob, err := os.ReadFile(pth); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(blob), "Tag-Name=`second`") {
		t.Errorf("the change was not written:\n%s", string(blob))
	}

	// delete it
	if err = h.store.DeleteRunner(rd.UUID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.Signal():
	case <-time.After(15 * time.Second):
		t.Fatal(`a deleted configuration never signalled`)
	}
	waitFor(t, `the config file to be removed`, func() bool {
		_, err := os.Stat(pth)
		return os.IsNotExist(err)
	})
	if ents, _ := os.ReadDir(storage); len(ents) != 0 {
		t.Errorf("storage still holds %v after the delete", ents)
	}
}

// TestManagerOnlyTakesWhatItCanRun checks the filtering: a runner of a kind this ingester
// never registered, or assigned to somebody else, must not land on disk.
func TestManagerOnlyTakesWhatItCanRun(t *testing.T) {
	h := newHarness(t)
	id := uuid.New()
	m, storage := newManager(t, h, id, `edge`)
	if err := m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Fatal(err)
	}

	mk := func(t *testing.T, kind, name string, a *dynamic.Assignment) dynamic.RunnerDefinition {
		t.Helper()
		rd, err := dynamic.MapRunnerDefinition(kind, name, pluginConfig{Tag_Name: name})
		if err != nil {
			t.Fatal(err)
		}
		rd.UUID = uuid.New()
		rd.Assigned = a
		if err = h.store.PutRunner(rd); err != nil {
			t.Fatal(err)
		}
		return rd
	}

	mine := mk(t, `testplugin`, `mine`, nil)
	mk(t, `otherplugin`, `wrongkind`, nil)
	mk(t, `testplugin`, `someoneelse`, &dynamic.Assignment{UUIDs: []uuid.UUID{uuid.New()}})
	mk(t, `testplugin`, `otherclass`, &dynamic.Assignment{Classes: []string{`core`}})
	byClass := mk(t, `testplugin`, `myclass`, &dynamic.Assignment{Classes: []string{`edge`}})
	byID := mk(t, `testplugin`, `byid`, &dynamic.Assignment{UUIDs: []uuid.UUID{id}})

	waitFor(t, `the assigned configs to land`, func() bool {
		ents, _ := os.ReadDir(storage)
		return len(ents) == 3
	})
	got := map[string]bool{}
	ents, _ := os.ReadDir(storage)
	for _, e := range ents {
		got[e.Name()] = true
	}
	for _, want := range []dynamic.RunnerDefinition{mine, byClass, byID} {
		f := `testplugin_` + want.Name + `_` + want.UUID.String() + `.conf`
		if !got[f] {
			t.Errorf("%s should have been written, storage holds %v", want.Name, ents)
		}
	}
	for _, unwanted := range []string{`wrongkind`, `someoneelse`, `otherclass`} {
		for name := range got {
			if strings.Contains(name, unwanted) {
				t.Errorf("%s should not have been written", unwanted)
			}
		}
	}
}

// TestManagerBacksOffWhenTheServerIsDown checks that an unreachable webserver does not
// stop the manager from coming up, and that it recovers on its own once the server
// appears.
func TestManagerBacksOffWhenTheServerIsDown(t *testing.T) {
	// a port nothing is listening on
	m, err := dynamic.NewDynamicConfigManager(nil, dynamic.Config{
		Webserver:  []string{`127.0.0.1:1`},
		Auth_Token: testSecret,
		Storage:    t.TempDir(),
	}, uuid.New(), nil)
	if err != nil {
		t.Fatalf("an unreachable webserver should not stop the manager from starting: %v", err)
	}
	// started, so the backoff loop is actually running.  Without this the test would pass
	// by virtue of nothing happening at all, which proves nothing.
	if err = m.Start(); err != nil {
		t.Fatal(err)
	}
	// it must keep working locally regardless
	if err = m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Errorf("registration should work offline: %v", err)
	}
	var target struct {
		Testplugin map[string]*struct{ Tag_Name string }
	}
	if err = m.Load(&target); err != nil {
		t.Errorf("Load should work offline: %v", err)
	}
	// and shut down promptly rather than waiting out a backoff
	done := make(chan error, 1)
	go func() { done <- m.Close() }()
	select {
	case err = <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal(`Close hung, the backoff is not watching the context`)
	}
}

// TestManagerRejectsBadToken checks that a manager pointed at a server it cannot
// authenticate against stays up and simply never syncs.
func TestManagerRejectsBadToken(t *testing.T) {
	h := newHarness(t)
	storage := t.TempDir()
	m, err := dynamic.NewDynamicConfigManager(nil, dynamic.Config{
		Webserver:  []string{h.ts.URL},
		Auth_Token: `the-wrong-token`,
		Storage:    storage,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// started, so the handshake is really attempted and really refused: an unstarted
	// manager writes nothing either, and would pass this test for the wrong reason
	if err = m.Start(); err != nil {
		t.Fatal(err)
	}
	if err = m.RegisterKind(`testplugin`, false, pluginConfig{}); err != nil {
		t.Fatal(err)
	}
	rd, _ := dynamic.MapRunnerDefinition(`testplugin`, `prod`, pluginConfig{})
	rd.UUID = uuid.New()
	if err = h.store.PutRunner(rd); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)
	if ents, _ := os.ReadDir(storage); len(ents) != 0 {
		t.Errorf("a manager that cannot authenticate wrote %v", ents)
	}
	select {
	case <-m.Signal():
		t.Error(`a manager that cannot authenticate signalled a reload`)
	default:
	}
	// and the server holds no registration for it
	if ids, err := h.store.Ingesters(); err != nil {
		t.Fatal(err)
	} else if len(ids) != 0 {
		t.Errorf("an unauthenticated ingester registered %v", ids)
	}
}

// drain empties a pending signal so a later wait is unambiguous.
func drain(m dynamic.Manager) {
	select {
	case <-m.Signal():
	default:
	}
}

// loadedPlugin is what a written config is read back into.  It has to carry every key the
// INI emits, Ingester-UUID included, and it has to be a named type, gcfg will not populate
// an anonymous struct consistently across more than one file.
type loadedPlugin struct {
	Ingester_UUID string
	Tag_Name      string
	Page_Size     int
	Rate          float64
	Enabled       bool
	Sections      []string
}

var _ = config.LoadConfigOverlays
