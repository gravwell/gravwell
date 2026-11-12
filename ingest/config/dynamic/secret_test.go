/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"strings"
	"testing"
)

type secretCfg struct {
	Ingester_UUID string
	Domain        string
	Token         string `dynamic:"secret"`
	Legacy        string `json:"-"`
	Both          string `json:"-" dynamic:"secret"`
	Spaced        string `dynamic:" secret "`
	Listed        string `dynamic:"something,secret"`
	Plain         string
}

func varOf(t *testing.T, rd RunnerDefinition, name string) Variable {
	t.Helper()
	for _, v := range rd.Variables {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("%s is not in the definition", name)
	return Variable{}
}

// TestSecretTag covers the dynamic:"secret" annotation on a reflected registration.
func TestSecretTag(t *testing.T) {
	rd, err := MapRunnerDefinition(`k`, `n`, secretCfg{
		Domain: `example.com`,
		Token:  `a-real-token`,
		Legacy: `also-sensitive`,
		Both:   `belt-and-braces`,
		Spaced: `spaced-out`,
		Listed: `in-a-list`,
		Plain:  `not-a-secret`,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{`Token`, `Both`, `Spaced`, `Listed`} {
		v := varOf(t, rd, name)
		if v.Type != typeSecret {
			t.Errorf("%s is typed %s, want %s", name, v.Type, typeSecret)
		}
		// the whole point: a secret is described but its value never travels
		if v.Value != nil {
			t.Errorf("%s shipped its value %v", name, v.Value)
		}
	}

	// json:"-" keeps working the way it did, the value is withheld but the type is not
	// changed, a withheld int is not a string
	if v := varOf(t, rd, `Legacy`); v.Type != typeString {
		t.Errorf("Legacy is typed %s, want %s", v.Type, typeString)
	} else if v.Value != nil {
		t.Errorf("Legacy shipped its value %v", v.Value)
	}

	// everything else is untouched
	if v := varOf(t, rd, `Plain`); v.Type != typeString || v.Value != `not-a-secret` {
		t.Errorf("Plain = %s/%v", v.Type, v.Value)
	}
	if v := varOf(t, rd, `Domain`); v.Value != `example.com` {
		t.Errorf("Domain = %v", v.Value)
	}

	// and no secret value leaks into the encoded form at all
	blob, err := rd.INI()
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{`a-real-token`, `also-sensitive`, `belt-and-braces`, `spaced-out`, `in-a-list`} {
		if strings.Contains(blob, leaked) {
			t.Errorf("a secret value reached the INI:\n%s", blob)
		}
	}
}

// TestSecretMustBeAString checks the guard, a secret is a string and nothing else.
func TestSecretMustBeAString(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
	}{
		{`int`, struct {
			Bad int `dynamic:"secret"`
		}{}},
		{`bool`, struct {
			Bad bool `dynamic:"secret"`
		}{}},
		{`slice`, struct {
			Bad []string `dynamic:"secret"`
		}{}},
	} {
		if _, err := MapRunnerDefinition(`k`, `n`, tc.v); err == nil {
			t.Errorf("a %s tagged as a secret should be refused", tc.name)
		}
	}
}

// TestSecretIsNotComplex is the bug this nearly shipped with.  INI() shunts every complex
// variable onto a list it never processes, so a secret that reported itself as complex
// would be silently dropped from the config file and the ingester would start without it.
func TestSecretIsNotComplex(t *testing.T) {
	if typeSecret.Complex() {
		t.Fatal(`a secret is a string, it must not be complex, INI would drop it`)
	}
	if err := typeSecret.Valid(); err != nil {
		t.Errorf(`a secret should be a valid type: %v`, err)
	}

	// prove it end to end: a populated secret has to survive into the INI
	rd := RunnerDefinition{
		Kind: `k`, Name: `n`,
		Variables: []Variable{
			{Name: `Token`, Type: typeSecret, Value: `s3cr3t`},
			{Name: `Domain`, Type: typeString, Value: `example.com`},
		},
	}
	blob, err := rd.INI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blob, "Token=`s3cr3t`") {
		t.Errorf("the secret was dropped from the config:\n%s", blob)
	}
	if !strings.Contains(blob, "Domain=`example.com`") {
		t.Errorf("the config is missing Domain:\n%s", blob)
	}
}

// TestSecretValidates covers Variable.Validate for the new type.
func TestSecretValidates(t *testing.T) {
	if err := (Variable{Name: `T`, Type: typeSecret, Value: `a string`}).Validate(); err != nil {
		t.Errorf("a string secret should validate: %v", err)
	}
	// a described but unset secret is the normal registration shape
	if err := (Variable{Name: `T`, Type: typeSecret}).Validate(); err != nil {
		t.Errorf("a secret with no value should validate: %v", err)
	}
	for _, bad := range []any{1, true, 1.5, []string{`a`}} {
		if err := (Variable{Name: `T`, Type: typeSecret, Value: bad}).Validate(); err == nil {
			t.Errorf("a %T should not validate as a secret", bad)
		}
	}
	// and a secret round trips through the INI quoting the same way a string does
	v := Variable{Name: `T`, Type: typeSecret, Value: "a`b\x01c"}
	var sb strings.Builder
	if err := v.emitIniLine(&sb, ``); err == nil {
		t.Error(`an unrepresentable secret should fail the same way a string does`)
	}
}

type requiredCfg struct {
	Domain   string   `dynamic:"required"`
	Token    string   `json:"-" dynamic:"secret,required"`
	Tags     []string `dynamic:"required"`
	Count    int      `dynamic:"required"`
	Optional string
	Spaced   string `dynamic:"required , secret"`
}

// TestRequiredTag covers the dynamic:"required" annotation.
func TestRequiredTag(t *testing.T) {
	rd, err := MapRunnerDefinition(`k`, `n`, requiredCfg{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`Domain`, `Token`, `Tags`, `Count`, `Spaced`} {
		if v := varOf(t, rd, name); !v.Required {
			t.Errorf("%s should be required", name)
		}
	}
	if v := varOf(t, rd, `Optional`); v.Required {
		t.Error(`Optional should not be required`)
	}

	// the options combine, order and spacing do not matter
	if v := varOf(t, rd, `Token`); v.Type != typeSecret || !v.Required {
		t.Errorf("Token = %s required=%v, want a required secret", v.Type, v.Required)
	}
	if v := varOf(t, rd, `Spaced`); v.Type != typeSecret || !v.Required {
		t.Errorf("Spaced = %s required=%v, want a required secret", v.Type, v.Required)
	}

	// required is a description, not a validation gate.  A prototype describes an empty
	// config, and a required variable with no value still has to validate or a
	// registration could never be sent.
	for _, v := range rd.Variables {
		if err := v.Validate(); err != nil {
			t.Errorf("%s does not validate as a prototype: %v", v.Name, err)
		}
	}

	// required works on any type, not just strings, unlike secret
	if v := varOf(t, rd, `Count`); v.Type != typeInt || !v.Required {
		t.Errorf("Count = %s required=%v", v.Type, v.Required)
	}
	if v := varOf(t, rd, `Tags`); v.Type != typeSliceString || !v.Required {
		t.Errorf("Tags = %s required=%v", v.Type, v.Required)
	}
}
