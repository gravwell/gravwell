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
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/hosted/plugins"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// pluginMembers reports the members of plugins.Configs by reflection.  The tests derive
// what they expect from the config set itself rather than from a list, so adding a plugin
// does not mean coming back here to update anything.
func pluginMembers(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf(plugins.Configs{})
	names := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		if f := rt.Field(i); f.IsExported() {
			names = append(names, f.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal(`plugins.Configs has no members, the tests below would prove nothing`)
	}
	sort.Strings(names)
	return names
}

// nopRunner builds the no op manager the runner uses when dynamic config is disabled.
func nopRunner() *dynamic.NopManager {
	return &dynamic.NopManager{}
}

// TestRegisterDynamicPluginTypes checks that every plugin carried in plugins.Configs is
// advertised to the manager, with no list to maintain on either side.
func TestRegisterDynamicPluginTypes(t *testing.T) {
	nm := nopRunner()
	if err := registerDynamicPluginTypes(nm); err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(nm.Available))
	for _, rd := range nm.Available {
		got = append(got, rd.Kind)
	}
	sort.Strings(got)
	if want := pluginMembers(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("registered %v, want every plugin in Configs %v", got, want)
	}

	// a kind is a description, it carries no identity and every plugin can run more than
	// one instance at a time, so none of them are singletons
	for _, rd := range nm.Available {
		if rd.Name != `` || rd.UUID != uuid.Nil() {
			t.Errorf("%s: a kind should carry no identity, got name %q uuid %v", rd.Kind, rd.Name, rd.UUID)
		}
		if rd.Singleton {
			t.Errorf("%s: plugins are held in maps, none of them are singletons", rd.Kind)
		}
		if len(rd.Variables) == 0 {
			t.Errorf("%s: registered with no variables, its config did not enumerate", rd.Kind)
		}
	}

	// a nil manager is a wiring mistake, not a panic
	if err := registerDynamicPluginTypes(nil); err == nil {
		t.Error(`a nil manager should be rejected`)
	}
}

// TestRegisterDynamicPluginTypesIdempotent checks that registering twice is reported
// rather than silently producing duplicates.
func TestRegisterDynamicPluginTypesIdempotent(t *testing.T) {
	nm := nopRunner()
	if err := registerDynamicPluginTypes(nm); err != nil {
		t.Fatal(err)
	}
	n := len(nm.Available)
	if err := registerDynamicPluginTypes(nm); err == nil {
		t.Error(`registering the same kinds twice should be rejected`)
	}
	if len(nm.Available) != n {
		t.Errorf("Available = %d, want %d, a rejected registration should add nothing", len(nm.Available), n)
	}
}

// TestNopRunnerLoadsEveryPluginConfig is the disabled dynamic config path: the runner
// still advertises every plugin and can take a configured runner for each one.
func TestNopRunnerLoadsEveryPluginConfig(t *testing.T) {
	nm := nopRunner()
	if err := registerDynamicPluginTypes(nm); err != nil {
		t.Fatal(err)
	}
	kinds, err := plugins.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range kinds {
		if err = nm.RegisterRunner(`prod`, pk.Kind, uuid.Nil(), pk.Config); err != nil {
			t.Errorf("%s: %v", pk.Kind, err)
		}
	}
	if len(nm.Configured) != len(kinds) {
		t.Fatalf("configured %d runners, want %d", len(nm.Configured), len(kinds))
	}
	for _, cr := range nm.Configured {
		if cr.UUID == uuid.Nil() {
			t.Errorf("%s: a UUID should have been generated", cr.Kind)
		}
	}
}

// TestDynamicRunnerLoadsEveryPluginConfig is the point of the whole exercise: a config
// written for any plugin has to be readable back into the very struct the runner uses.
func TestDynamicRunnerLoadsEveryPluginConfig(t *testing.T) {
	dir := t.TempDir()
	dm, err := dynamic.NewDynamicConfigManager(context.TODO(), dynamic.Config{
		Webserver:  []string{`10.0.0.1:8080`},
		Auth_Token: `token`,
		Storage:    dir,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dm.Close()

	if err = registerDynamicPluginTypes(dm); err != nil {
		t.Fatal(err)
	}
	kinds, err := plugins.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	// two instances of every plugin, the names have to survive as the map keys
	names := []string{`prod`, `dev`}
	for _, pk := range kinds {
		for _, name := range names {
			if err = dm.RegisterRunner(name, pk.Kind, uuid.New(), pk.Config); err != nil {
				t.Fatalf("%s %s: %v", pk.Kind, name, err)
			}
		}
	}

	// read every generated config back into the struct the runner actually parses into
	var cfgs plugins.Configs
	if err = config.LoadConfigOverlays(&cfgs, dir); err != nil {
		t.Fatalf("the generated plugin configs do not load back: %v", err)
	}
	// every member of Configs has to have been populated, by reflection so that a new
	// plugin is covered here without touching this test
	rv := reflect.ValueOf(cfgs)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		m := rv.Field(i)
		if m.Len() != len(names) {
			t.Errorf("Configs.%s holds %d configs, want %d", f.Name, m.Len(), len(names))
			continue
		}
		for _, name := range names {
			e := m.MapIndex(reflect.ValueOf(name))
			if !e.IsValid() {
				t.Errorf("Configs.%s is missing %q", f.Name, name)
			} else if e.Kind() == reflect.Pointer && e.IsNil() {
				t.Errorf("Configs.%s[%q] loaded as nil", f.Name, name)
			}
		}
	}
}

// TestPluginKindsDerivedFromConfigs guards the reflection itself, a plugin added to
// Configs in a shape the dynamic system cannot describe has to be reported loudly rather
// than quietly left out.
func TestPluginKindsDerivedFromConfigs(t *testing.T) {
	kinds, err := plugins.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(kinds))
	seen := map[string]bool{}
	for _, pk := range kinds {
		if pk.Kind == `` {
			t.Error(`a kind with no name`)
		}
		if seen[pk.Kind] {
			t.Errorf("%s appears twice", pk.Kind)
		}
		seen[pk.Kind] = true
		// the config has to be a zero valued struct, it describes a type and must never
		// carry data from whatever the caller happened to have lying around
		rv := reflect.ValueOf(pk.Config)
		if rv.Kind() != reflect.Struct {
			t.Errorf("%s: config is a %s, want a struct", pk.Kind, rv.Kind())
		} else if !rv.IsZero() {
			t.Errorf("%s: config is not zero valued", pk.Kind)
		}
		got = append(got, pk.Kind)
	}
	sort.Strings(got)
	if want := pluginMembers(t); !reflect.DeepEqual(got, want) {
		t.Errorf("Kinds() = %v, want every member of Configs %v", got, want)
	}
}

// TestConfigsIngesterCountCoversEveryPlugin checks the count the runner actually gates
// startup on.  main refuses to start when IngesterCount is zero, so a count that misses a
// plugin means a runner configured with only that plugin exits with "no hosted ingesters
// configured".  Counting is derived from Configs now, this pins the behaviour so a return
// to a hand written sum is caught.
func TestConfigsIngesterCountCoversEveryPlugin(t *testing.T) {
	kinds, err := plugins.Kinds()
	if err != nil {
		t.Fatal(err)
	}

	// every plugin on its own has to be enough to start the runner
	for _, pk := range kinds {
		dir := t.TempDir()
		var dm dynamic.Manager
		if dm, err = dynamic.NewDynamicConfigManager(context.TODO(), dynamic.Config{
			Webserver:  []string{`10.0.0.1:8080`},
			Auth_Token: `token`,
			Storage:    dir,
		}, uuid.New(), nil); err != nil {
			t.Fatal(err)
		}
		if err = registerDynamicPluginTypes(dm); err != nil {
			dm.Close()
			t.Fatal(err)
		}
		if err = dm.RegisterRunner(`prod`, pk.Kind, uuid.New(), pk.Config); err != nil {
			dm.Close()
			t.Fatalf("%s: %v", pk.Kind, err)
		}
		dm.Close()

		var cfgs plugins.Configs
		if err = config.LoadConfigOverlays(&cfgs, dir); err != nil {
			t.Fatalf("%s: %v", pk.Kind, err)
		}
		if n := cfgs.IngesterCount(); n != 1 {
			t.Errorf("%s alone counted %d ingesters, want 1, the runner would refuse to start", pk.Kind, n)
		}
	}

	// and the counts have to add up across all of them at once
	dir := t.TempDir()
	dm, err := dynamic.NewDynamicConfigManager(context.TODO(), dynamic.Config{
		Webserver:  []string{`10.0.0.1:8080`},
		Auth_Token: `token`,
		Storage:    dir,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dm.Close()
	if err = registerDynamicPluginTypes(dm); err != nil {
		t.Fatal(err)
	}
	names := []string{`prod`, `dev`}
	for _, pk := range kinds {
		for _, name := range names {
			if err = dm.RegisterRunner(name, pk.Kind, uuid.New(), pk.Config); err != nil {
				t.Fatalf("%s %s: %v", pk.Kind, name, err)
			}
		}
	}
	var cfgs plugins.Configs
	if err = config.LoadConfigOverlays(&cfgs, dir); err != nil {
		t.Fatal(err)
	}
	if got, want := cfgs.IngesterCount(), len(kinds)*len(names); got != want {
		t.Errorf("IngesterCount = %d, want %d, one config per plugin per name", got, want)
	}

	// an empty config set counts nothing, that is what makes the startup gate meaningful
	if n := (plugins.Configs{}).IngesterCount(); n != 0 {
		t.Errorf("an empty Configs counted %d, want 0", n)
	}
}

// TestDynamicLoadIntoRunnerConfig pins the pointer shape the reload path has to hand the
// dynamic manager.  newCfg in main is already a *cfgType, so passing &newCfg gives the
// overlay loader a **cfgType, which it rejects.  That aborts the whole reload, so with
// dynamic config enabled a SIGHUP would quietly stop reloading anything.
func TestDynamicLoadIntoRunnerConfig(t *testing.T) {
	dir := t.TempDir()
	dm, err := dynamic.NewDynamicConfigManager(context.TODO(), dynamic.Config{
		Webserver:  []string{`10.0.0.1:8080`},
		Auth_Token: `token`,
		Storage:    dir,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dm.Close()
	if err = registerDynamicPluginTypes(dm); err != nil {
		t.Fatal(err)
	}
	kinds, err := plugins.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range kinds {
		if err = dm.RegisterRunner(`prod`, pk.Kind, uuid.New(), pk.Config); err != nil {
			t.Fatalf("%s: %v", pk.Kind, err)
		}
	}

	// every kind rendered to a file, which is the plumbing this is really about
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != len(kinds) {
		t.Fatalf("wrote %d configs for %d kinds", len(ents), len(kinds))
	}

	// the shape the reload path uses, a *cfgType, every dynamic config has to land in it
	newCfg := &cfgType{}
	if err = dm.Load(newCfg); err != nil {
		t.Fatalf("the reload path cannot load dynamic configs: %v", err)
	}

	// but only the ones that could actually run.  These were registered from zero valued
	// prototypes, and a plugin config with no credentials in it is not a configuration
	// anything can run: Okta has no domain or token, SQS has no queue. Load checks each
	// one against the plugin that would have to run it and skips the ones it refuses,
	// which is the whole reason a bad configuration no longer stops the runner starting.
	// Tester is the only plugin whose zero value is runnable, so it is the only one here
	// that should land.
	if newCfg.Configs.Tester[`prod`] == nil {
		t.Error(`the one runnable configuration was not loaded`)
	}
	if got := newCfg.IngesterCount(); got != 1 {
		t.Errorf("reload picked up %d ingesters, want 1: %d of these prototypes are not runnable", got, len(kinds)-1)
	}

	// and the rest are reported rather than silently dropped, so an operator finds out
	// why a runner they configured is not running
	reported := map[string]string{}
	if sr, ok := dm.(interface{ Statuses() []dynamic.RunnerStatus }); ok {
		for _, rs := range sr.Statuses() {
			if !rs.OK() {
				reported[rs.Kind] = rs.Error
			}
		}
	}
	if len(reported) != len(kinds)-1 {
		t.Errorf("reported %d unusable configurations, want %d: %v", len(reported), len(kinds)-1, reported)
	}
	for kind, msg := range reported {
		if msg == `` {
			t.Errorf("%s was reported with no reason", kind)
		}
	}

	// a pointer to that pointer is not a struct and must be refused rather than quietly
	// loading nothing
	if err = dm.Load(&newCfg); err == nil {
		t.Error(`Load should reject a **cfgType`)
	}
}

// TestDynamicLoadSkipsABadTagAndKeepsGoing is the runner's own version of the case that
// stopped it starting: a dynamic configuration carrying a tag the indexer will refuse.
//
// It uses the real cfgType and the real tester plugin, because the failure was in the
// seam between them: the file parses into cfgType happily, and only tester's Verify knows
// the tag is unusable. Loading it meant cfg.Tags() blew up later, during muxer setup,
// where nothing ties the failure back to the configuration that caused it and the
// ingester is already on its way down.
func TestDynamicLoadSkipsABadTagAndKeepsGoing(t *testing.T) {
	dir := t.TempDir()
	dm, err := dynamic.NewDynamicConfigManager(context.TODO(), dynamic.Config{
		Webserver:  []string{`127.0.0.1:1`}, // nothing there, this is about the load
		Auth_Token: `token`,
		Storage:    dir,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dm.Close()
	// registered before loading, the way main does it, so the report can name the runner
	if err = registerDynamicPluginTypes(dm); err != nil {
		t.Fatal(err)
	}

	write := func(name, tag string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		body := "[Tester \"" + name + "\"]\n\tIngester-UUID=" + id.String() +
			"\n\tTag-Name=`" + tag + "`\n\tInterval=`10s`\n"
		if werr := os.WriteFile(filepath.Join(dir, `Tester_`+name+`_`+id.String()+`.conf`),
			[]byte(body), 0660); werr != nil {
			t.Fatal(werr)
		}
		return id
	}
	goodID := write(`works`, `things`)
	badID := write(`willfail`, `30984r09saokdj;lksaj;lk ;alsdkj f#)(*)!(@*`)

	cfg := &cfgType{}
	if err = dm.Load(cfg); err != nil {
		t.Fatalf("one unusable dynamic configuration stopped the runner loading: %v", err)
	}

	// the good one is configured and the bad one is not there at all
	if cfg.Configs.Tester[`works`] == nil {
		t.Fatal(`the usable configuration was not loaded`)
	}
	if cfg.Configs.Tester[`willfail`] != nil {
		t.Errorf("the unusable configuration was loaded: %+v", cfg.Configs.Tester[`willfail`])
	}
	if got := cfg.IngesterCount(); got != 1 {
		t.Errorf("configured %d ingesters, want 1", got)
	}

	// and this is what actually killed startup: collecting the tags across everything
	// configured has to succeed now that the bad one never made it in
	tags, err := cfg.Configs.Tags()
	if err != nil {
		t.Fatalf("collecting tags still fails, which is what stopped the runner: %v", err)
	}
	if !slices.Contains(tags, `things`) {
		t.Errorf("tags %v do not include the good runner's", tags)
	}

	// the failure is reported upstream, named, and attributed to the right runner
	var found bool
	for _, rs := range dm.(interface{ Statuses() []dynamic.RunnerStatus }).Statuses() {
		if rs.UUID != badID {
			continue
		}
		found = true
		if rs.OK() {
			t.Errorf("the unusable configuration reported clean: %+v", rs)
		}
		if rs.Kind != `Tester` || rs.Name != `willfail` {
			t.Errorf("the report does not name the runner: %+v", rs)
		}
		if !strings.Contains(rs.Error, `Forbidden character in tag`) {
			t.Errorf("the report does not carry the reason: %q", rs.Error)
		}
	}
	if !found {
		t.Error(`the unusable configuration was not reported upstream at all`)
	}
	if st, ok := statusOf(dm, goodID); ok && !st.OK() {
		t.Errorf("the good configuration was reported as failing: %+v", st)
	}
}

// statusOf finds what the manager reports about one runner.
func statusOf(dm dynamic.Manager, id uuid.UUID) (dynamic.RunnerStatus, bool) {
	sr, ok := dm.(interface{ Statuses() []dynamic.RunnerStatus })
	if !ok {
		return dynamic.RunnerStatus{}, false
	}
	for _, rs := range sr.Statuses() {
		if rs.UUID == id {
			return rs, true
		}
	}
	return dynamic.RunnerStatus{}, false
}
