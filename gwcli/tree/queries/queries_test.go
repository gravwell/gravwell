/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package queries

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
)

// TestSetGroupPreservesWriters is a regression test for gravwell/issues#2708:
// setGroup() migrated from the Readers-only SetGroups endpoint to the
// combined SetAccess endpoint, which replaces Writers and OwnerID wholesale
// on every call (OwnerID has no "leave unchanged" sentinel -- it's a plain,
// always-required int32, same as Writers). Without fetching the search's
// current Writers and OwnerID first, running set-groups would silently wipe
// any existing write access, or get rejected outright for sending an
// unowned/zero OwnerID. This drives the actual cobra command
// non-interactively against a mock server and asserts the wire request it
// sends preserves Writers and OwnerID while only updating Readers.
func TestSetGroupPreservesWriters(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	existingWriters := types.ACL{GIDs: []int32{99}}
	const existingOwnerID = int32(42)

	type accessBody struct {
		OwnerID int32
		Readers types.ACL
		Writers types.ACL
	}
	var gotAccess accessBody
	var sawAccessCall bool

	mux := http.NewServeMux()
	mux.HandleFunc("/api/searchctrl/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			si := types.SearchInfo{}
			si.OwnerID = existingOwnerID
			si.Writers = existingWriters
			if err := json.NewEncoder(w).Encode(si); err != nil {
				t.Errorf("failed to encode mock GetSearch response: %v", err)
			}
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/access"):
			sawAccessCall = true
			if err := json.NewDecoder(r.Body).Decode(&gotAccess); err != nil {
				t.Errorf("failed to decode access request body: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })

	c, err := client.NewOpts(client.Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	// GetSearch requires the client to believe it's authenticated (checked
	// client-side before any request is sent); the token itself is never
	// validated against the mock server.
	if err := c.ImportLoginToken("test-token"); err != nil {
		t.Fatalf("ImportLoginToken: %v", err)
	}
	connection.Client = c
	t.Cleanup(func() { connection.Client = nil })

	var stderr bytes.Buffer
	pair := setGroup()
	pair.Action.SetOut(io.Discard)
	pair.Action.SetErr(&stderr)
	pair.Action.SetArgs([]string{"--groups=1,2", "search-123"})
	if err := pair.Action.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, stderr.String())
	}

	if !sawAccessCall {
		t.Fatal("expected a PUT .../access request, got none")
	}
	if gotAccess.OwnerID != existingOwnerID {
		t.Errorf("OwnerID = %d, want unchanged %d", gotAccess.OwnerID, existingOwnerID)
	}
	if !slices.Equal(gotAccess.Writers.GIDs, existingWriters.GIDs) {
		t.Errorf("Writers.GIDs = %v, want unchanged %v", gotAccess.Writers.GIDs, existingWriters.GIDs)
	}
	if want := []int32{1, 2}; !slices.Equal(gotAccess.Readers.GIDs, want) {
		t.Errorf("Readers.GIDs = %v, want %v", gotAccess.Readers.GIDs, want)
	}
}
