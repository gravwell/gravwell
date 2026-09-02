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
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/spf13/cobra"
)

// accessBody mirrors the wire shape of a PUT .../access request.
type accessBody struct {
	OwnerID int32
	Readers types.ACL
	Writers types.ACL
}

// newAccessMockServer spins up a mock searchctrl server that returns existing
// as the search's current state on GET and captures the body of the PUT
// .../access call into the returned *accessBody. It wires connection.Client
// to point at the mock server and registers cleanup with t.
//
// *sawAccessCall is set true iff a PUT .../access request was received.
func newAccessMockServer(t *testing.T, existing types.SearchInfo) (got *accessBody, sawAccessCall *bool) {
	t.Helper()

	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	got = &accessBody{}
	sawAccessCall = new(bool)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/searchctrl/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			if err := json.NewEncoder(w).Encode(existing); err != nil {
				t.Errorf("failed to encode mock GetSearch response: %v", err)
			}
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/access"):
			*sawAccessCall = true
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
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

	return got, sawAccessCall
}

// execute runs cmd non-interactively with args and fails the test if it errors.
func execute(t *testing.T, cmd *cobra.Command, args []string) {
	t.Helper()
	var stderr bytes.Buffer
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v (stderr: %s)", err, stderr.String())
	}
}

// TestSetAccessPreservesUnspecifiedFields is a regression test for
// gravwell/issues#2680: setAccess() (formerly setGroup()) must only mutate
// the fields explicitly named by flags. The SetAccess endpoint clobbers
// OwnerID/Readers/Writers wholesale on every call, and setGroup() used to
// build a fresh types.ACL{GIDs: GIDs} for Readers -- which silently zeroed
// Readers.Global (and, per gravwell/issues#2708, would have zeroed
// OwnerID/Writers too without the earlier fix). This drives the actual
// cobra command non-interactively against a mock server and asserts the
// wire request preserves everything except the one field actually targeted.
func TestSetAccessPreservesUnspecifiedFields(t *testing.T) {
	existing := types.SearchInfo{}
	existing.OwnerID = 42
	existing.Readers = types.ACL{GIDs: []int32{7}, Global: true}
	existing.Writers = types.ACL{GIDs: []int32{99}, Global: true}

	got, sawAccessCall := newAccessMockServer(t, existing)

	execute(t, setAccess().Action, []string{"--reader-groups=1,2", "search-123"})

	if !*sawAccessCall {
		t.Fatal("expected a PUT .../access request, got none")
	}
	if got.OwnerID != existing.OwnerID {
		t.Errorf("OwnerID = %d, want unchanged %d", got.OwnerID, existing.OwnerID)
	}
	if want := []int32{1, 2}; !slices.Equal(got.Readers.GIDs, want) {
		t.Errorf("Readers.GIDs = %v, want %v", got.Readers.GIDs, want)
	}
	if got.Readers.Global != existing.Readers.Global {
		t.Errorf("Readers.Global = %v, want unchanged %v", got.Readers.Global, existing.Readers.Global)
	}
	if !slices.Equal(got.Writers.GIDs, existing.Writers.GIDs) {
		t.Errorf("Writers.GIDs = %v, want unchanged %v", got.Writers.GIDs, existing.Writers.GIDs)
	}
	if got.Writers.Global != existing.Writers.Global {
		t.Errorf("Writers.Global = %v, want unchanged %v", got.Writers.Global, existing.Writers.Global)
	}
}

// TestSetAccessFlagsIndependently checks that each of set-access's four
// flags mutates only its own field, leaving the other three (fetched from
// the search's current state) untouched.
func TestSetAccessFlagsIndependently(t *testing.T) {
	base := func() types.SearchInfo {
		si := types.SearchInfo{}
		si.OwnerID = 10
		si.Readers = types.ACL{GIDs: []int32{1}, Global: false}
		si.Writers = types.ACL{GIDs: []int32{2}, Global: false}
		return si
	}

	tests := []struct {
		name string
		arg  string
		want accessBody
	}{
		{
			name: "writer-groups",
			arg:  "--writer-groups=5,6",
			want: accessBody{OwnerID: 10, Readers: types.ACL{GIDs: []int32{1}}, Writers: types.ACL{GIDs: []int32{5, 6}}},
		},
		{
			name: "reader-global",
			arg:  "--reader-global",
			want: accessBody{OwnerID: 10, Readers: types.ACL{GIDs: []int32{1}, Global: true}, Writers: types.ACL{GIDs: []int32{2}}},
		},
		{
			name: "writer-global",
			arg:  "--writer-global",
			want: accessBody{OwnerID: 10, Readers: types.ACL{GIDs: []int32{1}}, Writers: types.ACL{GIDs: []int32{2}, Global: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, sawAccessCall := newAccessMockServer(t, base())

			execute(t, setAccess().Action, []string{tt.arg, "search-123"})

			if !*sawAccessCall {
				t.Fatal("expected a PUT .../access request, got none")
			}
			if got.OwnerID != tt.want.OwnerID {
				t.Errorf("OwnerID = %d, want %d", got.OwnerID, tt.want.OwnerID)
			}
			if !slices.Equal(got.Readers.GIDs, tt.want.Readers.GIDs) {
				t.Errorf("Readers.GIDs = %v, want %v", got.Readers.GIDs, tt.want.Readers.GIDs)
			}
			if got.Readers.Global != tt.want.Readers.Global {
				t.Errorf("Readers.Global = %v, want %v", got.Readers.Global, tt.want.Readers.Global)
			}
			if !slices.Equal(got.Writers.GIDs, tt.want.Writers.GIDs) {
				t.Errorf("Writers.GIDs = %v, want %v", got.Writers.GIDs, tt.want.Writers.GIDs)
			}
			if got.Writers.Global != tt.want.Writers.Global {
				t.Errorf("Writers.Global = %v, want %v", got.Writers.Global, tt.want.Writers.Global)
			}
		})
	}
}

// TestSetAccessNoFlagsIsANoop confirms that calling set-access with no
// access flags at all round-trips the search's existing state unchanged
// (rather than, say, defaulting any field to its zero value).
func TestSetAccessNoFlagsIsANoop(t *testing.T) {
	existing := types.SearchInfo{}
	existing.OwnerID = 7
	existing.Readers = types.ACL{GIDs: []int32{1, 2}, Global: true}
	existing.Writers = types.ACL{GIDs: []int32{3}, Global: false}

	got, sawAccessCall := newAccessMockServer(t, existing)

	execute(t, setAccess().Action, []string{"search-123"})

	if !*sawAccessCall {
		t.Fatal("expected a PUT .../access request, got none")
	}
	if got.OwnerID != existing.OwnerID {
		t.Errorf("OwnerID = %d, want unchanged %d", got.OwnerID, existing.OwnerID)
	}
	if !slices.Equal(got.Readers.GIDs, existing.Readers.GIDs) || got.Readers.Global != existing.Readers.Global {
		t.Errorf("Readers = %+v, want unchanged %+v", got.Readers, existing.Readers)
	}
	if !slices.Equal(got.Writers.GIDs, existing.Writers.GIDs) || got.Writers.Global != existing.Writers.Global {
		t.Errorf("Writers = %+v, want unchanged %+v", got.Writers, existing.Writers)
	}
}

// TestSetAccessGroupAlias confirms the old "set-groups" command name still
// resolves to set-access when dispatched by name (rather than invoked
// directly), so existing scripts/muscle memory keep working after the
// setGroup -> setAccess rename. Dispatched through a bare parent command
// rather than the real queries.NewNav() tree, since NewNav() pulls in
// sibling actions (e.g. info()) that panic outside of full CLI bootstrap
// due to unrelated uninitialized global state (clilog.Writer).
func TestSetAccessGroupAlias(t *testing.T) {
	existing := types.SearchInfo{}
	existing.OwnerID = 1
	existing.Writers = types.ACL{GIDs: []int32{9}}

	got, sawAccessCall := newAccessMockServer(t, existing)

	root := &cobra.Command{Use: "root"}
	group.AddActionGroup(root)
	root.AddCommand(setAccess().Action)
	execute(t, root, []string{"set-groups", "--reader-groups=1,2", "search-123"})

	if !*sawAccessCall {
		t.Fatal("expected a PUT .../access request, got none")
	}
	if want := []int32{1, 2}; !slices.Equal(got.Readers.GIDs, want) {
		t.Errorf("Readers.GIDs = %v, want %v", got.Readers.GIDs, want)
	}
	if !slices.Equal(got.Writers.GIDs, existing.Writers.GIDs) {
		t.Errorf("Writers.GIDs = %v, want unchanged %v", got.Writers.GIDs, existing.Writers.GIDs)
	}
}
