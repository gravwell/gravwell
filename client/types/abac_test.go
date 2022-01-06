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
		Default: DefaultDeny,
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
		Default: DefaultAllow,
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
		Default: DefaultAllow,
		Tags:    []string{`foo`, `bar`, `baz`},
	}

	set := []TagAccess{
		TagAccess{
			Default: DefaultAllow,
			Tags:    []string{`foo`, `foobar`},
		},
		TagAccess{
			Default: DefaultDeny,
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
	ta.Default = DefaultDeny
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
		Default: DefaultDeny,
		Tags:    []string{`foo`, `bar`, `baz`},
	}
	set := []TagAccess{
		TagAccess{
			Default: DefaultAllow,
			Tags:    []string{`foo`, `foobar`},
		},
		TagAccess{
			Default: DefaultDeny,
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
		Default: DefaultAllow,
		Tags:    []string{`foo`, `foobar`},
	}
	b := TagAccess{
		Default: DefaultDeny,
		Tags:    []string{`foobar`, `barbaz`},
	}
	if conflict, tag := CheckTagConflict(a, b); !conflict {
		t.Fatal("failed to detect tag conflict")
	} else if tag != `foobar` {
		t.Fatalf("Failed to detect conflicting tag: %s != %s", tag, `foobar`)
	}

	b.Default = DefaultAllow
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

func TestAddRemove(t *testing.T) {
	cs := CapabilitySet{
		Default: DefaultDeny,
	}
	//check a few when in default deny
	if cs.Has(Search) {
		t.Fatal("Search set on clean")
	} else if cs.Has(SOAREmail) {
		t.Fatal("SOAREmail set on clean")
	} else if cs.Has(DashboardRead) {
		t.Fatal("DashboardRead on clean")
	}

	//change to default allow and recheck
	cs.Default = DefaultAllow
	if !cs.Has(Search) {
		t.Fatal("Search not set on default allow")
	} else if !cs.Has(SOAREmail) {
		t.Fatal("SOAREmail not set on default allow")
	} else if !cs.Has(DashboardRead) {
		t.Fatal("DashboardRead not set on default allow")
	}

	//set a few overrides (they should NOT be set)
	if !cs.SetOverride(Search) {
		t.Fatal("Failed to set search override")
	} else if cs.Has(Search) {
		t.Fatal("capability is not denied after override")
	}
	//switch default
	cs.Default = DefaultDeny
	if !cs.Has(Search) {
		t.Fatal("capability is not allowed after override")
	}

	//remove override
	if !cs.ClearOverride(Search) {
		t.Fatal("failed to clear override")
	} else if cs.Has(Search) {
		t.Fatal("capability is not allowed afer clearing override")
	}
}

func TestOverlap(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}},
		Groups: []GroupDetails{
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
		},
	}

	//check that the user cannot do... anything
	for _, c := range fullCapList {
		if !ud.HasCapability(c) {
			t.Fatal("user does not have capability with all allow")
		}
	}

	//swap the last group to deny and recheck
	ud.Groups[1].ABAC.Capabilities.Default = DefaultDeny
	for _, c := range fullCapList {
		if ud.HasCapability(c) {
			t.Fatal("user has capability with group deny")
		}
	}
}

func TestOverlapWithUserExplicit(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}},
		Groups: []GroupDetails{
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
		},
	}

	//setup a group to explicitely override a denial for Search
	//swap the last group to deny and recheck
	ud.Groups[1].ABAC.Capabilities.SetOverride(Search)
	if ud.HasCapability(Search) {
		t.Fatal("User has Search capability after group explicit denial")
	}

	//now swap the user assigned default and explicitely allow
	ud.ABAC.Capabilities.Default = DefaultDeny
	if ud.HasCapability(Search) {
		t.Fatal("User has Search capability after default denial")
	}
	ud.ABAC.Capabilities.SetOverride(Search)
	if !ud.HasCapability(Search) {
		t.Fatal("User does NOT have Search capability after explicit allow")
	}

	//now setup a group with an explicit allow and make sure the default deny still overrides
	ud.ABAC.Capabilities.ClearOverride(Search)
	ud.Groups[1].ABAC.Capabilities.Default = DefaultDeny
	if ud.HasCapability(Search) {
		t.Fatal("User has Search capability after explicit group allow but default deny")
	}
}

func TestCapabilityList(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}},
		Groups: []GroupDetails{
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
			GroupDetails{ABAC: ABACRules{Capabilities: CapabilitySet{Default: DefaultAllow}}},
		},
	}
	if lst := ud.CapabilityList(); len(lst) != int(_maxCap) {
		t.Fatalf("wide open user does not have all capabilities: %d != %d", len(lst), _maxCap)
	}

	//set default to deny and check again
	ud.ABAC.Capabilities.Default = DefaultDeny
	if lst := ud.CapabilityList(); len(lst) != 0 {
		t.Fatalf("shut out user has capabilities: %d != 0", len(lst))
	}

	//do it again via group
	ud.ABAC.Capabilities.Default = DefaultAllow
	ud.Groups[1].ABAC.Capabilities.Default = DefaultDeny
	if lst := ud.CapabilityList(); len(lst) != 0 {
		t.Fatalf("shut out user has capabilities: %d != 0", len(lst))
	}

	//allow a few explicite in the group
	ud.Groups[1].ABAC.Capabilities.SetOverride(Search)
	ud.Groups[1].ABAC.Capabilities.SetOverride(GetTags)
	ud.ABAC.Capabilities.SetOverride(Search)
	if lst := ud.CapabilityList(); len(lst) != 1 {
		t.Fatalf("shut out user has capabilities: %d != 1", len(lst))
	} else if lst[0].Name != GetTags.Name() {
		t.Fatalf("invalid allowed list: %v", lst)
	}
}
