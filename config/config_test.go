/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"net"
	"testing"
)

func TestParseSourceIP(t *testing.T) {
	tsts := []string{
		`192.168.1.1`,
		`10.0.0.1`,
		`172.17.0.2`,
		`1.1.1.1`,
		`dead::beef`,
		`::1`,
	}
	for _, v := range tsts {
		if b, err := ParseSource(v); err != nil {
			t.Fatal(err)
		} else if !b.Equal(net.ParseIP(v)) {
			t.Fatal("bad source result")
		}
	}
}

type ipTest struct {
	v    string
	ipeq string
}

func TestParseSourceIntegter(t *testing.T) {
	tsts := []ipTest{
		ipTest{`1`, `::1`},
		ipTest{`1000`, `::03e8`},
		ipTest{`1000000`, `::000f:4240`},
		ipTest{`0xbeef`, `::beef`},
		ipTest{`0xfeedbeef`, `::feed:beef`},
		ipTest{`feedfebedeadbeef12345678`, `::feed:febe:dead:beef:1234:5678`},
	}
	for _, v := range tsts {
		if b, err := ParseSource(v.v); err != nil {
			t.Fatal(err)
		} else if !b.Equal(net.ParseIP(v.ipeq)) {
			t.Fatal("bad source result", v.v, v.ipeq, b)
		}
	}
}
