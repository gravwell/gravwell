/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"
)

type offsetTest struct {
	val string
	idx int
	sz  int
}

func TestRfcOffset(t *testing.T) {
	tsts := []offsetTest{
		offsetTest{val: "\n<12>1 foo to the bar", idx: 0, sz: 5},
		offsetTest{val: "\n<1234>1 foo to the bar", idx: -1},
		offsetTest{val: "\n<>1 foo to the bar", idx: -1},
		offsetTest{val: "\n<1 foo to the bar", idx: -1},
		offsetTest{val: "\n<", idx: -1},
		offsetTest{val: "\n<1", idx: -1},
		offsetTest{val: "\n<12", idx: -1},
		offsetTest{val: "\n<123", idx: -1},
		offsetTest{val: "<123>1 foo to the bar", idx: -1},
		offsetTest{val: "<123>1\n<1> foo to the bar", idx: 6, sz: 4},
		offsetTest{val: "<123>1\n<12> foo to the bar", idx: 6, sz: 5},
		offsetTest{val: "<123>1\n<123> foo to the bar", idx: 6, sz: 6},
		offsetTest{val: "<123>1\n<1234> foo to the bar", idx: -1},
		offsetTest{val: "\n\n<123>", idx: 1, sz: 6},
		offsetTest{val: "\n\n<12>", idx: 1, sz: 5},
		offsetTest{val: "\n\n<1>", idx: 1, sz: 4},
		offsetTest{val: "\n\n<1\n<234>", idx: 4, sz: 6},
	}

	for _, tst := range tsts {
		if offset, sz := rfc5424StartIndex([]byte(tst.val)); offset != tst.idx {
			t.Fatalf("bad offset: %d != %d - %q", offset, tst.idx, tst.val)
		} else if sz != tst.sz {
			t.Fatalf("bad size: %d != %d - %q", sz, tst.sz, tst.val)
		}
	}
}

var testVal = []byte(`` + "\n<123>")
