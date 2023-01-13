/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"net"
	"testing"
)

type tSet struct {
	c string
	v net.IP
}

func TestParseGoodSource(t *testing.T) {
	good := []tSet{
		tSet{c: `1`, v: net.ParseIP(`::1`)},
		tSet{c: `0x1`, v: net.ParseIP(`::1`)},
		tSet{c: `0x99FE`, v: net.ParseIP(`::99fe`)},
		tSet{c: `0xdeadbeeffeedf00d`, v: net.ParseIP(`::dead:beef:feed:f00d`)},
		tSet{c: `FEADBEEF`, v: net.ParseIP(`254.173.190.239`)},
		tSet{c: `192.168.0.1`, v: net.ParseIP(`192.168.0.1`)},
		tSet{c: `FEEDDEADBEEF`, v: net.ParseIP(`::FEED:DEAD:BEEF`)},
	}
	for _, v := range good {
		if r, err := ParseSource(v.c); err != nil {
			t.Fatal(err)
		} else {
			if !r.Equal(v.v) {
				t.Fatal("Bad source override", r, v.v)
			}
		}
	}
}

func TestParseBadSource(t *testing.T) {
	bad := []string{
		``,
		`this should break`,
		`AABBCCDDEEFF00112233445566778899FFEEDDCCBBAA`,
		`AABBCCDDEEFF0011223344556677889`,
	}
	for _, v := range bad {
		if r, err := ParseSource(v); err == nil {
			t.Fatalf("Parse didn't fail on bad %s.  Got %v", v, r)
		}
	}
}
