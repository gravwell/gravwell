/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestKVTableRowKeyEscaping(t *testing.T) {
	var gotMethod, gotID, gotKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kv/{id}/rows/{key}", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotID, gotKey = r.Method, r.PathValue("id"), r.PathValue("key")
		w.Write([]byte(`{"Key":"a/b c?d","Version":1}`))
	})
	c := newTestClient(t, mux)

	const key = "a/b c?d"
	row, err := c.PutKVTableRow("tbl", key, types.KVTableRowWrite{})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotID != "tbl" || gotKey != key {
		t.Errorf("server saw id %q key %q, want %q %q", gotID, gotKey, "tbl", key)
	}
	if row.Key != key || row.Version != 1 {
		t.Errorf("unexpected row %+v", row)
	}
}

func TestDeleteKVTableRowVersion(t *testing.T) {
	var gotVersion string
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/kv/{id}/rows/{key}", func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.URL.Query().Get("expected_version")
	})
	c := newTestClient(t, mux)

	if err := c.DeleteKVTableRow("tbl", "k"); err != nil {
		t.Fatal(err)
	}
	if gotVersion != "" {
		t.Errorf("unconditional delete sent expected_version=%q", gotVersion)
	}
	if err := c.DeleteKVTableRowVersion("tbl", "k", 7); err != nil {
		t.Fatal(err)
	}
	if gotVersion != "7" {
		t.Errorf("expected_version = %q, want 7", gotVersion)
	}
}

func TestListKVTableRowsAddsTableFilter(t *testing.T) {
	var got types.QueryOptions
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/list/kv-rows", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		w.Write([]byte(`{"Results":[]}`))
	})
	c := newTestClient(t, mux)

	caller := make([]types.Filter, 1, 4) // spare capacity, so an in-place append would be visible
	caller[0] = types.Filter{Key: "owner", Operation: "=", Values: []any{"alice"}}
	if _, err := c.ListKVTableRows("tbl", types.QueryOptions{Filters: caller}); err != nil {
		t.Fatal(err)
	}
	if len(got.Filters) != 2 || got.Filters[1].Key != "TableID" || got.Filters[1].Values[0] != "tbl" {
		t.Errorf("server saw filters %+v", got.Filters)
	}
	if extra := caller[:2][1]; extra.Key != "" {
		t.Errorf("caller's filter slice was modified: %+v", extra)
	}
}

func TestKVTableRowArgs(t *testing.T) {
	c := newTestClient(t, http.NewServeMux())
	if _, err := c.GetKVTableRow("", "k"); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty table ID: got %v", err)
	}
	if _, err := c.GetKVTableRow("tbl", ""); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("empty key: got %v", err)
	}
	if _, err := c.ListKVTableRows("", types.QueryOptions{}); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty table ID on list: got %v", err)
	}
}
