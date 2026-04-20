/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/client/objlog"
	"github.com/gravwell/gravwell/v3/client/types"
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

// check that the client can issue and receive pings (positive and negative)
func TestPing(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	mux := http.NewServeMux()
	var failReq atomic.Bool
	mux.HandleFunc(TEST_URL, func(w http.ResponseWriter, r *http.Request) {
		if failReq.Load() {
			w.WriteHeader(500)
		}
	})
	srv := http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })
	// test we can make a successful ping against a mock
	c, err := NewOpts(Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Test(); err != nil {
		t.Fatal("bad status returned from endpoint, expected 200: ", err)
	}
	failReq.Store(true) // check that we gracefully handle
	if err := c.Test(); !errors.Is(err, ErrInvalidTestStatus) {
		t.Fatal("expected ErrInvalidTestStatus error; got ", err)
	}
}

// Ensure major version mismatches are caught prior to login attempts (and that minor mismatches are allowed).
func TestAPIVersionCheck(t *testing.T) {
	// spool up a mock endpoint to fire tests against
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	mux := http.NewServeMux()
	var ( // each tests sets major and minor
		mockMajor atomic.Uint32
		mockMinor atomic.Uint32
	)

	mux.HandleFunc(API_VERSION_URL, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vi := types.VersionInfo{
			API: types.ApiInfo{Major: mockMajor.Load(), Minor: mockMinor.Load()},
		}

		if err := json.NewEncoder(w).Encode(vi); err != nil {
			w.WriteHeader(500)
		}
	})
	srv := http.Server{Handler: mux}

	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })

	tests := []struct {
		name             string
		major            uint32
		minor            uint32
		wantVersionError bool
	}{
		{"match major | match minor", types.API_VERSION_MAJOR, types.API_VERSION_MINOR, false},
		{"match major | mismatch minor", types.API_VERSION_MAJOR, types.API_VERSION_MINOR + 1, false},
		{"mismatch major | match minor", types.API_VERSION_MAJOR + 1, types.API_VERSION_MINOR, true},
		{"mismatch major | mismatch minor", types.API_VERSION_MAJOR + 1, types.API_VERSION_MINOR + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			c, err := NewOpts(Opts{Server: l.Addr().String()})
			if err != nil {
				t.Fatal(err)
			}

			// use a short timeout window, as we don't actually care about anything past the version check.
			c.SetRequestTimeout(100 * time.Millisecond)
			defer c.Close()

			// update mock server's version
			mockMajor.Store(tt.major)
			mockMinor.Store(tt.minor)

			// test bare API check
			if vErr, err := c.CheckApiVersion(); err != nil {
				t.Fatal(err)
			} else if (vErr != "") != tt.wantVersionError {
				t.Fatalf("unexpected error state. Wanted version error? %v | Actual error: %v", tt.wantVersionError, vErr)
			}

			// test u/p login
			if err := c.Login("someusername", "somepassword"); isVersionError(err) != tt.wantVersionError {
				t.Fatalf("unexpected error state. Wanted version error? %v | Actual error: %v", tt.wantVersionError, err)
			}

			// test mfa login
			if _, err := c.MFALogin("someusername", "somepassword", types.AUTH_TYPE_TOTP, "1111"); isVersionError(err) != tt.wantVersionError {
				t.Fatalf("unexpected error state. Wanted version error? %v | Actual error: %v", tt.wantVersionError, err)
			}

			// test API key login
			if err := c.LoginWithAPIToken("myfancyapitoken"); isVersionError(err) != tt.wantVersionError {
				t.Fatalf("unexpected error state. Wanted version error? %v | Actual error: %v", tt.wantVersionError, err)
			}

			// test JWT login
			if err := c.ImportLoginToken("alogintokenImadeup"); err != nil {
				t.Fatal(err)
			} else if err := c.TestLogin(); isVersionError(err) != tt.wantVersionError {
				t.Fatalf("unexpected error state. Wanted version error? %v | Actual error: %v", tt.wantVersionError, err)
			}
		})
	}
}

// hold over check for v3
func isVersionError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "version mismatch")
}
