/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
)

// TestSetAccess covers gravwell/issues#2708: Client.SetAccess replaces the
// old Readers-only SetGroups/SetGlobal with a single call that can also set
// Writers and, for admins, reassign ownership. It verifies the actual wire
// request the method produces -- method, path, and JSON body shape -- against
// a mock server (same pattern as TestPing above), since that request shape
// is the real contract the webserver, and every other consumer of this SDK,
// depends on.
func TestSetAccess(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	type accessBody types.SearchCtrlSetAccessRequest

	var gotMethod, gotPath string
	var gotBody accessBody
	var gotRawBody []byte
	var failReq bool

	mux := http.NewServeMux()
	mux.HandleFunc("/api/searchctrl/", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody = accessBody{}
		var err error
		if gotRawBody, err = io.ReadAll(r.Body); err != nil {
			t.Errorf("failed to read request body: %v", err)
		} else if err := json.Unmarshal(gotRawBody, &gotBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if failReq {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	srv := http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })

	c, err := client.NewOpts(client.Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("reassigns owner and sets Readers/Writers", func(t *testing.T) {
		nid := int32(7)
		readers := types.ACL{GIDs: []int32{1, 2}}
		writers := types.ACL{Global: true}

		if err := c.SetAccess("search-123", nid, readers, writers); err != nil {
			t.Fatalf("SetAccess: %v", err)
		}

		if gotMethod != http.MethodPut {
			t.Errorf("method = %q, want %q", gotMethod, http.MethodPut)
		}
		if want := "/api/searchctrl/search-123/access"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
		if gotBody.OwnerID != nid {
			t.Errorf("OwnerID = %v, want %d", gotBody.OwnerID, nid)
		}
		if !equalACL(gotBody.Readers, readers) {
			t.Errorf("Readers = %+v, want %+v", gotBody.Readers, readers)
		}
		if !equalACL(gotBody.Writers, writers) {
			t.Errorf("Writers = %+v, want %+v", gotBody.Writers, writers)
		}
	})

	t.Run("ownerID of 0 rides the wire as a literal 0, not omitted or null", func(t *testing.T) {
		// SetAccess/SearchCtrlSetAccessRequest have no "leave unchanged"
		// sentinel for OwnerID anymore (it's a plain, always-present int32,
		// not the old *int32) -- validating that 0 is invalid is the
		// webserver's job (see TestSetSearchAccess/TestSearchCtrlSetAccessHandler
		// in the backend repo), not this SDK's. This only proves the client
		// doesn't do anything surprising with a zero value on the way out --
		// e.g. dropping the field via an accidental `omitempty` tag on
		// SearchCtrlSetAccessRequest.OwnerID, which would let a real bug there
		// slip past unnoticed.
		if err := c.SetAccess("search-789", 0, types.ACL{}, types.ACL{}); err != nil {
			t.Fatalf("SetAccess with ownerID=0: %v", err)
		}
		if gotBody.OwnerID != 0 {
			t.Errorf("OwnerID = %d, want 0", gotBody.OwnerID)
		}
		if !strings.Contains(string(gotRawBody), `"OwnerID":0`) {
			t.Errorf("expected the actual wire body to contain a literal OwnerID:0, got %s", gotRawBody)
		}
	})

	t.Run("non-200 response surfaces as an error", func(t *testing.T) {
		failReq = true
		t.Cleanup(func() { failReq = false })
		if err := c.SetAccess("search-123", 1, types.ACL{}, types.ACL{}); err == nil {
			t.Fatal("expected error on 500 response, got nil")
		}
	})
}

func equalACL(a, b types.ACL) bool {
	if a.Global != b.Global || len(a.GIDs) != len(b.GIDs) {
		return false
	}
	for i := range a.GIDs {
		if a.GIDs[i] != b.GIDs[i] {
			return false
		}
	}
	return true
}
