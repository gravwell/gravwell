/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v3/client/objlog"
	"testing"
)

func TestServerIP(t *testing.T) {
	// Make a client with an IP address
	c, err := NewClient("1.2.3.4", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "1.2.3.4" {
		t.Fatalf("Invalid IP address, expected 1.2.3.4 got %v", c.ServerIP())
	}
	// And try with a port
	c, err = NewClient("1.2.3.4:8080", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "1.2.3.4" {
		t.Fatalf("Invalid IP address, expected 1.2.3.4 got %v", c.ServerIP())
	}
	// And v6
	c, err = NewClient("[::2]:8080", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "::2" {
		t.Fatalf("Invalid IP address, expected ::2 got %v", c.ServerIP())
	}

	// Make a client with a known-valid hostname
	c, err = NewClient("localhost", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsLoopback() {
		t.Fatalf("Invalid IP address, expected loopback got %v", c.ServerIP())
	}
	c, err = NewClient("localhost:80", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsLoopback() {
		t.Fatalf("Invalid IP address, expected loopback got %v", c.ServerIP())
	}

	// Make a client with a hostname that is guaranteed to fail
	c, err = NewClient("my.host.INVALIDDOMAIN.INVALIDTLD", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsUnspecified() {
		t.Fatalf("Invalid IP address, expected unspecified got %v", c.ServerIP())
	}
	c, err = NewClient("my.host.INVALIDDOMAIN.INVALIDTLD:443", false, false, &objlog.NilObjLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsUnspecified() {
		t.Fatalf("Invalid IP address, expected unspecified got %v", c.ServerIP())
	}
}
