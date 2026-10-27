/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// malformed datagrams that have panicked the gosnmp trap parser
var malformedTraps = [][]byte{
	{0x30, 0x03, 0x02, 0x01, 0x03}, // SEQUENCE{INTEGER 3}, a v3 header with nothing after the version
	{0x30, 0x03, 0x02, 0x01, 0x01},
	{0x30, 0x05, 0x02, 0x01, 0x03, 0x30, 0x00},
}

func TestUnmarshalMalformedTrap(t *testing.T) {
	versions := []gosnmp.SnmpVersion{gosnmp.Version1, gosnmp.Version2c, gosnmp.Version3}
	for _, ver := range versions {
		params := &gosnmp.GoSNMP{
			Transport: "udp",
			Version:   ver,
			Timeout:   time.Second,
			MaxOids:   gosnmp.MaxOids,
			Community: "public",
		}
		if ver == gosnmp.Version3 {
			params.SecurityModel = gosnmp.UserSecurityModel
			params.SecurityParameters = &gosnmp.UsmSecurityParameters{UserName: "user"}
		}
		for _, pkt := range malformedTraps {
			if _, err := params.UnmarshalTrap(pkt, false); err == nil {
				t.Fatalf("version %v accepted malformed trap %x", ver, pkt)
			}
		}
	}
}
