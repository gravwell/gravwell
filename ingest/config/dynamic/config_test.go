/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"uuid"
)

type enumCfg struct {
	Ingester_UUID string
	Mode          string   `dynamic:"enum=static|environment|ec2role"`
	Kinds         []string `dynamic:"enum=alpha|beta|gamma"`
	Key           string   `dynamic:"requiredif=Mode:|static"`
	Free          string
}

// syncDef builds a definition the webserver would hand down for the mock kind.
func syncDef(t *testing.T, name string, id uuid.UUID, tag string) RunnerDefinition {
	t.Helper()
	rd, err := MapRunnerDefinition(`mock`, name, mockRunner{Tag_Name: tag})
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = id
	return rd
}

// confNames lists the config files in a directory.
func confNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var r []string
	for _, e := range ents {
		if filepath.Ext(e.Name()) == confExt {
			r = append(r, e.Name())
		}
	}
	return r
}

// testConfig is a Config that is valid apart from whatever the caller overrides.
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Webserver:  []string{`10.0.0.1:8080`},
		Auth_Token: `token`,
		Storage:    t.TempDir(),
	}
}

// TestConfigVerifyRequired covers the guards on the required members.  Class is
// optional, so a config that omits it must still validate.
func TestConfigVerifyRequired(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		c    Config
	}{
		{`no webserver`, Config{Auth_Token: `t`, Storage: dir}},
		{`empty webserver list`, Config{Webserver: []string{}, Auth_Token: `t`, Storage: dir}},
		{`no auth token`, Config{Webserver: []string{`10.0.0.1`}, Storage: dir}},
		{`no storage`, Config{Webserver: []string{`10.0.0.1`}, Auth_Token: `t`}},
	} {
		if err := tc.c.Verify(); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	c := Config{}
	if c.Enabled() {
		t.Fatal("failed to catch empty struct as not enabled")
	} else if err := c.Verify(); err != nil {
		t.Fatal("Verify on empty Config failed", err)
	}

	// a nil config is a programming error, not a panic
	var nilc *Config
	if err := nilc.Verify(); err == nil {
		t.Error(`a nil config should not validate`)
	}

	// Class is optional
	c = testConfig(t)
	if err := c.Verify(); err != nil {
		t.Errorf("a config with no Class should validate: %v", err)
	}
	c = testConfig(t)
	c.Class = `edge`
	if err := c.Verify(); err != nil {
		t.Errorf("a config with a Class should validate: %v", err)
	}
}

// TestConfigVerifyWebserverNormalize checks that endpoints are parsed as URLs and that
// an endpoint with no protocol gets http:// attached.
func TestConfigVerifyWebserverNormalize(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{`bare host and port`, `10.0.0.1:8080`, `http://10.0.0.1:8080`},
		{`bare host`, `localhost`, `http://localhost`},
		{`bare host with path`, `foo.example.com:443/api`, `http://foo.example.com:443/api`},
		{`bare IPv6`, `[::1]:8080`, `http://[::1]:8080`},
		{`http preserved`, `http://10.0.0.1:8080`, `http://10.0.0.1:8080`},
		{`https preserved`, `https://foo.example.com/api/v1`, `https://foo.example.com/api/v1`},
		{`scheme lowercased`, `HTTPS://foo.example.com`, `https://foo.example.com`},
		{`surrounding whitespace`, "  10.0.0.1:8080\t", `http://10.0.0.1:8080`},
		// a protocol relative endpoint already contains a :// in its path, so it never
		// picks up the prefix and has to have its scheme filled in after the parse
		{`protocol relative`, `//foo.example.com/a://b`, `http://foo.example.com/a://b`},
		{`protocol relative with port`, `//foo.example.com:8080/x://y`, `http://foo.example.com:8080/x://y`},
	} {
		c := testConfig(t)
		c.Webserver = []string{tc.in}
		if err := c.Verify(); err != nil {
			t.Errorf("%s: %q should validate: %v", tc.name, tc.in, err)
		} else if c.Webserver[0] != tc.want {
			t.Errorf("%s: %q normalized to %q, want %q", tc.name, tc.in, c.Webserver[0], tc.want)
		}
	}

	// every endpoint in the list is normalized, in place and in order
	c := testConfig(t)
	c.Webserver = []string{`10.0.0.1:8080`, `https://foo.example.com`, ` localhost `}
	want := []string{`http://10.0.0.1:8080`, `https://foo.example.com`, `http://localhost`}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(c.Webserver, want) {
		t.Errorf("normalized to %v, want %v", c.Webserver, want)
	}

	// normalization is stable, validating an already validated config changes nothing
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	} else if !reflect.DeepEqual(c.Webserver, want) {
		t.Errorf("revalidation changed the endpoints to %v, want %v", c.Webserver, want)
	}
}

// TestConfigVerifyWebserverErrors covers endpoints that cannot be used.
func TestConfigVerifyWebserverErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		ws   []string
	}{
		{`empty endpoint`, []string{``}},
		{`whitespace endpoint`, []string{"  \t"}},
		{`empty among valid`, []string{`10.0.0.1:8080`, ``}},
		{`no host`, []string{`http://`}},
		{`no host with path`, []string{`http:///api`}},
		{`ftp scheme`, []string{`ftp://foo.example.com`}},
		{`file scheme`, []string{`file:///tmp/foo`}},
		{`ws scheme`, []string{`ws://foo.example.com`}},
		{`control character`, []string{"http://foo.example.com/\x7f"}},
		{`bad escape`, []string{`http://foo.example.com/%zz`}},
		{`valid then invalid`, []string{`https://foo.example.com`, `ftp://bar.example.com`}},
	} {
		c := testConfig(t)
		c.Webserver = tc.ws
		if err := c.Verify(); err == nil {
			t.Errorf("%s: %v should not validate", tc.name, tc.ws)
		}
	}
}

// TestConfigVerifyStorageCreate checks that a missing Storage directory is created,
// including any missing parents.
func TestConfigVerifyStorageCreate(t *testing.T) {
	c := testConfig(t)
	c.Storage = filepath.Join(c.Storage, `parent`, `storage`)
	if err := c.Verify(); err != nil {
		t.Fatalf("Storage should have been created: %v", err)
	}
	if fi, err := os.Stat(c.Storage); err != nil {
		t.Fatalf("Storage was not created: %v", err)
	} else if !fi.IsDir() {
		t.Fatal(`Storage is not a directory`)
	}

	// an existing directory is accepted as is
	if err := c.Verify(); err != nil {
		t.Errorf("an existing Storage directory should validate: %v", err)
	}
}

// TestConfigVerifyStorageErrors covers Storage paths we cannot use.
func TestConfigVerifyStorageErrors(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, `file`)
	if err := os.WriteFile(fpath, []byte(`data`), 0660); err != nil {
		t.Fatal(err)
	}

	// Storage is a regular file rather than a directory
	c := testConfig(t)
	c.Storage = fpath
	if err := c.Verify(); err == nil {
		t.Error(`a file should not be accepted as Storage`)
	}

	// a parent component of the path is a regular file, the stat fails with something
	// other than a not exist error and we must not try to create it
	c = testConfig(t)
	c.Storage = filepath.Join(fpath, `storage`)
	if err := c.Verify(); err == nil {
		t.Error(`a Storage path below a file should not validate`)
	}

	// the file we walked through must be left alone
	if b, err := os.ReadFile(fpath); err != nil {
		t.Fatal(err)
	} else if string(b) != `data` {
		t.Errorf(`the file at the Storage path was modified: %q`, string(b))
	}
}

// TestConfigVerifyStorageNotWritable checks that a directory we cannot create files in
// is rejected, permission bits alone are not enough to know that.
func TestConfigVerifyStorageNotWritable(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip(`mode bits do not gate writes on windows`)
	} else if os.Geteuid() == 0 {
		t.Skip(`root ignores the mode bits`)
	}
	dir := filepath.Join(t.TempDir(), `readonly`)
	if err := os.Mkdir(dir, 0500); err != nil {
		t.Fatal(err)
	}
	c := testConfig(t)
	c.Storage = dir
	if err := c.Verify(); err == nil {
		t.Error(`a read only Storage directory should not validate`)
	}

	// a missing Storage below a directory we cannot write to cannot be created
	c = testConfig(t)
	c.Storage = filepath.Join(dir, `storage`)
	if err := c.Verify(); err == nil {
		t.Error(`a Storage directory that cannot be created should not validate`)
	}
}

// TestConfigVerifyStorageClean makes sure the writability probe does not leave
// anything behind in the storage directory.
func TestConfigVerifyStorageClean(t *testing.T) {
	c := testConfig(t)
	for range 4 {
		if err := c.Verify(); err != nil {
			t.Fatal(err)
		}
	}
	if dents, err := os.ReadDir(c.Storage); err != nil {
		t.Fatal(err)
	} else if len(dents) != 0 {
		names := make([]string, 0, len(dents))
		for _, dent := range dents {
			names = append(names, dent.Name())
		}
		t.Errorf(`Storage should be empty, found %v`, names)
	}
}

// TestEnumIsCarried covers the declaration reaching whoever draws the form.
func TestEnumIsCarried(t *testing.T) {
	rd, err := MapRunnerDefinition(`enum`, `probe`, enumCfg{})
	if err != nil {
		t.Fatal(err)
	}
	mode, ok := findVar(rd, `Mode`)
	if !ok {
		t.Fatal(`no Mode variable`)
	}
	if strings.Join(mode.Enum, `,`) != `static,environment,ec2role` {
		t.Errorf("Mode enum = %v", mode.Enum)
	}
	kinds, _ := findVar(rd, `Kinds`)
	if strings.Join(kinds.Enum, `,`) != `alpha,beta,gamma` {
		t.Errorf("Kinds enum = %v", kinds.Enum)
	}
	if free, _ := findVar(rd, `Free`); len(free.Enum) != 0 {
		t.Errorf("an untagged member picked up an enum: %v", free.Enum)
	}
}

// TestEnumIsEnforced is what stops a value outside the set being stored and only refused
// later by the plugin.
func TestEnumIsEnforced(t *testing.T) {
	rd, err := MapRunnerDefinition(`enum`, `probe`, enumCfg{})
	if err != nil {
		t.Fatal(err)
	}
	mode, _ := findVar(rd, `Mode`)
	if err = (Variable{Name: mode.Name, Type: mode.Type, Enum: mode.Enum, Value: `static`}).Validate(); err != nil {
		t.Errorf("a declared value was refused: %v", err)
	}
	err = (Variable{Name: mode.Name, Type: mode.Type, Enum: mode.Enum, Value: `nonsense`}).Validate()
	if err == nil {
		t.Fatal(`a value outside the enum was accepted`)
	}
	// the message has to say what is allowed, an operator reads this with no other context
	for _, must := range []string{`nonsense`, `static`, `environment`, `ec2role`} {
		if !strings.Contains(err.Error(), must) {
			t.Errorf("the error does not mention %q: %v", must, err)
		}
	}

	// a list is checked entry by entry
	kinds, _ := findVar(rd, `Kinds`)
	good := Variable{Name: kinds.Name, Type: kinds.Type, Enum: kinds.Enum, Value: []string{`alpha`, `gamma`}}
	if err = good.Validate(); err != nil {
		t.Errorf("declared list values were refused: %v", err)
	}
	bad := Variable{Name: kinds.Name, Type: kinds.Type, Enum: kinds.Enum, Value: []string{`alpha`, `delta`}}
	if err = bad.Validate(); err == nil {
		t.Error(`a list carrying an undeclared value was accepted`)
	}
	// and after a JSON round trip, where a list arrives as []any
	viaJSON := Variable{Name: kinds.Name, Type: kinds.Type, Enum: kinds.Enum, Value: []any{`alpha`, `delta`}}
	if err = viaJSON.Validate(); err == nil {
		t.Error(`an undeclared value survived a JSON round trip`)
	}
}

// TestRequiredWhen covers the conditional requirement: a member needed only alongside a
// particular choice elsewhere, which a flat form cannot express on its own.
func TestRequiredWhen(t *testing.T) {
	rd, err := MapRunnerDefinition(`enum`, `probe`, enumCfg{})
	if err != nil {
		t.Fatal(err)
	}
	key, ok := findVar(rd, `Key`)
	if !ok {
		t.Fatal(`no Key variable`)
	}
	if key.Required {
		t.Error(`a conditionally required member must not be marked always required`)
	}
	if key.RequiredWhen == nil || key.RequiredWhen.Field != `Mode` {
		t.Fatalf("no condition recorded: %+v", key.RequiredWhen)
	}

	set := func(mode any) RunnerDefinition {
		out := rd
		out.Variables = append([]Variable(nil), rd.Variables...)
		for i := range out.Variables {
			if out.Variables[i].Name == `Mode` {
				out.Variables[i].Value = mode
			}
		}
		return out
	}
	for _, tc := range []struct {
		mode any
		want bool
	}{
		{`static`, true},
		// unset means static, see sqs_common.GetCredentials, so the empty entry in the
		// condition has to match or leaving the field alone drops the requirement
		{nil, true},
		{``, true},
		{`environment`, false},
		{`ec2role`, false},
	} {
		if got := set(tc.mode).RequiredNow(key); got != tc.want {
			t.Errorf("Mode=%v: required = %v, want %v", tc.mode, got, tc.want)
		}
	}

	// an always-required member stays required whatever else is going on
	if !set(`ec2role`).RequiredNow(Variable{Name: `x`, Required: true}) {
		t.Error(`an unconditional requirement was dropped`)
	}
}

// TestBadEnumTagsAreRefused keeps a mistyped tag from silently doing nothing.
func TestBadEnumTagsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
	}{
		{`enum on a number`, struct {
			N int `dynamic:"enum=1|2"`
		}{}},
		{`enum on a bool`, struct {
			B bool `dynamic:"enum=yes|no"`
		}{}},
		{`empty enum`, struct {
			S string `dynamic:"enum="`
		}{}},
		{`requiredif with no value`, struct {
			S string `dynamic:"requiredif=Other"`
		}{}},
		{`requiredif with no field`, struct {
			S string `dynamic:"requiredif=:static"`
		}{}},
		// a secret is masked and its value withheld, so there is nothing to offer a
		// choice of and nothing to check the choice against.  Accepted, the pair would be
		// inert in both directions.
		{`enum on a secret`, struct {
			S string `json:"-" dynamic:"secret,enum=a|b"`
		}{}},
		{`enum on a secret, tags reversed`, struct {
			S string `json:"-" dynamic:"enum=a|b,secret"`
		}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MapRunnerDefinition(`k`, `n`, tc.v); err == nil {
				t.Error(`a malformed tag was accepted, so it would silently do nothing`)
			}
		})
	}
}

// TestAuthTokenFromEnv covers supplying the shared control secret through the environment,
// which is how a container hands the same value to the webserver and to the ingester
// without baking a credential into an image.
func TestAuthTokenFromEnv(t *testing.T) {
	const fromEnv = `secret-from-the-environment`
	const fromFile = `secret-from-the-config-file`

	newCfg := func(token string, dir string) *Config {
		return &Config{
			Webserver:  []string{`127.0.0.1:80`},
			Auth_Token: token,
			Storage:    dir,
		}
	}

	t.Run(`fills in an absent token`, func(t *testing.T) {
		t.Setenv(envIngestControlAuth, fromEnv)
		c := newCfg(``, t.TempDir())
		if err := c.Verify(); err != nil {
			t.Fatalf("a config whose token comes from the environment was refused: %v", err)
		}
		if c.Auth_Token != fromEnv {
			t.Fatalf("token is %q, expected the environment's %q", c.Auth_Token, fromEnv)
		}
	})

	t.Run(`the config file wins`, func(t *testing.T) {
		// same precedence as every other secret here: the environment fills in a value
		// that is absent, it does not override one an operator wrote down
		t.Setenv(envIngestControlAuth, fromEnv)
		c := newCfg(fromFile, t.TempDir())
		if err := c.Verify(); err != nil {
			t.Fatal(err)
		}
		if c.Auth_Token != fromFile {
			t.Fatalf("token is %q, the environment overrode the config file", c.Auth_Token)
		}
	})

	t.Run(`still required when neither supplies it`, func(t *testing.T) {
		t.Setenv(envIngestControlAuth, ``)
		c := newCfg(``, t.TempDir())
		if err := c.Verify(); err == nil {
			t.Fatal("a config with no token from either source was accepted")
		}
	})

	t.Run(`the environment alone does not enable dynamic config`, func(t *testing.T) {
		// an ingester whose configuration never asked for dynamic config must not be
		// switched into it by an environment variable that happens to be set
		t.Setenv(envIngestControlAuth, fromEnv)
		var c Config
		if c.Enabled() {
			t.Fatal("an empty config reports itself enabled")
		}
		if err := c.Verify(); err != nil {
			t.Fatalf("verifying a disabled config failed: %v", err)
		}
		if c.Auth_Token != `` {
			t.Fatalf("a disabled config picked up a token %q from the environment", c.Auth_Token)
		}
	})
}

// TestSyncSweepsConfigsDeletedWhileDown is the restart case.
//
// The in memory list of configured runners is empty every time the process starts, so a
// configuration the server deleted while this ingester was down is remembered by nothing
// and was previously left on disk forever, loaded on every start.
func TestSyncSweepsConfigsDeletedWhileDown(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	stays, goes := uuid.New(), uuid.New()
	if err := dcm.Sync([]RunnerDefinition{
		syncDef(t, `stays`, stays, `a`),
		syncDef(t, `goes`, goes, `b`),
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(confNames(t, dir)); n != 2 {
		t.Fatalf("setup wrote %d configs, want 2", n)
	}

	// the restart: everything in memory is gone, the files are not
	dcm.Configured = nil

	// the server now only has one of them
	if err := dcm.Sync([]RunnerDefinition{syncDef(t, `stays`, stays, `a`)}); err != nil {
		t.Fatal(err)
	}
	names := confNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("after the sweep the directory holds %v, want just the surviving runner", names)
	}
	if !strings.Contains(names[0], stays.String()) {
		t.Errorf("the wrong config survived: %v", names)
	}
}

// TestSyncSweepsRenamedConfigs covers the rename, which leaves two files for one runner:
// the file name carries the name, so a rename writes a new file and the old one is
// orphaned under a UUID that is still live.
func TestSyncSweepsRenamedConfigs(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	if err := dcm.Sync([]RunnerDefinition{syncDef(t, `before`, id, `a`)}); err != nil {
		t.Fatal(err)
	}
	dcm.Configured = nil // the restart

	if err := dcm.Sync([]RunnerDefinition{syncDef(t, `after`, id, `a`)}); err != nil {
		t.Fatal(err)
	}
	names := confNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("a rename left %v, want one file", names)
	}
	if !strings.Contains(names[0], `after`) {
		t.Errorf("the stale name survived: %v", names)
	}
}

// TestSyncLeavesLocalConfigsAlone is the guard rail on the sweep.  A configuration the
// ingester registered for itself carries no marker, and the server has no standing to
// delete it however little it knows about it.
func TestSyncLeavesLocalConfigsAlone(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	// the ingester's own runner, written by RegisterRunner
	if err := dcm.RegisterRunner(`local`, `mock`, uuid.New(), mockRunner{Tag_Name: `x`}); err != nil {
		t.Fatal(err)
	}
	before := confNames(t, dir)
	if len(before) != 1 {
		t.Fatalf("setup wrote %v", before)
	}

	dcm.Configured = nil // the restart, so even the local runner is forgotten

	// the server sends nothing at all
	if err := dcm.Sync(nil); err != nil {
		t.Fatal(err)
	}
	after := confNames(t, dir)
	if len(after) != 1 || after[0] != before[0] {
		t.Errorf("the ingester's own configuration was swept: had %v, now %v", before, after)
	}
}

// TestSyncLeavesUnmarkedFilesAlone covers anything else in the directory: a file dropped
// in by hand is not the server's to remove.
func TestSyncLeavesUnmarkedFilesAlone(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	hand := filepath.Join(dir, `handwritten`+confExt)
	if err := os.WriteFile(hand, []byte("[mock \"hand\"]\n\tTag-Name=`x`\n"), 0660); err != nil {
		t.Fatal(err)
	}
	if err := dcm.Sync(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hand); err != nil {
		t.Errorf("a hand written config was swept: %v", err)
	}
}

// TestSyncKeepsTheLastGoodCopyOfARejectedRunner checks the sweep does not undo the thing
// the reconcile went out of its way to do: hold on to a working file when the server
// sends a version that will not run.
func TestSyncKeepsTheLastGoodCopyOfARejectedRunner(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := dcm.Sync([]RunnerDefinition{syncDef(t, `beat`, id, `good`)}); err != nil {
		t.Fatal(err)
	}
	good := confNames(t, dir)
	if len(good) != 1 {
		t.Fatalf("setup wrote %v", good)
	}

	// a definition carrying a value that cannot be written into a config file
	bad := syncDef(t, `beat`, id, "tag\x01with\\a control char")
	if err := dcm.Sync([]RunnerDefinition{bad}); err != nil {
		t.Fatal(err)
	}
	after := confNames(t, dir)
	if len(after) != 1 || after[0] != good[0] {
		t.Errorf("the working copy was swept when the server sent a bad one: had %v, now %v", good, after)
	}
}

// TestRemoteMarkerIsInvisibleToTheLoader checks the marker does not change what a config
// means, only who owns it.
func TestRemoteMarkerIsInvisibleToTheLoader(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}
	if err := dcm.Sync([]RunnerDefinition{syncDef(t, `beat`, uuid.New(), `thetag`)}); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Mock map[string]*mockRunner
	}
	if err := dcm.Load(&cfg); err != nil {
		t.Fatalf("a marked config would not load: %v", err)
	}
	if cfg.Mock[`beat`] == nil {
		t.Fatal(`the marked config did not load`)
	}
	if cfg.Mock[`beat`].Tag_Name != `thetag` {
		t.Errorf("the marker changed what loaded: %+v", cfg.Mock[`beat`])
	}
	if n := len(dcm.Statuses()); n > 0 {
		for _, rs := range dcm.Statuses() {
			if !rs.OK() {
				t.Errorf("a marked config was reported as failing to load: %+v", rs)
			}
		}
	}
}

// TestLegacyMarkerStillOwnsItsFile covers the upgrade.  The marker used to open with a
// semicolon and now opens with a hash, and a file already on disk carries whichever one
// wrote it.  The marker is the only thing that says a file is the server's to delete, so
// an ingester that stopped recognizing the old one would sweep nothing and load a deleted
// configuration on every start, forever.
func TestLegacyMarkerStillOwnsItsFile(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	// a file exactly as an older ingester left it
	stale := syncDef(t, `stale`, uuid.New(), `b`)
	ini, err := stale.INI()
	if err != nil {
		t.Fatal(err)
	}
	pth := dcm.runnerPath(stale)
	if err = writeConfFile(pth, legacyRemoteMarker+"\n"+ini); err != nil {
		t.Fatal(err)
	}
	if !isRemoteConfig(pth) {
		t.Fatal("a config carrying the old marker is no longer recognized as the server's")
	}

	// the server has since dropped it, and the sweep has to take it
	live := syncDef(t, `live`, uuid.New(), `a`)
	if err = dcm.Sync([]RunnerDefinition{live}); err != nil {
		t.Fatal(err)
	}
	names := confNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("the sweep left %v, want just the live runner", names)
	}

	// and a file the server still has is rewritten with the current marker
	pth = dcm.runnerPath(live)
	got, err := os.ReadFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), remoteMarker+"\n") {
		t.Errorf("a rewritten config does not open with the current marker:\n%s", got)
	}
}

// TestLegacyMarkerHealsOnRewrite is the other half: a configuration the server still has
// keeps its file, and the next write moves it to the current marker rather than leaving
// the old one on disk forever.
func TestLegacyMarkerHealsOnRewrite(t *testing.T) {
	dir := t.TempDir()
	dcm := newLoadManager(t, dir)
	if err := dcm.RegisterKind(`mock`, false, mockRunner{}); err != nil {
		t.Fatal(err)
	}

	rd := syncDef(t, `kept`, uuid.New(), `a`)
	ini, err := rd.INI()
	if err != nil {
		t.Fatal(err)
	}
	pth := dcm.runnerPath(rd)
	if err = writeConfFile(pth, legacyRemoteMarker+"\n"+ini); err != nil {
		t.Fatal(err)
	}

	if err = dcm.Sync([]RunnerDefinition{rd}); err != nil {
		t.Fatal(err)
	}
	if names := confNames(t, dir); len(names) != 1 {
		t.Fatalf("a config the server still has was not kept: %v", names)
	}
	got, err := os.ReadFile(pth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), remoteMarker+"\n") {
		t.Errorf("the old marker survived a rewrite:\n%s", got)
	}
}
