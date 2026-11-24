/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gcfg"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/plugins/jamf"
	"github.com/gravwell/gravwell/v4/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v4/hosted/plugins/msgraph"
	"github.com/gravwell/gravwell/v4/hosted/plugins/okta"
	"github.com/gravwell/gravwell/v4/hosted/plugins/sqs"
	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v4/hosted/plugins/wiz"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// These tests live here rather than next to the dynamic package to prevent import cycles

// exampleHarness mirrors the unexported cfgReadType in hosted/runner so that the plugin
// example.conf files parse here exactly as they do in production.
type exampleHarness struct {
	Global  config.IngestConfig
	Attach  attach.AttachConfig
	State   storage.BoltConfig
	Configs // the same embedded plugin config set the runner uses
}

// pluginCase describes one plugin: how to reach its config out of a parsed Configs, and
// which members are secrets that MapConfig deliberately withholds.
type pluginCase struct {
	kind    string
	example string   // path to the plugin's example.conf, relative to this package
	secrets []string // members tagged json:"-", zeroed before comparing
	// get pulls the named config out of a Configs, and reports the names present
	get func(Configs) (name string, cfg any, ok bool)
	// fresh returns an empty config of this plugin's type to read the generated INI into
	fresh func() any
	// extract pulls the config back out of whatever fresh returned
	extract func(target any, name string) (any, bool)
}

func cases() []pluginCase {
	return []pluginCase{
		{
			kind: `Okta`, example: `okta/example.conf`, secrets: []string{`Token`},
			get: func(c Configs) (string, any, bool) { return first(c.Okta) },
			fresh: func() any {
				return &struct{ Okta map[string]*okta.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ Okta map[string]*okta.Config }).Okta[n]
				return v, ok
			},
		},
		{
			kind: `Mimecast`, example: `mimecast/example.conf`,
			secrets: []string{`Client_Id`, `Client_Secret`},
			get:     func(c Configs) (string, any, bool) { return first(c.Mimecast) },
			fresh: func() any {
				return &struct{ Mimecast map[string]*mimecast.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ Mimecast map[string]*mimecast.Config }).Mimecast[n]
				return v, ok
			},
		},
		{
			kind: `Jamf`, example: `jamf/example.conf`, secrets: []string{`Client_Secret`},
			get: func(c Configs) (string, any, bool) { return first(c.Jamf) },
			fresh: func() any {
				return &struct{ Jamf map[string]*jamf.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ Jamf map[string]*jamf.Config }).Jamf[n]
				return v, ok
			},
		},
		{
			kind: `SQS`, example: `sqs/example.conf`, secrets: []string{`Secret`},
			get: func(c Configs) (string, any, bool) { return first(c.SQS) },
			fresh: func() any {
				return &struct{ SQS map[string]*sqs.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ SQS map[string]*sqs.Config }).SQS[n]
				return v, ok
			},
		},
		{
			kind: `Wiz`, example: `wiz/example.conf`,
			secrets: []string{`Client_Id`, `Client_Secret`},
			get:     func(c Configs) (string, any, bool) { return first(c.Wiz) },
			fresh: func() any {
				return &struct{ Wiz map[string]*wiz.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ Wiz map[string]*wiz.Config }).Wiz[n]
				return v, ok
			},
		},
		{
			kind: `Tester`, example: `tester/example.conf`,
			get: func(c Configs) (string, any, bool) { return first(c.Tester) },
			fresh: func() any {
				return &struct{ Tester map[string]*tester.Config }{}
			},
			extract: func(t any, n string) (any, bool) {
				v, ok := t.(*struct{ Tester map[string]*tester.Config }).Tester[n]
				return v, ok
			},
		},
	}
}

// first pulls the single entry out of a plugin's config map.
func first[T any](m map[string]*T) (string, any, bool) {
	for k, v := range m {
		return k, *v, true
	}
	return ``, nil, false
}

// zeroSecrets blanks the members MapConfig withholds so the comparison is about the
// members that are supposed to survive.
func zeroSecrets(v any, names []string) any {
	out := reflect.New(reflect.TypeOf(v)).Elem()
	out.Set(reflect.ValueOf(v))
	for _, n := range names {
		f := out.FieldByName(n)
		if f.IsValid() && f.CanSet() {
			f.SetZero()
		}
	}
	return out.Interface()
}

// TestExampleConfigParity is the functional check the whole exercise is aimed at.  Each
// plugin's shipped example.conf is parsed the way the runner parses it, the resulting
// native config is mapped to a dynamic Config, encoded back out to an INI, and read again
// with gcfg.  The config that comes out the far side has to be the one that went in.
func TestExampleConfigParity(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.kind, func(t *testing.T) {
			var h exampleHarness
			if err := gcfg.ReadFileInto(&h, filepath.Clean(tc.example)); err != nil {
				t.Fatalf(`reading %s: %v`, tc.example, err)
			}
			name, cfg, ok := tc.get(h.Configs)
			if !ok {
				t.Fatalf(`%s has no %s section`, tc.example, tc.kind)
			}

			c, err := dynamic.MapRunnerDefinition(tc.kind, name, cfg)
			if err != nil {
				t.Fatalf(`MapConfig: %v`, err)
			}
			ini, err := c.INI()
			if err != nil {
				t.Fatalf(`INI: %v`, err)
			}
			t.Logf("%s %q regenerated as:\n%s", tc.kind, name, ini)

			target := tc.fresh()
			if err = gcfg.ReadStringInto(target, ini); err != nil {
				t.Fatalf("gcfg rejected the generated INI: %v\n%s", err, ini)
			}
			got, ok := tc.extract(target, name)
			if !ok {
				t.Fatalf(`subsection %q missing from the regenerated INI`, name)
			}
			want := zeroSecrets(cfg, tc.secrets)
			if have := reflect.ValueOf(got).Elem().Interface(); !reflect.DeepEqual(have, want) {
				t.Errorf("example.conf did not survive the round trip\n got %+v\nwant %+v", have, want)
			}
		})
	}
}

// TestExampleConfigVerifies goes one step further than shape: the regenerated config has
// to still pass the plugin's own Verify, which is what actually decides whether an
// ingester will run with it.  Secrets are supplied the way a GUI would supply them.
func TestExampleConfigVerifies(t *testing.T) {
	type verifier interface{ Verify() error }
	for _, tc := range cases() {
		t.Run(tc.kind, func(t *testing.T) {
			var h exampleHarness
			if err := gcfg.ReadFileInto(&h, filepath.Clean(tc.example)); err != nil {
				t.Fatalf(`reading %s: %v`, tc.example, err)
			}
			name, cfg, ok := tc.get(h.Configs)
			if !ok {
				t.Fatalf(`no %s section`, tc.kind)
			}
			c, err := dynamic.MapRunnerDefinition(tc.kind, name, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// a GUI hands the secrets back in the variables, put them back the same way
			for i, v := range c.Variables {
				for _, s := range tc.secrets {
					if v.Name == strings.ReplaceAll(s, `_`, `-`) {
						c.Variables[i].Value = `supplied-by-the-user`
					}
				}
			}
			ini, err := c.INI()
			if err != nil {
				t.Fatal(err)
			}
			target := tc.fresh()
			if err = gcfg.ReadStringInto(target, ini); err != nil {
				t.Fatalf("gcfg rejected %v\n%s", err, ini)
			}
			got, _ := tc.extract(target, name)
			v, ok := got.(verifier)
			if !ok {
				t.Skipf(`%T has no Verify`, got)
			}
			if err = v.Verify(); err != nil {
				t.Errorf("the regenerated config no longer verifies: %v\n%s", err, ini)
			}
		})
	}
}

// TestPluginConfigsFlatten checks that every real plugin config maps to a flat variable
// list with no duplicate names, which is what the embedded base configs require.
func TestPluginConfigsFlatten(t *testing.T) {
	for _, tc := range []struct {
		kind string
		cfg  any
		// members that must be present, proving the embedded base was flattened in
		promoted []string
	}{
		{`Okta`, okta.Config{}, nil},
		{`Mimecast`, mimecast.Config{}, []string{`Tag-Name`, `Tag-Prefix`, `Lookback`,
			`Requests-Per-Minute`, `Request-Interval`}},
		{`MSGraph`, msgraph.Config{}, nil},
		{`Jamf`, jamf.Config{}, []string{`Tag-Name`, `Lookback`, `Requests-Per-Minute`,
			`Request-Interval`}},
		{`Wiz`, wiz.Config{}, []string{`Lookback`, `Requests-Per-Minute`, `Request-Interval`}},
		{`SQS`, sqs.Config{}, []string{`Tag-Name`}},
		{`Tester`, tester.Config{}, []string{`Tag-Name`}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c, err := dynamic.MapRunnerDefinition(tc.kind, `x`, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, v := range c.Variables {
				if seen[v.Name] {
					t.Errorf(`%s appears twice, gcfg can only address one of them`, v.Name)
				}
				seen[v.Name] = true
				if v.Type.Complex() {
					t.Errorf(`%s came out as %s, an embedded base must be flattened`, v.Name, v.Type)
				}
			}
			for _, n := range tc.promoted {
				if !seen[n] {
					t.Errorf(`%s was not promoted out of its embedded base`, n)
				}
			}
			// Ingester-UUID always comes from hosted.BaseConfig and is lifted out
			if seen[`Ingester-UUID`] {
				t.Error(`Ingester-UUID should be lifted into Config.UUID, not left as a variable`)
			}
		})
	}
}

// TestBaseConfigsFlatten pins the behaviour for the shared bases themselves.
func TestBaseConfigsFlatten(t *testing.T) {
	type withAll struct {
		hosted.BaseConfig
		hosted.MultiTagConfig
		hosted.PollingConfig
		Host string
	}
	c, err := dynamic.MapRunnerDefinition(`x`, `y`, withAll{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range c.Variables {
		names = append(names, v.Name)
	}
	want := []string{`Tag-Name`, `Tag-Prefix`, `Lookback`, `Requests-Per-Minute`,
		`Request-Interval`, `Host`}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("variables:\n got %q\nwant %q", names, want)
	}
}
