/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package version

import (
	"testing"
)

func TestParse(t *testing.T) {
	good := []string{
		`1.2.3`,
		`0.1.2`,
		`200.500.99`,
		`0.0.1`,
		`99000033.11111111.999999999`,
	}
	bad := []string{
		``,
		`hello`,
		`1`,
		`1.2`,
		`1.2.3 `,
		`1.foobar.2`,
		`-1.2.3`,
		`1.-2.3`,
		`abcd.1.2`,
		`1.2.3.4`,
		`v1.2.3`,
	}
	for _, g := range good {
		if v, err := Parse(g); err != nil {
			t.Fatalf("parse error on %s %v", g, err)
		} else if v.String() != g {
			t.Fatalf("version did not come back out: %v != %v", v.String(), g)
		}
	}
	for _, b := range bad {
		if _, err := Parse(b); err == nil {
			t.Fatalf("failed to catch bad version %q", b)
		}
	}

}
