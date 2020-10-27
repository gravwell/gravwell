/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import "testing"

type rateVals struct {
	Input string
	Bps   int64
}

func TestParseRate(t *testing.T) {
	tests := []rateVals{
		{"1", 1},
		{"1024", 1024},

		{"1Kbit", 1024},
		{"1Kbps", 1024},
		{"1kbit", 1024},
		{"1kbps", 1024},
		{"1KBps", 8192},

		{"1mbit", 1024 * 1024},
		{"1mbps", 1024 * 1024},
		{"1Mbit", 1024 * 1024},
		{"1Mbps", 1024 * 1024},
		{"1MBps", 8 * 1024 * 1024},

		{"1gbit", 1024 * 1024 * 1024},
		{"1gbps", 1024 * 1024 * 1024},
		{"1Gbit", 1024 * 1024 * 1024},
		{"1Gbps", 1024 * 1024 * 1024},
		{"1GBps", 8 * 1024 * 1024 * 1024},
	}

	for i := range tests {
		if rate, err := ParseRate(tests[i].Input); err != nil {
			t.Fatalf("Failed to parse %v: %v", tests[i].Input, err)
		} else if rate != tests[i].Bps {
			t.Fatalf("%v incorrectly parsed to %v, expected %v", tests[i].Input, rate, tests[i].Bps)
		}
	}
}
