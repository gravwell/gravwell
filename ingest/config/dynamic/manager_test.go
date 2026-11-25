/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config"
)

// mockRunner is a stand in for a real runner config: a lifted UUID, a few primitives,
// a secret that must never be written out and an unexported member that is not config.
type mockRunner struct {
	Ingester_UUID string
	Tag_Name      string
	Count         int
	Enabled       bool
	Token         string `json:"-"`
	derived       int
}

// loadedRunner is what a written config is read back into.  It has to be a named type,
// gcfg will not populate an anonymous struct consistently across more than one file.
type loadedRunner struct {
	Ingester_UUID string
	Tag_Name      string
	Count         int
	Enabled       bool
}

// newTestManager builds a DynamicConfigManager over a temporary storage directory and
// hands back the concrete type so the tests can reach Available and Configured.
func newTestManager(t *testing.T) *DynamicConfigManager {
	t.Helper()
	c := Config{
		Webserver:  []string{`10.0.0.1:8080`},
		Auth_Token: `token`,
		Storage:    t.TempDir(),
	}
	m, err := NewDynamicConfigManager(context.Background(), c, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dcm, ok := m.(*DynamicConfigManager)
	if !ok {
		t.Fatalf("NewDynamicConfigManager returned a %T", m)
	}
	return dcm
}

// confFiles lists the .conf files in the storage directory, which is what a loader sees.
func confFiles(t *testing.T, dir string) []string {
	t.Helper()
	dents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, dent := range dents {
		if filepath.Ext(dent.Name()) == `.conf` {
			out = append(out, dent.Name())
		}
	}
	return out
}

// TestNewDynamicConfigManager checks that the constructor validates the config it is
// handed and fills in the optional arguments.
func TestNewDynamicConfigManager(t *testing.T) {
	// a config that cannot validate must not produce a manager.  Config.Enabled gates
	// this, a wholly empty config is disabled rather than invalid, so break one that has
	// been partly filled in
	bad := Config{Webserver: []string{`10.0.0.1:8080`}} // no Auth-Token, no Storage
	if _, err := NewDynamicConfigManager(context.Background(), bad, uuid.New(), nil); err == nil {
		t.Error(`an invalid config should not produce a manager`)
	}

	// a nil context and a nil logger are both filled in
	c := Config{Webserver: []string{`10.0.0.1:8080`}, Auth_Token: `token`, Storage: t.TempDir()}
	m, err := NewDynamicConfigManager(context.TODO(), c, uuid.New(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dcm := m.(*DynamicConfigManager)
	if dcm.ctx == nil || dcm.lgr == nil {
		t.Error(`nil context and logger should have been populated`)
	}
	// Verify normalizes as it goes, the manager keeps the normalized copy
	if dcm.Webserver[0] != `http://10.0.0.1:8080` {
		t.Errorf("the manager did not keep the validated config, Webserver = %v", dcm.Webserver)
	}
	// Signal hands back a real channel now, and it must not fire until something has
	// actually changed on disk
	ch := dcm.Signal()
	if ch == nil {
		t.Fatal(`Signal returned nil`)
	}
	select {
	case <-ch:
		t.Error(`Signal fired with nothing configured`)
	default:
	}
	if err = dcm.Close(); err != nil {
		t.Error(err)
	}
}

// TestDynamicRegisterRunnerWritesConf covers the happy path: the runner is recorded, a
// .conf lands in the storage directory and it carries what the runner was registered with.
func TestDynamicRegisterRunnerWritesConf(t *testing.T) {
	dcm := newTestManager(t)
	if err := dcm.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	guid := uuid.New()
	if err := dcm.RegisterRunner(`prod`, `okta`, guid, mockRunner{
		Tag_Name: `okta`,
		Count:    4,
		Enabled:  true,
		Token:    `hunter2`,
	}); err != nil {
		t.Fatal(err)
	}

	if len(dcm.Configured) != 1 {
		t.Fatalf("Configured = %d, want 1", len(dcm.Configured))
	}
	cr := dcm.Configured[0]
	if cr.Kind != `okta` || cr.Name != `prod` || cr.UUID != guid {
		t.Fatalf("bad runner %+v", cr.RunnerDefinition)
	}

	// the file is named kind_name_guid.conf and the runner remembers where it went
	want := filepath.Join(dcm.Storage, `okta_prod_`+guid.String()+`.conf`)
	if cr.backingFile != want {
		t.Errorf("backingFile = %q, want %q", cr.backingFile, want)
	}
	if got := confFiles(t, dcm.Storage); len(got) != 1 || got[0] != filepath.Base(want) {
		t.Fatalf("storage holds %v, want just %q", got, filepath.Base(want))
	}

	b, err := os.ReadFile(cr.backingFile)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	t.Logf("wrote:\n%s", body)
	for _, sub := range []string{"[okta \"prod\"]", `Ingester-UUID=` + guid.String(), "Tag-Name=`okta`", `Count=4`, `Enabled=true`} {
		if !strings.Contains(body, sub) {
			t.Errorf("config is missing %q", sub)
		}
	}
	// a json:"-" member is a secret, it is described but never written out
	if strings.Contains(body, `hunter2`) {
		t.Error(`the secret was written to disk`)
	}
	// an unexported member is not config at all
	if strings.Contains(body, `derived`) {
		t.Error(`an unexported member was written to disk`)
	}
	// exactly one section header, the INI block already carries it
	if n := strings.Count(body, `[okta`); n != 1 {
		t.Errorf("found %d section headers, want 1", n)
	}
}

// TestDynamicRegisterRunnerRoundTrip checks that what RegisterRunner writes is actually
// consumable by the config overlay loader, which is the whole point of writing it.
func TestDynamicRegisterRunnerRoundTrip(t *testing.T) {
	dcm := newTestManager(t)
	if err := dcm.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`prod`, `dev`} {
		if err := dcm.RegisterRunner(name, `okta`, uuid.New(), mockRunner{
			Tag_Name: `okta-` + name,
			Count:    7,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var target struct {
		Okta map[string]*loadedRunner
	}
	if err := config.LoadConfigOverlays(&target, dcm.Storage); err != nil {
		t.Fatalf("the written configs do not load back: %v", err)
	}
	if len(target.Okta) != 2 {
		t.Fatalf("loaded %d runners, want 2", len(target.Okta))
	}
	for _, name := range []string{`prod`, `dev`} {
		got, ok := target.Okta[name]
		if !ok {
			t.Fatalf("%s did not load", name)
		} else if got.Tag_Name != `okta-`+name {
			t.Errorf("%s: Tag-Name = %q", name, got.Tag_Name)
		} else if got.Count != 7 {
			t.Errorf("%s: Count = %d, want 7", name, got.Count)
		}
	}

	// Load rejects the same junk the NopManager does before it touches the disk
	for _, bad := range []any{nil, 42} {
		if err := dcm.Load(bad); err == nil {
			t.Errorf("Load(%v) should have failed", bad)
		}
	}

	// Load goes through the same path
	var viaLoad struct {
		Okta map[string]*loadedRunner
	}
	if err := dcm.Load(&viaLoad); err != nil {
		t.Fatal(err)
	} else if len(viaLoad.Okta) != 2 {
		t.Errorf("Load picked up %d runners, want 2", len(viaLoad.Okta))
	}
}

// TestDynamicRegisterRunnerUUID covers where the UUID in the file name comes from.
func TestDynamicRegisterRunnerUUID(t *testing.T) {
	dcm := newTestManager(t)
	if err := dcm.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	// no UUID anywhere, one is generated and it is the one used for the file name
	if err := dcm.RegisterRunner(`gen`, `okta`, uuid.Nil(), mockRunner{Tag_Name: `a`}); err != nil {
		t.Fatal(err)
	}
	cr := dcm.Configured[0]
	if cr.UUID == uuid.Nil() {
		t.Fatal(`a UUID should have been generated`)
	} else if !strings.Contains(cr.backingFile, cr.UUID.String()) {
		t.Errorf("file %q does not carry the generated UUID %v", cr.backingFile, cr.UUID)
	}
	if _, err := os.Stat(cr.backingFile); err != nil {
		t.Error(err)
	}

	// no UUID passed but the config carries one, that one wins
	lifted := uuid.New()
	if err := dcm.RegisterRunner(`lift`, `okta`, uuid.Nil(), mockRunner{
		Ingester_UUID: lifted.String(),
		Tag_Name:      `b`,
	}); err != nil {
		t.Fatal(err)
	}
	if got := dcm.Configured[1].UUID; got != lifted {
		t.Errorf("UUID = %v, want the one from the config %v", got, lifted)
	}

	// an explicit UUID beats the one in the config
	explicit := uuid.New()
	if err := dcm.RegisterRunner(`explicit`, `okta`, explicit, mockRunner{
		Ingester_UUID: lifted.String(),
		Tag_Name:      `c`,
	}); err != nil {
		t.Fatal(err)
	}
	if got := dcm.Configured[2].UUID; got != explicit {
		t.Errorf("UUID = %v, want the explicit %v", got, explicit)
	}
}

// TestDynamicRegisterRunnerRejects checks that a rejected registration is recorded
// nowhere, not in memory and not on disk.
func TestDynamicRegisterRunnerRejects(t *testing.T) {
	dcm := newTestManager(t)
	if err := dcm.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := dcm.RegisterKind(`wiz`, true, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := dcm.RegisterRunner(`prod`, `okta`, uuid.New(), mockRunner{Tag_Name: `a`}); err != nil {
		t.Fatal(err)
	}
	if err := dcm.RegisterRunner(`one`, `wiz`, uuid.New(), mockRunner{Tag_Name: `b`}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		want error
		fn   func() error
	}{
		{`unknown kind`, ErrUnknownKind, func() error {
			return dcm.RegisterRunner(`x`, `nope`, uuid.New(), mockRunner{})
		}},
		{`duplicate name`, ErrRunnerRegistered, func() error {
			return dcm.RegisterRunner(`prod`, `okta`, uuid.New(), mockRunner{})
		}},
		{`second singleton`, ErrSingletonRegistered, func() error {
			return dcm.RegisterRunner(`two`, `wiz`, uuid.New(), mockRunner{})
		}},
		{`empty name`, nil, func() error {
			return dcm.RegisterRunner(``, `okta`, uuid.New(), mockRunner{})
		}},
		{`empty kind`, nil, func() error {
			return dcm.RegisterRunner(`x`, ``, uuid.New(), mockRunner{})
		}},
		{`nil config`, nil, func() error {
			return dcm.RegisterRunner(`x`, `okta`, uuid.New(), nil)
		}},
		{`not a struct`, nil, func() error {
			return dcm.RegisterRunner(`x`, `okta`, uuid.New(), 42)
		}},
		{`unrepresentable value`, ErrUnrepresentable, func() error {
			return dcm.RegisterRunner(`ctl`, `okta`, uuid.New(), mockRunner{Tag_Name: "a`b\x01c"})
		}},
	} {
		err := tc.fn()
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
		} else if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}

	// the two good registrations are all that survived, and nothing extra hit the disk
	if len(dcm.Configured) != 2 {
		t.Errorf("Configured = %d, want 2", len(dcm.Configured))
	}
	if got := confFiles(t, dcm.Storage); len(got) != 2 {
		t.Errorf("storage holds %v, want 2 files", got)
	}
	// a failed write must not leave a temp file behind either
	dents, err := os.ReadDir(dcm.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if len(dents) != 2 {
		names := make([]string, 0, len(dents))
		for _, dent := range dents {
			names = append(names, dent.Name())
		}
		t.Errorf("storage holds %v, want only the two configs", names)
	}
}

// TestDynamicRegisterRunnerStorageFailure checks that a runner is not claimed when its
// config cannot be written.
func TestDynamicRegisterRunnerStorageFailure(t *testing.T) {
	dcm := newTestManager(t)
	if err := dcm.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	// drop the storage directory out from under the manager
	if err := os.RemoveAll(dcm.Storage); err != nil {
		t.Fatal(err)
	}
	if err := dcm.RegisterRunner(`prod`, `okta`, uuid.New(), mockRunner{Tag_Name: `a`}); err == nil {
		t.Fatal(`a runner whose config cannot be written should not register`)
	}
	if len(dcm.Configured) != 0 {
		t.Errorf("Configured = %d, want 0, a runner with no config on disk is not registered", len(dcm.Configured))
	}
}

// TestFnameChunk covers the file name sanitizer, path separators and traversal in
// particular since kind and name reach it from a config.
func TestFnameChunk(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{`okta`, `okta`},
		{`Okta-2_b`, `Okta-2_b`},
		{``, ``},
		{`my runner`, `myrunner`},
		{`../../etc/passwd`, `etcpasswd`},
		{`a/b\c`, `abc`},
		{`.`, ``},
		{`..`, ``},
		{`a.conf`, `aconf`},
		{"tab\there", `tabhere`},
		{"null\x00byte", `nullbyte`},
		{`c:\windows`, `cwindows`},
		{`a:b*c?d"e<f>g|h`, `abcdefgh`},
		{`héllo`, `hllo`},
	} {
		if got := fnameChunk(tc.in); got != tc.want {
			t.Errorf("fnameChunk(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// whatever comes out has to be usable as a single path element
	for _, in := range []string{`../../etc/passwd`, `a/b`, `.`, `..`, `a b`} {
		got := fnameChunk(in)
		if got == `` {
			continue // nothing survived, which is safe by definition
		} else if got != filepath.Base(got) || strings.ContainsAny(got, `/\`) {
			t.Errorf("fnameChunk(%q) = %q, which is not a single path element", in, got)
		}
	}
}

// TestWriteConfFile covers the temp file dance directly.
func TestWriteConfFile(t *testing.T) {
	dir := t.TempDir()
	pth := filepath.Join(dir, `x.conf`)
	if err := writeConfFile(pth, "[a \"b\"]\n"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(pth); err != nil {
		t.Fatal(err)
	} else if string(b) != "[a \"b\"]\n" {
		t.Errorf("wrote %q", string(b))
	}
	// the temp file is gone and, just as importantly, never looked like a config
	dents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dents) != 1 || dents[0].Name() != `x.conf` {
		t.Errorf("directory holds more than the config: %v", dents)
	}

	// an overwrite replaces the contents rather than appending to them
	if err := writeConfFile(pth, "[c \"d\"]\n"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(pth); err != nil {
		t.Fatal(err)
	} else if string(b) != "[c \"d\"]\n" {
		t.Errorf("overwrite produced %q", string(b))
	}

	// a path we cannot write fails and leaves nothing behind
	if err := writeConfFile(filepath.Join(dir, `nope`, `x.conf`), "x"); err == nil {
		t.Error(`writing into a missing directory should fail`)
	}
	if dents, err = os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else if len(dents) != 1 {
		t.Errorf("a failed write left something behind: %v", dents)
	}

	// a destination we cannot rename over fails after the temp file is written, which
	// still has to be cleaned up
	blocked := filepath.Join(dir, `blocked.conf`)
	if err := os.Mkdir(blocked, 0770); err != nil {
		t.Fatal(err)
	}
	if err := writeConfFile(blocked, "x"); err == nil {
		t.Error(`renaming over a directory should fail`)
	}
	if _, err := os.Stat(blocked + `.temp`); !os.IsNotExist(err) {
		t.Errorf("the temp file was not cleaned up: %v", err)
	}
}

// TestNopManagerRegisterKind covers kind registration, which both managers share.
func TestNopManagerRegisterKind(t *testing.T) {
	var n NopManager
	if err := n.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if len(n.Available) != 1 {
		t.Fatalf("Available = %d, want 1", len(n.Available))
	}
	// a kind describes the type, it carries no identity
	a := n.Available[0]
	if a.Kind != `okta` || a.Name != `` || a.UUID != uuid.Nil() || a.Singleton {
		t.Fatalf("bad kind registration %+v", a)
	}
	// unexported members are not config and the lifted UUID is not a variable
	for _, v := range a.Variables {
		if v.Name == `derived` || v.Name == ingesterUUIDName {
			t.Errorf("%s should not be a variable", v.Name)
		}
	}
	// the secret is described so a GUI can ask for it, it just carries no value
	if v, ok := findVar(a.RunnerDefinition, `Token`); !ok {
		t.Error(`the secret should still be described`)
	} else if v.Value != nil {
		t.Errorf("the secret carries a value %v", v.Value)
	}

	// the singleton flag is recorded
	if err := n.RegisterKind(`wiz`, true, mockRunner{}); err != nil {
		t.Fatal(err)
	} else if !n.Available[1].Singleton {
		t.Error(`the singleton flag was not recorded`)
	}

	// one entry per kind
	if err := n.RegisterKind(`okta`, false, mockRunner{}); !errors.Is(err, ErrKindRegistered) {
		t.Errorf("want ErrKindRegistered, got %v", err)
	}

	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{`empty kind`, func() error { return n.RegisterKind(``, false, mockRunner{}) }},
		{`nil type`, func() error { return n.RegisterKind(`x`, false, nil) }},
		{`not a struct`, func() error { return n.RegisterKind(`x`, false, 42) }},
		{`unsupported member`, func() error {
			return n.RegisterKind(`x`, false, struct{ Bad map[string]string }{})
		}},
	} {
		if err := tc.fn(); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	if len(n.Available) != 2 {
		t.Errorf("Available = %d, want 2, a failed registration should not be recorded", len(n.Available))
	}
}

// TestNopManagerRegisterRunner covers the in memory half of runner registration, the
// NopManager has nowhere to write a config so it just remembers what it was handed.
func TestNopManagerRegisterRunner(t *testing.T) {
	var n NopManager
	// a kind has to be registered first
	if err := n.RegisterRunner(`prod`, `okta`, uuid.Nil(), mockRunner{}); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("want ErrUnknownKind, got %v", err)
	}
	if err := n.RegisterKind(`okta`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	guid := uuid.New()
	if err := n.RegisterRunner(`prod`, `okta`, guid, mockRunner{Tag_Name: `okta`, Count: 4}); err != nil {
		t.Fatal(err)
	}
	cr := n.Configured[0]
	if cr.Kind != `okta` || cr.Name != `prod` || cr.UUID != guid {
		t.Fatalf("bad runner %+v", cr.RunnerDefinition)
	}
	// nothing was written, so nothing is backing it
	if cr.backingFile != `` {
		t.Errorf("backingFile = %q, want empty", cr.backingFile)
	}

	// an empty UUID is generated rather than left nil
	if err := n.RegisterRunner(`dev`, `okta`, uuid.Nil(), mockRunner{Tag_Name: `okta`}); err != nil {
		t.Fatal(err)
	}
	if g := n.Configured[1].UUID; g == uuid.Nil() || g == guid {
		t.Errorf("UUID = %v, want a freshly generated one", g)
	}

	// the same name within a kind is a duplicate, the same name under another kind is not
	if err := n.RegisterRunner(`prod`, `okta`, uuid.New(), mockRunner{}); !errors.Is(err, ErrRunnerRegistered) {
		t.Errorf("want ErrRunnerRegistered, got %v", err)
	}
	if err := n.RegisterKind(`jamf`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := n.RegisterRunner(`prod`, `jamf`, uuid.New(), mockRunner{}); err != nil {
		t.Errorf("the same name under a different kind should register: %v", err)
	}

	// a singleton kind takes exactly one runner, whatever it is named
	if err := n.RegisterKind(`wiz`, true, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := n.RegisterRunner(`one`, `wiz`, uuid.Nil(), mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if !n.Configured[3].Singleton {
		t.Error(`the singleton flag did not carry to the configured runner`)
	}
	if err := n.RegisterRunner(`two`, `wiz`, uuid.Nil(), mockRunner{}); !errors.Is(err, ErrSingletonRegistered) {
		t.Errorf("want ErrSingletonRegistered, got %v", err)
	}

	if len(n.Configured) != 4 {
		t.Errorf("Configured = %d, want 4", len(n.Configured))
	}
}

// TestNopManagerLoad covers the argument checking the other managers lean on.
func TestNopManagerLoad(t *testing.T) {
	var n NopManager
	var target struct{ Okta map[string]*loadedRunner }
	if err := n.Load(&target); err != nil {
		t.Errorf("a pointer to a struct should load: %v", err)
	}
	for _, tc := range []struct {
		name string
		v    any
	}{
		{`nil`, nil},
		{`not a pointer`, target},
		{`plain value`, 42},
	} {
		if err := n.Load(tc.v); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}
