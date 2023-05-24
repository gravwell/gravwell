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

var globalDefaultAllow = CBACRules{
	Capabilities: testGetAllCaps(),
	Tags: TagAccess{
		Grants: []string{`*`},
	},
}

func TestEmpty(t *testing.T) {
	var ta TagAccess
	if ok := ta.check(`foo`); ok {
		t.Fatal("Empty disallowed a tag")
	}
}

func TestBasic(t *testing.T) {
	ta := TagAccess{
		Grants: []string{`foo`, `bar`, `baz`},
	}
	//check allow
	if ok := ta.check(`baz`); !ok {
		t.Fatal("Invalid deny")
	}

	//check  miss
	if ok := ta.check(`foobar`); ok {
		t.Fatal("Invalid allow")
	}
}

func TestGlobs(t *testing.T) {
	ta := TagAccess{
		Grants: []string{`foo`, `bar`, `foo*`},
	}
	//check miss
	if ok := ta.check(`baz`); ok {
		t.Fatal("Invalid allow")
	}

	//check allow
	if ok := ta.check(`foobar`); !ok {
		t.Fatal("Invalid miss")
	}
}

func TestIntersection(t *testing.T) {
	prime := TagAccess{
		Grants: []string{`foo`, `bar`, `baz`},
	}

	set := []TagAccess{
		TagAccess{
			Grants: []string{`foo`, `foobar`},
		},
		TagAccess{
			Grants: []string{`foobar`, `barbaz`, `fizz*buzz`},
		},
	}
	//check foo - denied by prime and set[0]
	if !CheckTagAccess(`foo`, prime, set) {
		t.Fatal(`invalid allowance for "foo"`)
	}
	//check foobar - allowed by prime and set[1] but denied by set[0]
	if !CheckTagAccess(`foobar`, prime, set) {
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

	//check things that would miss the glob
	if CheckTagAccess(`fizzbuzzer`, prime, set) {
		t.Fatal(`invalid allowance for "fizzbuzzer"`)
	}
	//check things that would hit the glob
	vals := []string{
		`fizzbuzz`,
		`fizz-buzz`,
		`fizz-to-the-buzz`,
	}
	for _, v := range vals {
		if !CheckTagAccess(v, prime, set) {
			t.Fatalf(`invalid allowance for "%s"`, v)
		}
	}
}

func TestValidate(t *testing.T) {
	//check that empty is ok
	var ta TagAccess
	if err := ta.Validate(); err != nil {
		t.Fatal(err)
	} else if ok := ta.check(`foo`); ok {
		t.Fatal("empty after validate disallowed a tag", ok)
	}

	//add a few tags, some of them twice
	ta.Grants = []string{
		`foo`,
		`bar`,
		`baz`,
		`foo`,
		`foobar`,
	}
	if err := ta.Validate(); err != nil {
		t.Fatal(err)
	} else if ok := ta.check(`foo`); !ok {
		t.Fatal("failed to allow ok tag")
	} else if ok = ta.check(`foobar`); !ok {
		t.Fatal("failed to allow ok tag")
	} else if ok = ta.check(`foobaz`); ok {
		t.Fatal("failed to disallow tag")
	} else if len(ta.Grants) != 4 {
		t.Fatal("Did not remove duplicate tag")
	}

	//check with an invalid tag
	ta.Grants = []string{
		`i love bad tags`,
	}
	if err := ta.Validate(); err == nil {
		t.Fatalf("Failed to catch bad tag")
	}

	//check with a bad glob pattern
	ta.Grants = []string{
		`foo[a-f`, //missing training range bracket
	}
	if err := ta.Validate(); err == nil {
		t.Fatalf("Failed to catch glob")
	}
}

func TestFilterWhitelist(t *testing.T) {
	ta := TagAccess{
		Grants: []string{`foo`, `bar`, `baz`},
	}
	set := []TagAccess{
		TagAccess{
			Grants: []string{`foo`, `fooit`},
		},
		TagAccess{
			Grants: []string{`ba*`, `barbaz`},
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
	cs := CapabilitySet{}
	//check a few when in default deny
	if cs.Has(Search) {
		t.Fatal("Search set on clean")
	} else if cs.Has(SOAREmail) {
		t.Fatal("SOAREmail set on clean")
	} else if cs.Has(DashboardRead) {
		t.Fatal("DashboardRead on clean")
	}

	//set a few grants and recheck
	cs.Set(Search)
	cs.Set(SOAREmail)
	cs.Set(DashboardRead)
	if !cs.Has(Search) {
		t.Fatal("Search not set on default allow")
	} else if !cs.Has(SOAREmail) {
		t.Fatal("SOAREmail not set on default allow")
	} else if !cs.Has(DashboardRead) {
		t.Fatal("DashboardRead not set on default allow")
	}

	//set a few grants (they should NOT be set)
	if !cs.Set(DashboardWrite) {
		t.Fatal("Failed to set search grant")
	} else if !cs.Has(DashboardWrite) {
		t.Fatal("capability is not denied after grant")
	}
	//remove grant
	if !cs.Clear(Search) {
		t.Fatal("failed to clear grant")
	} else if cs.Has(Search) {
		t.Fatal("capability is not allowed afer clearing grant")
	}
}

func TestOverlap(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	//check that the user cannot do... anything
	for _, c := range fullCapList {
		if ud.HasCapability(c) {
			t.Fatal("user does not have capability with all allow")
		}
	}

	//swap the last group to deny and recheck
	ud.Groups[1].CBAC.Capabilities = testGetAllCaps()
	for _, c := range fullCapList {
		if !ud.HasCapability(c) {
			t.Fatal("user has capability with group deny")
		}
	}
}

func TestOverlapWithUserExplicit(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	//allow search
	ud.Groups[1].CBAC.Capabilities.Set(Search)
	if !ud.HasCapability(Search) {
		t.Fatal("User has Search capability after group set")
	}

	//check that the SOAREmail grant isn't there, as a sanity check
	if ud.HasCapability(SOAREmail) {
		t.Fatal("User has SOAREmail after setting search")
	}

	//remove Search grant
	ud.Groups[1].CBAC.Capabilities.Clear(Search)
	if ud.HasCapability(Search) {
		t.Fatal("User has Search capability after clear")
	}

	//setup an explicit allow and explicit deny, ensure it is denied
	ud.Groups[0].CBAC.Capabilities.Set(Search)
	if !ud.HasCapability(Search) {
		t.Fatal("User has Search capability after conflicting explicit group clear")
	}
}

func TestCapabilityList(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: testGetAllCaps()}},
		},
	}
	if lst := ud.CapabilityList(); len(lst) != len(fullCapList) {
		t.Fatalf("wide open user does not have all capabilities: %d != %d", len(lst), _maxCap)
	}

	ud.Groups[1].CBAC.Capabilities = CapabilitySet{}
	//allow a few explicite in the group
	ud.Groups[1].CBAC.Capabilities.Set(Download)
	if lst := ud.CapabilityList(); len(lst) != 1 {
		t.Fatalf("shut out user has capabilities: %d != 1", len(lst))
	} else if lst[0].Name != Download.Name() {
		t.Fatalf("invalid allowed list: %v", lst)
	}
}

func TestGlobalDeny(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	//check that the user cannot do... anything
	for _, c := range fullCapList {
		if ud.HasCapability(c) {
			t.Fatal("user has capability with global deny")
		}
	}
}

func TestGlobalDenyExplicitAllowUser(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	ud.CBAC.Capabilities.Set(Search)

	//check that the user can search but not do anything else
	for _, c := range fullCapList {
		if c.CapabilityDesc().Cap == Search {
			if !ud.HasCapability(c) {
				t.Fatal("user does not have capability with global deny and explicit allow")
			}
		} else if ud.HasCapability(c) {
			t.Fatal("user has capability with global deny", c.String())
		}
	}
}

func TestGlobalDenyExplicitAllowGroup(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	ud.Groups[0].CBAC.Capabilities.Set(Search)

	//check that the user can search but not do anything else
	for _, c := range fullCapList {
		if c.CapabilityDesc().Cap == Search {
			if !ud.HasCapability(c) {
				t.Fatal("user does not have capability with global deny and explicit allow")
			}
		} else if ud.HasCapability(c) {
			t.Fatal("user has capability with global deny", c.String())
		}
	}
}

func TestGlobalTagDeny(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	if ud.HasTagAccess("foo") {
		t.Fatal("user has capability with global deny")
	}
}

func TestGlobalTagDenyExplicitAllowUser(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}, Tags: TagAccess{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	ud.CBAC.Tags.Grants = []string{"foo"}

	if !ud.HasTagAccess("foo") {
		t.Fatal("user does not have tag access after grant")
	}
}

func TestGlobalTagDenyExplicitAllowGroup(t *testing.T) {
	//setup user with default allow and two groups, all allow
	ud := UserDetails{
		CBAC: CBACRules{Capabilities: CapabilitySet{}},
		Groups: []GroupDetails{
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}, Tags: TagAccess{}}},
			GroupDetails{CBAC: CBACRules{Capabilities: CapabilitySet{}}},
		},
	}

	ud.Groups[0].CBAC.Tags.Grants = []string{"foo"}

	if !ud.HasTagAccess("foo") {
		t.Fatal("user does not have capability with global deny and explicit grant")
	}
}

func testGetAllCaps() (cs CapabilitySet) {
	for _, c := range fullCapList {
		cs.Set(c)
	}
	return
}
