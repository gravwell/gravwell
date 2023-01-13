/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"testing"
)

func TestVersionCompare(t *testing.T) {
	o := BuildInfo{
		CanonicalVersion: CanonicalVersion{
			Major: 1,
			Minor: 0,
			Point: 5,
		},
	}
	n := o
	if o.NewerVersion(n) {
		t.Fatal(n.String() + " not newer than " + o.String())
	}
	n.Point++
	if !o.NewerVersion(n) {
		t.Fatal(n.String() + " is newer than " + o.String())
	}
	n.Point = 1
	n.Minor = 100
	if !o.NewerVersion(n) {
		t.Fatal(n.String() + " is newer than " + o.String())
	}
	n = o
	n.Major = 0
	n.Minor = 0
	n.Point = 1000
	if o.NewerVersion(n) {
		t.Fatal(n.String() + " not newer than " + o.String())
	}
}

func TestVersionStuff(t *testing.T) {
	if v, err := ParseCanonicalVersion(``); err != nil {
		t.Fatal(err)
	} else if v.Enabled() {
		t.Fatal("this shouldn't be enabled")
	} else if v.String() != `0.0.0` {
		t.Fatal("Bad string " + v.String())
	}

	if v, err := ParseCanonicalVersion(`0.0.0`); err != nil {
		t.Fatal(err)
	} else if v.Enabled() {
		t.Fatal("this shouldn't be enabled")
	} else if v.String() != `0.0.0` {
		t.Fatal("Bad string " + v.String())
	}

	if v, err := ParseCanonicalVersion(`1.2.3`); err != nil {
		t.Fatal(err)
	} else if !v.Enabled() {
		t.Fatal("this should be enabled")
	} else if v.String() != `1.2.3` {
		t.Fatal("Bad string " + v.String())
	} else if v.Compare(CanonicalVersion{1, 2, 3}) != 0 {
		t.Fatal("Bad Compare", v)
	} else if v.Compare(CanonicalVersion{1, 2, 2}) != -1 {
		t.Fatal("Bad Compare", v)
	} else if v.Compare(CanonicalVersion{1, 2, 4}) != 1 {
		t.Fatal("Bad Compare", v)
	} else if v.Compare(CanonicalVersion{0, 1, 2}) != -1 {
		t.Fatal("Bad Compare", v)
	} else if v.Compare(CanonicalVersion{1, 1, 2}) != -1 {
		t.Fatal("Bad Compare", v)
	} else if v.Compare(CanonicalVersion{0, 0, 55}) != -1 {
		t.Fatal("Bad Compare", v)
	}

	badVals := []string{
		`bad val`,
		`a.b.c`,
		`1.2`,
		`1`,
		`1.2.3.4`,
	}
	for _, bv := range badVals {
		if _, err := ParseCanonicalVersion(bv); err == nil {
			t.Fatalf("Failed to catch bad value: %v", bv)
		}
	}
}
