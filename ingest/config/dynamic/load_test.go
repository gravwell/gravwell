/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"
)

// loadCfg stands in for a hosted runner's configuration: a map of named plugin configs,
// which is the shape gcfg populates from a named section.
type loadCfg struct {
	Tester map[string]*testerish
}

type testerish struct {
	Ingester_UUID string
	Tag_Name      string
	Interval      string
	Page_Size     int
}

// writeConf drops a configuration into storage under the name runnerPath would give it.
func writeConf(t *testing.T, dir, kind, name string, id uuid.UUID, body string) string {
	t.Helper()
	pth := filepath.Join(dir, fnameChunk(kind)+`_`+fnameChunk(name)+`_`+id.String()+confExt)
	if err := os.WriteFile(pth, []byte(body), 0660); err != nil {
		t.Fatal(err)
	}
	return pth
}

// newLoadManager builds a manager pointed at a dead webserver: these tests are about what
// Load does with a directory, not about talking to anything.
func newLoadManager(t *testing.T, dir string) *DynamicConfigManager {
	t.Helper()
	m, err := NewDynamicConfigManager(nil, Config{
		Webserver:  []string{`http://127.0.0.1:1`},
		Auth_Token: `token`,
		Storage:    dir,
	}, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m.(*DynamicConfigManager)
}

// statusFor finds what Load concluded about one runner.
func loadStatus(t *testing.T, dcm *DynamicConfigManager, id uuid.UUID) (RunnerStatus, bool) {
	t.Helper()
	for _, rs := range dcm.Statuses() {
		if rs.UUID == id {
			return rs, true
		}
	}
	return RunnerStatus{}, false
}

// TestLoadSurvivesABadConfig is the case this exists for.
//
// A configuration arrives from a webserver, so a bad one is not a local mistake somebody
// can be told to go and fix: it lands on every ingester that matches its assignment. If
// one of those files stops the ingester loading, one bad edit strands the fleet, and it
// stays stranded until somebody with shell access finds the file.
func TestLoadSurvivesABadConfig(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	// registered, which is the state a reload runs in: main registers the plugin kinds
	// before any SIGHUP or dynamic update can come back round to Load
	if err := dcm.RegisterKind(`Tester`, false, testerish{}); err != nil {
		t.Fatal(err)
	}

	goodID, badID := uuid.New(), uuid.New()
	writeConf(t, dir, `Tester`, `good`, goodID,
		"[Tester \"good\"]\n\tTag-Name=`test`\n\tInterval=`1s`\n")
	// a key the config type does not have, which is what an ingester downgrade produces
	writeConf(t, dir, `Tester`, `bad`, badID,
		"[Tester \"bad\"]\n\tTag-Name=`test`\n\tNonsense=`x`\n")

	var cfg loadCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("one bad configuration stopped the whole load: %v", err)
	}

	// the good one is loaded, which is the entire point
	if cfg.Tester[`good`] == nil {
		t.Fatal(`the good configuration was not loaded`)
	}
	if cfg.Tester[`good`].Interval != `1s` {
		t.Errorf("the good configuration is wrong: %+v", cfg.Tester[`good`])
	}
	// and the bad one left nothing half applied behind it
	if cfg.Tester[`bad`] != nil {
		t.Errorf("a rejected configuration was partially applied: %+v", cfg.Tester[`bad`])
	}

	// the reason is recorded against the runner it belongs to, ready to go upstream
	st, ok := loadStatus(t, dcm, badID)
	if !ok {
		t.Fatal(`nothing was reported for the configuration that would not load`)
	}
	if st.OK() {
		t.Errorf("the broken configuration reported clean: %+v", st)
	}
	if !strings.Contains(st.Error, `Nonsense`) {
		t.Errorf("the reported reason does not say what was wrong: %q", st.Error)
	}
	if st.Kind != `Tester` || st.Name != `bad` {
		t.Errorf("the failure is not labelled with the runner it belongs to: %+v", st)
	}

	// and on the very first load of a process, before any kind is registered, there is
	// nothing to split the label with.  It is reported unsplit rather than guessed at,
	// and the UUID, which is what the report is keyed on, is unaffected.
	fresh := newLoadManager(t, dir)
	var refetch loadCfg
	if err := fresh.Load(&refetch); err != nil {
		t.Fatal(err)
	}
	st, ok = loadStatus(t, fresh, badID)
	if !ok {
		t.Fatal(`nothing was reported with no kinds registered`)
	}
	if st.UUID != badID {
		t.Errorf("the report is keyed on the wrong runner: %+v", st)
	}
	if st.Kind != `` || st.Name != `Tester_bad` {
		t.Errorf("with no kinds registered the label should be left unsplit, got %+v", st)
	}
	// and nothing is reported against the one that loaded
	if st, ok := loadStatus(t, dcm, goodID); ok && !st.OK() {
		t.Errorf("the good configuration was reported as failing: %+v", st)
	}
}

// TestLoadKeepsGoingPastSeveralBadConfigs covers a directory where most of it is broken,
// which is what a bad ingester upgrade looks like.
func TestLoadKeepsGoingPastSeveralBadConfigs(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	good := uuid.New()
	writeConf(t, dir, `Tester`, `good`, good,
		"[Tester \"good\"]\n\tTag-Name=`test`\n")

	bad := map[string]uuid.UUID{
		`unknownkey`: uuid.New(),
		`badtype`:    uuid.New(),
		`garbage`:    uuid.New(),
		`truncated`:  uuid.New(),
	}
	writeConf(t, dir, `Tester`, `unknownkey`, bad[`unknownkey`], "[Tester \"unknownkey\"]\n\tNope=`x`\n")
	writeConf(t, dir, `Tester`, `badtype`, bad[`badtype`], "[Tester \"badtype\"]\n\tPage-Size=`not a number`\n")
	writeConf(t, dir, `Tester`, `garbage`, bad[`garbage`], "this is not an ini file at all\x00\x01\n")
	writeConf(t, dir, `Tester`, `truncated`, bad[`truncated`], "[Tester \"truncated\"\n")

	var cfg loadCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("Load failed with four bad configurations present: %v", err)
	}
	if cfg.Tester[`good`] == nil {
		t.Fatal(`the one good configuration was not loaded`)
	}
	for name, id := range bad {
		st, ok := loadStatus(t, dcm, id)
		if !ok {
			t.Errorf("%s: nothing reported", name)
			continue
		}
		if st.OK() {
			t.Errorf("%s: reported clean", name)
		}
		if !strings.Contains(st.Error, `failed to load`) {
			t.Errorf("%s: unhelpful reason %q", name, st.Error)
		}
	}
}

// TestLoadClearsAFixedConfig covers the other half of the lifecycle: the set is replaced
// by each Load, so a configuration that has been repaired stops being reported without
// anything having to retract it.
func TestLoadClearsAFixedConfig(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	id := uuid.New()
	pth := writeConf(t, dir, `Tester`, `beat`, id, "[Tester \"beat\"]\n\tNonsense=`x`\n")

	var cfg loadCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatal(err)
	}
	if st, ok := loadStatus(t, dcm, id); !ok || st.OK() {
		t.Fatalf("setup failed, the broken config was not reported: %+v", st)
	}

	// repair it the way a webserver would, by replacing the file
	if err := os.WriteFile(pth, []byte("[Tester \"beat\"]\n\tTag-Name=`test`\n"), 0660); err != nil {
		t.Fatal(err)
	}
	var fixed loadCfg
	if err := dcm.Load(&fixed); err != nil {
		t.Fatal(err)
	}
	if fixed.Tester[`beat`] == nil {
		t.Fatal(`the repaired configuration was not loaded`)
	}
	if st, ok := loadStatus(t, dcm, id); ok && !st.OK() {
		t.Errorf("the error survived a load that worked: %+v", st)
	}
}

// TestLoadHandlesAnUnidentifiableFile covers a file nobody's naming convention produced.
// There is no runner to report it against, so it must not be reported against the wrong
// one and must not stop the load.
func TestLoadHandlesAnUnidentifiableFile(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	good := uuid.New()
	writeConf(t, dir, `Tester`, `good`, good, "[Tester \"good\"]\n\tTag-Name=`test`\n")
	if err := os.WriteFile(filepath.Join(dir, `handwritten`+confExt),
		[]byte("[Tester \"hand\"]\n\tNonsense=`x`\n"), 0660); err != nil {
		t.Fatal(err)
	}

	var cfg loadCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("an unidentifiable file stopped the load: %v", err)
	}
	if cfg.Tester[`good`] == nil {
		t.Error(`the good configuration was not loaded`)
	}
	for _, rs := range dcm.Statuses() {
		if !rs.OK() && rs.UUID == good {
			t.Errorf("a file with no runner was blamed on one that was fine: %+v", rs)
		}
	}
}

// TestLoadIgnoresNonConfigFiles covers the temporary files writeConfFile leaves during a
// write, which deliberately do not end in .conf.
func TestLoadIgnoresNonConfigFiles(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	writeConf(t, dir, `Tester`, `good`, uuid.New(), "[Tester \"good\"]\n\tTag-Name=`test`\n")
	for _, name := range []string{`Tester_x_half.conf.temp`, `notes.txt`, `.hidden`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("garbage {{{\n"), 0660); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, `subdir`+confExt), 0770); err != nil {
		t.Fatal(err)
	}

	var cfg loadCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("Load tripped over something that is not a config: %v", err)
	}
	if cfg.Tester[`good`] == nil {
		t.Error(`the good configuration was not loaded`)
	}
	if n := len(dcm.Statuses()); n != 0 {
		t.Errorf("reported %d failures for files that are not configs: %+v", n, dcm.Statuses())
	}
}

// TestLoadStillFailsOnAnUnusableDirectory draws the line: skipping a file is the right
// answer for a bad file, but a storage directory that cannot be read at all is a
// deployment problem and hiding it would start the ingester with no configuration and no
// complaint.
func TestLoadStillFailsOnAnUnusableDirectory(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, `storage`)
	if err := os.WriteFile(notDir, []byte(`x`), 0660); err != nil {
		t.Fatal(err)
	}
	dcm := newLoadManager(t, dir)
	dcm.Storage = notDir // point it at a file rather than a directory

	var cfg loadCfg
	if err := dcm.Load(&cfg); err == nil {
		t.Error(`a storage path that is not a directory was accepted silently`)
	}
}

// TestParseRunnerFile covers pulling the UUID back out of a file name.  The rest is
// handed back unsplit on purpose, see splitKindName.
func TestParseRunnerFile(t *testing.T) {
	id := uuid.New()
	for _, tc := range []struct {
		file, rest string
		ok         bool
	}{
		{`Tester_beat_` + id.String() + `.conf`, `Tester_beat`, true},
		{`Tester_my_long_name_` + id.String() + `.conf`, `Tester_my_long_name`, true},
		{`My_Kind_my_runner_` + id.String() + `.conf`, `My_Kind_my_runner`, true},
		{`Tester_` + id.String() + `.conf`, `Tester`, true},
		{`handwritten.conf`, ``, false},
		{`Tester_beat_not-a-uuid.conf`, ``, false},
	} {
		gotID, rest, ok := parseRunnerFile(filepath.Join(`/store`, tc.file))
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.file, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if gotID != id {
			t.Errorf("%s: uuid = %v, want %v", tc.file, gotID, id)
		}
		if rest != tc.rest {
			t.Errorf("%s: rest = %q, want %q", tc.file, rest, tc.rest)
		}
	}
}

// TestSplitKindName covers the part that cannot be done by looking for a separator: both
// halves may contain one, so the registered kinds are what settle it.
func TestSplitKindName(t *testing.T) {
	dcm := newLoadManager(t, t.TempDir())
	if err := dcm.RegisterKind(`Tester`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := dcm.RegisterKind(`My_Kind`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ rest, kind, name string }{
		{`Tester_beat`, `Tester`, `beat`},
		{`Tester_my_long_name`, `Tester`, `my_long_name`},
		// the case the old split got wrong: it took the first underscore and reported
		// kind "My", name "Kind_my_runner"
		{`My_Kind_my_runner`, `My_Kind`, `my_runner`},
		// nothing registered matches, so no split is invented
		{`Unknown_thing`, ``, `Unknown_thing`},
		{`Tester`, ``, `Tester`},
	} {
		kind, name := dcm.splitKindName(tc.rest)
		if kind != tc.kind || name != tc.name {
			t.Errorf("%q: kind/name = %q/%q, want %q/%q", tc.rest, kind, name, tc.kind, tc.name)
		}
	}

	// before anything is registered there is nothing to split with, which is the state
	// the very first load of a process runs in
	fresh := newLoadManager(t, t.TempDir())
	if kind, name := fresh.splitKindName(`Tester_beat`); kind != `` || name != `Tester_beat` {
		t.Errorf("with no kinds registered got %q/%q, want the label left unsplit", kind, name)
	}
}

// verifiedRunner is a plugin config with an opinion about its own values, which is the
// shape that matters here: it parses as INI whatever you put in Tag_Name, and only the
// plugin knows that some of those are not usable.
type verifiedRunner struct {
	Ingester_UUID string
	Tag_Name      string
	Interval      string
}

func (v *verifiedRunner) Verify() error {
	if strings.ContainsAny(v.Tag_Name, " ;!#*") {
		return fmt.Errorf("invalid tag %q", v.Tag_Name)
	}
	return nil
}

// panicOnVerify stands in for a plugin whose Verify blows up on deployed data.
type panicOnVerify struct {
	Ingester_UUID string
	Tag_Name      string
}

func (p *panicOnVerify) Verify() error {
	if p.Tag_Name == `boom` {
		panic(`plugin Verify blew up on a deployed value`)
	}
	return nil
}

// verifiedCfg is the target, shaped the way a hosted runner's configuration is: plugin
// configurations held in named maps, reached through an embedded struct.
type verifiedCfg struct {
	verifiedPlugins
}

type verifiedPlugins struct {
	Checked map[string]*verifiedRunner
	Panicky map[string]*panicOnVerify
}

// TestLoadSkipsAConfigThePluginRefuses is the case that was still stopping the runner.
//
// A tag full of punctuation is a perfectly good INI string, so the file loads: nothing
// before the plugin's own Verify can tell it apart from a working configuration. It was
// therefore merged into the live config and only blew up later, at tag negotiation, by
// which point the ingester was already refusing to start and could not say why.
func TestLoadSkipsAConfigThePluginRefuses(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	goodID, badID := uuid.New(), uuid.New()
	writeConf(t, dir, `Checked`, `good`, goodID,
		"[Checked \"good\"]\n\tTag-Name=`fine`\n\tInterval=`1s`\n")
	writeConf(t, dir, `Checked`, `bad`, badID,
		"[Checked \"bad\"]\n\tTag-Name=`30984r09saokdj;lksaj;lk ;alsdkj f#)(*)!(@*`\n\tInterval=`5m`\n")

	var cfg verifiedCfg
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("a configuration the plugin refuses stopped the whole load: %v", err)
	}

	// the good one runs
	if cfg.Checked[`good`] == nil {
		t.Fatal(`the good configuration was not loaded`)
	}
	// and the bad one is nowhere near the live configuration, so nothing downstream can
	// trip over it
	if cfg.Checked[`bad`] != nil {
		t.Errorf("a configuration the plugin refused was loaded anyway: %+v", cfg.Checked[`bad`])
	}

	// the reason is recorded against the right runner, ready to go upstream
	st, ok := loadStatus(t, dcm, badID)
	if !ok {
		t.Fatal(`nothing was reported for the refused configuration`)
	}
	if st.OK() {
		t.Errorf("the refused configuration reported clean: %+v", st)
	}
	if !strings.Contains(st.Error, `invalid tag`) {
		t.Errorf("the reported reason is not the plugin's: %q", st.Error)
	}
	if got, _ := loadStatus(t, dcm, goodID); !got.OK() && got.UUID == goodID {
		t.Errorf("the good configuration was reported as failing: %+v", got)
	}
}

// TestLoadSurvivesAPluginThatPanicsOnVerify keeps the load path from being a way to crash
// the ingester with a deployed value.
func TestLoadSurvivesAPluginThatPanicsOnVerify(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	goodID, boomID := uuid.New(), uuid.New()
	writeConf(t, dir, `Checked`, `good`, goodID, "[Checked \"good\"]\n\tTag-Name=`fine`\n")
	writeConf(t, dir, `Panicky`, `boom`, boomID, "[Panicky \"boom\"]\n\tTag-Name=`boom`\n")

	var cfg verifiedCfg
	// no recover here on purpose: a panic escaping Load fails this by crashing the run
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("Load failed rather than skipping the panicking configuration: %v", err)
	}
	if cfg.Checked[`good`] == nil {
		t.Error(`the good configuration was not loaded`)
	}
	if cfg.Panicky[`boom`] != nil {
		t.Error(`a configuration whose Verify panicked was loaded anyway`)
	}
	st, ok := loadStatus(t, dcm, boomID)
	if !ok || st.OK() {
		t.Fatalf("the panicking configuration was not reported as failing: %+v", st)
	}
	if !strings.Contains(st.Error, `panic`) {
		t.Errorf("the reason does not say it panicked: %q", st.Error)
	}
}

// TestLoadVerifiesOnlyWhatTheFileIntroduced guards the isolation the check depends on.
// Verify runs against a copy holding just this file, so one configuration can never be
// failed by something another file put in the target.
func TestLoadVerifiesOnlyWhatTheFileIntroduced(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)

	// something already in the live configuration that would not pass Verify
	cfg := verifiedCfg{verifiedPlugins{
		Checked: map[string]*verifiedRunner{
			`preexisting`: {Tag_Name: `already;bad tag!`},
		},
	}}
	id := uuid.New()
	writeConf(t, dir, `Checked`, `new`, id, "[Checked \"new\"]\n\tTag-Name=`fine`\n")

	if err := dcm.Load(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Checked[`new`] == nil {
		t.Error(`a good configuration was blamed for what was already in the target`)
	}
	if st, ok := loadStatus(t, dcm, id); ok && !st.OK() {
		t.Errorf("a good configuration was reported as failing: %+v", st)
	}
}
