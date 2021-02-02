/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"testing"
)

func TestEmpty(t *testing.T) {
	var ta TagAccess
	if _, ok := ta.Check(`foo`); !ok {
		t.Fatal("Empty disallowed a tag")
	}
}

func TestBasicWhitelist(t *testing.T) {
	ta := TagAccess{
		Default: TagDefaultDeny,
		Tags:    []string{`foo`, `bar`, `baz`},
	}
	//check allow
	if exp, ok := ta.Check(`baz`); !ok {
		t.Fatal("Invalid deny")
	} else if !exp {
		t.Fatal("Invalid explicit")
	}

	//check  miss
	if _, ok := ta.Check(`foobar`); ok {
		t.Fatal("Invalid allow")
	}
}

func TestBasicBlacklist(t *testing.T) {
	ta := TagAccess{
		Default: TagDefaultAllow,
		Tags:    []string{`foo`, `bar`, `baz`},
	}
	//check allow
	if exp, ok := ta.Check(`foobar`); !ok {
		t.Fatal("Invalid deny")
	} else if exp {
		t.Fatal("invalid explicit")
	}

	//check  miss
	if exp, ok := ta.Check(`foo`); ok || !exp {
		t.Fatal("Missed hard deny")
	}
}

func TestBasicIntersection(t *testing.T) {
	prime := TagAccess{
		Default: TagDefaultAllow,
		Tags:    []string{`foo`, `bar`, `baz`},
	}

	set := []TagAccess{
		TagAccess{
			Default: TagDefaultAllow,
			Tags:    []string{`foo`, `foobar`},
		},
		TagAccess{
			Default: TagDefaultDeny,
			Tags:    []string{`foobar`, `barbaz`},
		},
	}
	//check foo - denied by prime and set[0]
	if CheckTagAccess(`foo`, prime, set) {
		t.Fatal(`invalid allowance for "foo"`)
	}
	//check foobar - allowed by prime and set[1] but denied by set[0]
	if CheckTagAccess(`foobar`, prime, set) {
		t.Fatal(`invalid allowance for "foobar"`)
	}
	//check barbaz - allowed by all
	if !CheckTagAccess(`barbaz`, prime, set) {
		t.Fatal(`invalid denial for "barbaz"`)
	}
	//check ChuckTesta - not disallowed by anyone, but not explicitely allowed by set[1]
	if CheckTagAccess(`ChuckTesta`, prime, set) {
		t.Fatal(`invalid allowance for "ChuckTesta"`)
	}
}

func TestValidate(t *testing.T) {
	//check that empty is ok
	var ta TagAccess
	if err := ta.Validate(); err != nil {
		t.Fatal(err)
	} else if exp, ok := ta.Check(`foo`); !ok || exp {
		t.Fatal("empty after validate disallowed a tag", ok, exp)
	}

	//add a few tags, some of them twice
	ta.Tags = []string{
		`foo`,
		`bar`,
		`baz`,
		`foo`,
		`foobar`,
	}
	ta.Default = TagDefaultDeny
	if err := ta.Validate(); err != nil {
		t.Fatal(err)
	} else if exp, ok := ta.Check(`foo`); !ok || !exp {
		t.Fatal("failed to allow ok tag")
	} else if exp, ok = ta.Check(`foobar`); !ok || !exp {
		t.Fatal("failed to allow ok tag")
	} else if exp, ok = ta.Check(`foobaz`); ok || exp {
		t.Fatal("failed to disallow tag")
	} else if len(ta.Tags) != 4 {
		t.Fatal("Did not remove duplicate tag")
	}
}

func TestFilterWhitelist(t *testing.T) {
	ta := TagAccess{
		Default: TagDefaultDeny,
		Tags:    []string{`foo`, `bar`, `baz`},
	}
	set := []TagAccess{
		TagAccess{
			Default: TagDefaultAllow,
			Tags:    []string{`foo`, `foobar`},
		},
		TagAccess{
			Default: TagDefaultDeny,
			Tags:    []string{`foobar`, `barbaz`},
		},
	}

	candidates := [][]string{
		[]string{`foo`, `foobar`, `foobarbaz`, `baz`},
		[]string{`foobar`, `foobarbaz`, `baz`},
		[]string{`foobarbaz`, `barbaz`},
		[]string{},
		nil,
	}
	outputs := [][]string{
		[]string{`foo`, `baz`},
		[]string{`baz`},
		[]string{`barbaz`},
		nil,
		nil,
		nil,
	}
	for i, c := range candidates {
		if ns := FilterTags(c, ta, set); !checkVals(outputs[i], ns) {
			t.Fatalf("Failed to filter tags: %v -> %v != %v", c, ns, outputs[i])
		}
	}
}

func TestConflict(t *testing.T) {
	a := TagAccess{
		Default: TagDefaultAllow,
		Tags:    []string{`foo`, `foobar`},
	}
	b := TagAccess{
		Default: TagDefaultDeny,
		Tags:    []string{`foobar`, `barbaz`},
	}
	if conflict, tag := CheckTagConflict(a, b); !conflict {
		t.Fatal("failed to detect tag conflict")
	} else if tag != `foobar` {
		t.Fatalf("Failed to detect conflicting tag: %s != %s", tag, `foobar`)
	}

	b.Default = TagDefaultAllow
	if conflict, _ := CheckTagConflict(a, b); conflict {
		t.Fatal("invalid conflict detected")
	}
}

func checkVals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
