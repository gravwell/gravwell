/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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
	mux.HandleFunc(client.SEARCHES_URL+"/", func(w http.ResponseWriter, r *http.Request) {
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
		if want := "/api/searches/search-123/access"; gotPath != want {
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

// TestDownloadSearch exercises DownloadSearch's initiate-POST-then-fetch-GET flow.
func TestDownloadSearch(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })

	const sid = "search-dl-1"
	const resultBody = "entry one\nentry two\n"

	var (
		gotInitiateMethod string
		gotInitiateBody   types.SearchDownloadRequest
		fetchCalls        int
		initiateStatus    = http.StatusOK
		fetchStatus       = http.StatusOK
	)

	downloadsPath := fmt.Sprintf(client.SEARCHES_ID_DOWNLOADS_URL, sid)
	resourcePath := downloadsPath + "/resource-1"

	mux := http.NewServeMux()
	mux.HandleFunc(downloadsPath, func(w http.ResponseWriter, r *http.Request) {
		gotInitiateMethod = r.Method
		gotInitiateBody = types.SearchDownloadRequest{}
		if err := json.NewDecoder(r.Body).Decode(&gotInitiateBody); err != nil {
			t.Errorf("failed to decode initiate request body: %v", err)
		}
		if initiateStatus != http.StatusOK {
			w.WriteHeader(initiateStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.SearchDownloadResponse{
			DownloadResourceURL: resourcePath,
			SearchID:            sid,
		})
	})
	mux.HandleFunc(resourcePath, func(w http.ResponseWriter, r *http.Request) {
		fetchCalls++
		if fetchStatus != http.StatusOK {
			w.WriteHeader(fetchStatus)
			return
		}
		_, _ = w.Write([]byte(resultBody))
	})
	srv := http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Shutdown(t.Context()) })

	c, err := client.NewOpts(client.Opts{Server: l.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("round trips the initiate-then-fetch flow and streams the resource body", func(t *testing.T) {
		rc, err := c.DownloadSearch(context.Background(), sid, types.TimeRange{}, "text")
		if err != nil {
			t.Fatalf("DownloadSearch: %v", err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		if string(data) != resultBody {
			t.Errorf("body = %q, want %q", data, resultBody)
		}
		if gotInitiateMethod != http.MethodPost {
			t.Errorf("initiate method = %q, want %q", gotInitiateMethod, http.MethodPost)
		}
		if gotInitiateBody.Format != "text" {
			t.Errorf("Format = %q, want text", gotInitiateBody.Format)
		}
	})

	t.Run("a zero-value TimeRange is not forwarded as a Timeframe", func(t *testing.T) {
		rc, err := c.DownloadSearch(context.Background(), sid, types.TimeRange{}, "text")
		if err != nil {
			t.Fatalf("DownloadSearch: %v", err)
		}
		_, _ = io.Copy(io.Discard, rc)
		rc.Close()

		if gotInitiateBody.Timeframe != nil {
			t.Errorf("expected no timeframe for a zero-value TimeRange, got %+v", gotInitiateBody.Timeframe)
		}
	})

	t.Run("a populated TimeRange is forwarded as a Timeframe", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		tr := types.TimeRange{StartTS: entry.FromStandard(start), EndTS: entry.FromStandard(end)}

		rc, err := c.DownloadSearch(context.Background(), sid, tr, "text")
		if err != nil {
			t.Fatalf("DownloadSearch: %v", err)
		}
		_, _ = io.Copy(io.Discard, rc)
		rc.Close()

		if gotInitiateBody.Timeframe == nil {
			t.Fatal("expected a timeframe to be set")
		}
		if !gotInitiateBody.Timeframe.Start.Equal(start) {
			t.Errorf("Timeframe.Start = %v, want %v", gotInitiateBody.Timeframe.Start, start)
		}
		if !gotInitiateBody.Timeframe.End.Equal(end) {
			t.Errorf("Timeframe.End = %v, want %v", gotInitiateBody.Timeframe.End, end)
		}
	})

	t.Run("a non-200 from the initiate request is surfaced without ever fetching the resource", func(t *testing.T) {
		initiateStatus = http.StatusInternalServerError
		t.Cleanup(func() { initiateStatus = http.StatusOK })
		priorFetchCalls := fetchCalls

		if _, err := c.DownloadSearch(context.Background(), sid, types.TimeRange{}, "text"); err == nil {
			t.Fatal("expected an error, got nil")
		}
		if fetchCalls != priorFetchCalls {
			t.Errorf("resource endpoint should not have been called after the initiate request failed")
		}
	})

	t.Run("a 404 from the resource fetch (e.g. an expired/aged-out download) is surfaced as an error", func(t *testing.T) {
		fetchStatus = http.StatusNotFound
		t.Cleanup(func() { fetchStatus = http.StatusOK })

		rc, err := c.DownloadSearch(context.Background(), sid, types.TimeRange{}, "text")
		if rc != nil {
			rc.Close()
			t.Error("expected a nil reader on error")
		}
		var clientErr *client.ClientError
		if !errors.As(err, &clientErr) {
			t.Fatalf("err = %v, want *client.ClientError", err)
		}
		if clientErr.StatusCode != http.StatusNotFound {
			t.Errorf("StatusCode = %d, want %d", clientErr.StatusCode, http.StatusNotFound)
		}
		// gwcli string-matches "404" in the error text to detect an aged-out search.
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error text %q does not contain \"404\"", err.Error())
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
