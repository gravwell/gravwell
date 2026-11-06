package plugins

import (
	"strings"
	"testing"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
)

// TestPluginSecretsAreTyped checks that every plugin member the team marked as not for
// shipping is now typed as a secret, and that none of them carry a value into a
// definition.
func TestPluginSecretsAreTyped(t *testing.T) {
	// what each plugin is expected to treat as a secret
	want := map[string][]string{
		`Okta`:     {`Token`},
		`Jamf`:     {`Client-Secret`},
		`Mimecast`: {`Client-Id`, `Client-Secret`},
		`MSGraph`:  {`Client-Secret`},
		`SQS`:      {`Secret`},
		`Wiz`:      {`Client-Id`, `Client-Secret`},
		`Tester`:   nil,
	}

	kinds, err := Kinds()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, pk := range kinds {
		seen[pk.Kind] = true
		rd, err := dynamic.MapRunnerDefinition(pk.Kind, pk.Kind, pk.Config)
		if err != nil {
			t.Fatalf("%s: %v", pk.Kind, err)
		}
		got := map[string]bool{}
		for _, v := range rd.Variables {
			if string(v.Type) == `secret` {
				got[v.Name] = true
				if v.Value != nil {
					t.Errorf("%s: secret %s carries a value %v", pk.Kind, v.Name, v.Value)
				}
			}
		}
		expected, ok := want[pk.Kind]
		if !ok {
			t.Errorf("%s has no expectation recorded, add one", pk.Kind)
			continue
		}
		for _, name := range expected {
			if !got[name] {
				t.Errorf("%s: %s should be a secret", pk.Kind, name)
			}
			delete(got, name)
		}
		for name := range got {
			t.Errorf("%s: %s is a secret but was not expected to be", pk.Kind, name)
		}
	}
	for kind := range want {
		if !seen[kind] {
			t.Errorf("%s is expected but is not a registered kind", kind)
		}
	}
}

// TestPluginRequiredMatchesVerify pins which members are marked required.  The list is
// derived from each plugin's own Verify: only the members it refuses to run without.
// Marking a member that Verify gives a default to would block a configuration the plugin
// would happily accept, which is the failure mode worth guarding against.
func TestPluginRequiredMatchesVerify(t *testing.T) {
	want := map[string][]string{
		`Okta`:     {`Domain`, `Token`},
		`Jamf`:     {`Host`, `Client-Id`, `Client-Secret`},
		`Mimecast`: {`Client-Id`, `Client-Secret`},
		`MSGraph`:  {`Tenant-ID`, `Client-ID`, `Client-Secret`, `Content-Type`},
		`SQS`:      {`Queue-URL`, `Region`},
		`Wiz`:      {`Client-Id`, `Client-Secret`, `Endpoint`, `Tag-Name`},
		`Tester`:   nil, // everything it takes has a default
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
		got := map[string]bool{}
		for _, v := range rd.Variables {
			if v.Required {
				got[v.Name] = true
			}
		}
		expected, ok := want[pk.Kind]
		if !ok {
			t.Errorf("%s has no expectation recorded, add one", pk.Kind)
			continue
		}
		for _, name := range expected {
			if !got[name] {
				t.Errorf("%s: %s should be required", pk.Kind, name)
			}
			delete(got, name)
		}
		for name := range got {
			t.Errorf("%s: %s is marked required but its Verify supplies a default", pk.Kind, name)
		}
	}
}

// TestPluginSecretsSurviveTheConfig checks the other half: a secret an operator supplies
// has to reach the ingester's config file, otherwise the plugin cannot authenticate.
func TestPluginSecretsSurviveTheConfig(t *testing.T) {
	rd, err := dynamic.MapRunnerDefinition(`Okta`, `prod`, Kinds1Okta())
	if err != nil {
		t.Fatal(err)
	}
	rd.UUID = uuid.New()
	// the operator fills the secret in through the interface
	for i := range rd.Variables {
		if rd.Variables[i].Name == `Token` {
			rd.Variables[i].Value = `operator-supplied-token`
		}
		if rd.Variables[i].Name == `Domain` {
			rd.Variables[i].Value = `example.okta.com`
		}
	}
	ini, err := rd.INI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ini, `operator-supplied-token`) {
		t.Errorf("the secret did not reach the config:\n%s", ini)
	}
	if !strings.Contains(ini, `example.okta.com`) {
		t.Errorf("the config is incomplete:\n%s", ini)
	}
}

// Kinds1Okta returns the okta prototype for the test above.
func Kinds1Okta() any {
	ks, _ := Kinds()
	for _, pk := range ks {
		if pk.Kind == `Okta` {
			return pk.Config
		}
	}
	return nil
}
