/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package plugins

import (
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/hosted/plugins/jamf"
	"github.com/gravwell/gravwell/v4/hosted/plugins/mimecast"
	"github.com/gravwell/gravwell/v4/hosted/plugins/msgraph"
	"github.com/gravwell/gravwell/v4/hosted/plugins/sqs"
	"github.com/gravwell/gravwell/v4/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v4/hosted/plugins/wiz"
	"github.com/gravwell/gravwell/v4/ingest"
)

// badTags are the shapes a tag gets wrong in the field.  Every one of them is something
// the indexer refuses at negotiation, so an ingester configured with one comes up and
// then does not ingest, which is the failure this validation exists to turn into an error
// somebody can see.
var badTags = []struct{ name, tag string }{
	{`space`, `bad tag`},
	{`bang`, `bad!tag`},
	{`dot`, `bad.tag`},
	{`colon`, `bad:tag`},
	{`slash`, `bad/tag`},
	{`star`, `bad*tag`},
	{`dollar`, `bad$tag`},
	{`bracket`, `bad[tag]`},
	{`quote`, `bad"tag`},
	{`backtick`, "bad`tag"},
	{`backslash`, `bad\tag`},
	{`equals`, `bad=tag`},
	{`percent`, `bad%tag`},
	{`tab`, "bad\ttag"},
	{`control`, "bad\x01tag"},
	// ingest.MAX_TAG_LENGTH is the limit and the check is strictly greater, so this is
	// the first length that is actually too long
	{`oversized`, strings.Repeat(`a`, ingest.MAX_TAG_LENGTH+1)},
}

// verifier is what every plugin config is, and what the dynamic validation path calls.
type verifier interface{ Verify() error }

// configs under test, each already valid apart from the tag the case puts in it.  The
// function shape is deliberate: each case needs a fresh config because Verify mutates.
type tagCase struct {
	kind string
	// withTag builds an otherwise valid config carrying tag
	withTag func(tag string) verifier
}

func tagCases() []tagCase {
	const uuidStr = `4f1c35f6-6af6-4103-8fdc-df2c63026f0d`
	return []tagCase{
		{`Tester`, func(tag string) verifier {
			return &tester.Config{
				BaseConfig:      hosted.BaseConfig{Ingester_UUID: uuidStr},
				SingleTagConfig: hosted.SingleTagConfig{Tag_Name: tag},
			}
		}},
		{`Jamf`, func(tag string) verifier {
			return &jamf.Config{
				BaseConfig:     hosted.BaseConfig{Ingester_UUID: uuidStr},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: tag},
				Host:           `https://example.jamfcloud.com`,
				Client_Id:      `id`,
				Client_Secret:  `secret`,
			}
		}},
		{`SQS`, func(tag string) verifier {
			return &sqs.Config{
				BaseConfig:       hosted.BaseConfig{Ingester_UUID: uuidStr},
				SingleTagConfig:  hosted.SingleTagConfig{Tag_Name: tag},
				Queue_URL:        `https://sqs.us-east-1.amazonaws.com/1/q`,
				Region:           `us-east-1`,
				Credentials_Type: `static`,
				AKID:             `akid`,
				Secret:           `secret`,
			}
		}},
		{`Mimecast`, func(tag string) verifier {
			return &mimecast.Config{
				BaseConfig:     hosted.BaseConfig{Ingester_UUID: uuidStr},
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: tag},
				Client_Id:      `id`,
				Client_Secret:  `secret`,
				Api:            []mimecast.Api{mimecast.AuditApi},
			}
		}},
		{`MSGraph`, func(tag string) verifier {
			return &msgraph.Config{
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: uuidStr},
				Tenant_ID:     `tenant`,
				Client_ID:     `id`,
				Client_Secret: `secret`,
				Content_Type:  []msgraph.ContentType{msgraph.ContentAlerts},
				Tag_Name:      tag,
			}
		}},
		{`Wiz`, func(tag string) verifier {
			return &wiz.Config{
				BaseConfig:    hosted.BaseConfig{Ingester_UUID: uuidStr},
				Client_Id:     `id`,
				Client_Secret: `secret`,
				Endpoint:      `https://api.us1.app.wiz.io/graphql`,
				Tag_Name:      tag,
			}
		}},
	}
}

// TestPluginsRejectBadTags is the gap this closes.  Before it, only wiz checked its tag,
// so a configuration naming a tag the indexer will refuse was accepted by every other
// plugin and only failed later, at negotiation, where nothing ties it back to the
// configuration that caused it.
func TestPluginsRejectBadTags(t *testing.T) {
	for _, tc := range tagCases() {
		t.Run(tc.kind, func(t *testing.T) {
			// the control: a good tag is still accepted, a check that rejects
			// everything is no use to anyone
			if err := tc.withTag(`good-tag`).Verify(); err != nil {
				t.Fatalf("a valid tag was rejected: %v", err)
			}
			for _, bt := range badTags {
				t.Run(bt.name, func(t *testing.T) {
					err := tc.withTag(bt.tag).Verify()
					if err == nil {
						t.Fatalf("accepted tag %q, which the indexer will refuse", bt.tag)
					}
					// the message has to say it is about a tag, an operator reads this
					// in a status list with no other context.  Case insensitive: the
					// plugins spell it Tag-Name, ingest spells it "Tag name".
					if !strings.Contains(strings.ToLower(err.Error()), `tag`) {
						t.Errorf("error does not mention the tag: %v", err)
					}
				})
			}
		})
	}
}

// TestTagPrefixIsCheckedAsAResolvedTag covers the case checking the raw fields would
// miss: a prefix is not a tag, it is half of one, and it is the join that goes wrong.
func TestTagPrefixIsCheckedAsAResolvedTag(t *testing.T) {
	const uuidStr = `4f1c35f6-6af6-4103-8fdc-df2c63026f0d`
	c := &mimecast.Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: uuidStr},
		MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: `bad prefix`},
		Client_Id:      `id`,
		Client_Secret:  `secret`,
		Api:            []mimecast.Api{mimecast.AuditApi},
	}
	if err := c.Verify(); err == nil {
		t.Error(`a Tag-Prefix that resolves to an illegal tag was accepted`)
	}
	// and a good prefix still resolves to something legal
	c = &mimecast.Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: uuidStr},
		MultiTagConfig: hosted.MultiTagConfig{Tag_Prefix: `mc`},
		Client_Id:      `id`,
		Client_Secret:  `secret`,
		Api:            []mimecast.Api{mimecast.AuditApi},
	}
	if err := c.Verify(); err != nil {
		t.Errorf("a valid Tag-Prefix was rejected: %v", err)
	}
	for _, tag := range c.Tags() {
		if err := ingest.CheckTag(tag); err != nil {
			t.Errorf("resolved tag %q is not valid: %v", tag, err)
		}
	}
}

// TestEveryTagCarryingPluginIsCovered keeps this honest as plugins are added: any config
// with a tag field has to implement TagProvider, or VerifyTags never sees it.
func TestEveryTagCarryingPluginIsCovered(t *testing.T) {
	kinds, err := Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range kinds {
		t.Run(pk.Kind, func(t *testing.T) {
			rt := reflect.TypeOf(pk.Config)
			if !hasTagField(rt, 0) {
				t.Skip(`no tag field, nothing to validate`)
			}
			if _, ok := reflect.New(rt).Interface().(hosted.TagProvider); !ok {
				t.Errorf(`config has a tag field but does not implement hosted.TagProvider, so its tags are never checked`)
			}
		})
	}
}

// hasTagField reports whether a config carries a member whose name mentions a tag,
// walking embedded structs the way gcfg does.
func hasTagField(rt reflect.Type, depth int) bool {
	if depth > 4 || rt == nil || rt.Kind() != reflect.Struct {
		return false
	}
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if hasTagField(f.Type, depth+1) {
				return true
			}
			continue
		}
		if f.IsExported() && strings.Contains(strings.ToLower(f.Name), `tag`) {
			return true
		}
	}
	return false
}

// TestEnumsMatchTheCodeTheyDescribe keeps a declared value set from drifting from the set
// the plugin actually accepts.  A picker that offers a value Verify rejects is worse than
// a text box: it looks like an endorsement.
func TestEnumsMatchTheCodeTheyDescribe(t *testing.T) {
	want := map[string]map[string][]string{
		`Mimecast`: {`Api`: mimecastApis()},
		`MSGraph`:  {`Content-Type`: {`alerts`, `secureScores`, `controlProfiles`}},
		`SQS`:      {`Credentials-Type`: {`static`, `environment`, `ec2role`}},
	}
	kinds, err := Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range kinds {
		rd, err := dynamic.MapRunnerDefinition(pk.Kind, pk.Kind, pk.Config)
		if err != nil {
			t.Fatalf("%s: %v", pk.Kind, err)
		}
		expect := want[pk.Kind]
		for _, v := range rd.Variables {
			exp, declared := expect[v.Name]
			switch {
			case declared && len(v.Enum) == 0:
				t.Errorf("%s %s: lost its enum", pk.Kind, v.Name)
			case !declared && len(v.Enum) > 0:
				t.Errorf("%s %s: unexpected enum %v, this test needs updating", pk.Kind, v.Name, v.Enum)
			case declared:
				// compared as sets: the order in the tag is what the picker shows, and
				// that is a presentation choice.  What has to hold is that nothing is
				// offered which the plugin rejects, and nothing it accepts is missing.
				missing, extra := diffSets(exp, v.Enum)
				if len(missing) > 0 {
					t.Errorf("%s %s: the plugin accepts %v but the picker does not offer them",
						pk.Kind, v.Name, missing)
				}
				if len(extra) > 0 {
					t.Errorf("%s %s: the picker offers %v which the plugin rejects",
						pk.Kind, v.Name, extra)
				}
			}
		}
	}
}

// mimecastApis is the set the plugin actually supports, read off the plugin rather than
// copied, so adding an API to the code and forgetting the tag fails here.
func mimecastApis() []string {
	out := []string{string(mimecast.AuditApi)}
	for api := range mimecast.SIEMApiEvents {
		out = append(out, string(api))
	}
	return out
}

// diffSets reports what is in want but not got, and in got but not want.
func diffSets(want, got []string) (missing, extra []string) {
	have := make(map[string]bool, len(got))
	for _, g := range got {
		have[g] = true
	}
	wanted := make(map[string]bool, len(want))
	for _, w := range want {
		wanted[w] = true
		if !have[w] {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !wanted[g] {
			extra = append(extra, g)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	return
}

// TestSQSCredentialsAreConditionallyRequired is the bug this closes.
//
// AKID and Secret are needed for static credentials and meaningless for the other two
// modes.  Marked always required they would block role based auth; marked optional, which
// is what they were, an operator can save a static configuration with no key in it and
// only find out when the ingester refuses it.
func TestSQSCredentialsAreConditionallyRequired(t *testing.T) {
	rd, err := dynamic.MapRunnerDefinition(`SQS`, `probe`, sqs.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`AKID`, `Secret`} {
		v, ok := findVarByName(rd, name)
		if !ok {
			t.Fatalf("no %s variable", name)
		}
		if v.Required {
			t.Errorf("%s is marked always required, which blocks environment and ec2role credentials", name)
		}
		if v.RequiredWhen == nil {
			t.Fatalf("%s carries no condition, so a static config with no credentials still saves", name)
		}
		if v.RequiredWhen.Field != `Credentials-Type` {
			t.Errorf("%s depends on %q", name, v.RequiredWhen.Field)
		}
	}

	set := func(mode any) dynamic.RunnerDefinition {
		out := rd
		out.Variables = append([]dynamic.Variable(nil), rd.Variables...)
		for i := range out.Variables {
			if out.Variables[i].Name == `Credentials-Type` {
				out.Variables[i].Value = mode
			}
		}
		return out
	}
	akid, _ := findVarByName(rd, `AKID`)
	for _, tc := range []struct {
		mode any
		want bool
	}{
		{`static`, true},
		{nil, true}, // unset means static
		{`environment`, false},
		{`ec2role`, false},
	} {
		if got := set(tc.mode).RequiredNow(akid); got != tc.want {
			t.Errorf("Credentials-Type=%v: AKID required = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

func findVarByName(c dynamic.RunnerDefinition, name string) (dynamic.Variable, bool) {
	for _, v := range c.Variables {
		if v.Name == name {
			return v, true
		}
	}
	return dynamic.Variable{}, false
}
