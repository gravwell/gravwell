/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/gravwell/gravwell/v4/client"
)

func TestServerIP(t *testing.T) {
	// Make a client with an IP address
	c, err := client.New("1.2.3.4", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "1.2.3.4" {
		t.Fatalf("Invalid IP address, expected 1.2.3.4 got %v", c.ServerIP())
	}
	// And try with a port
	c, err = client.New("1.2.3.4:8080", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "1.2.3.4" {
		t.Fatalf("Invalid IP address, expected 1.2.3.4 got %v", c.ServerIP())
	}
	// And v6
	c, err = client.New("[::2]:8080", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerIP().String() != "::2" {
		t.Fatalf("Invalid IP address, expected ::2 got %v", c.ServerIP())
	}

	// Make a client with a known-valid hostname
	c, err = client.New("localhost", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsLoopback() {
		t.Fatalf("Invalid IP address, expected loopback got %v", c.ServerIP())
	}
	c, err = client.New("localhost:80", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ServerIP().IsLoopback() {
		t.Fatalf("Invalid IP address, expected loopback got %v", c.ServerIP())
	}

	// Make a client with a hostname that is guaranteed to fail
	if c, err := client.New("my.host.INVALIDDOMAIN.INVALIDTLD", false, false); err != nil {
		t.Fatal(err)
	} else if !c.ServerIP().IsUnspecified() {
		t.Fatalf("Invalid IP address, expected unspecified got %v", c.ServerIP())
	}
	if c, err := client.New("my.host.INVALIDDOMAIN.INVALIDTLD:443", false, false); err != nil {
		t.Fatal(err)
	} else if !c.ServerIP().IsUnspecified() {
		t.Fatalf("Invalid IP address, expected unspecified got %v", c.ServerIP())
	}
}

// check that the client can issue and receive pings (positive and negative)
func TestMockPing(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	srv := http.Server{}
	var failReq bool
	http.HandleFunc(client.TEST_URL, func(w http.ResponseWriter, r *http.Request) {
		if failReq {
			w.WriteHeader(500)
		}
	})
	go srv.Serve(l)
	defer srv.Shutdown(context.Background())
	// test we can make a successful ping against a mock
	c, err := client.NewOpts(client.Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Test(); err != nil {
		t.Fatal("bad status returned from endpoint, expected 200: ", err)
	}
	failReq = true // check that we gracefuly handle
	if err := c.Test(); !errors.Is(err, client.ErrInvalidTestStatus) {
		t.Fatal("expected ErrInvalidTestStatus error; got ", err)
	}
}
