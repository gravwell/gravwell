/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestDataTableRowKeyEscaping(t *testing.T) {
	var gotMethod, gotID, gotKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tables/{id}/rows/{key}", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotID, gotKey = r.Method, r.PathValue("id"), r.PathValue("key")
		w.Write([]byte(`{"Values":{"host":"k"},"Version":1}`))
	})
	// A key that escapes its path segment lands on one of these instead.
	for _, p := range []string{"/api/tables/{id}", "/api/tables/{id}/rows"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("%s %s reached %s", r.Method, r.URL.EscapedPath(), p)
		})
	}
	c := newTestClient(t, mux)

	calls := map[string]func(id, key string) error{
		http.MethodGet: func(id, key string) error { _, err := c.GetDataTableRow(id, key); return err },
		http.MethodPut: func(id, key string) error {
			_, err := c.PutDataTableRow(id, key, types.DataTableRowWrite{})
			return err
		},
		http.MethodPatch: func(id, key string) error {
			_, err := c.UpdateDataTableRow(id, key, types.DataTableRowPatch{Clear: []string{"ip"}})
			return err
		},
		http.MethodDelete: func(id, key string) error { return c.DeleteDataTableRow(id, key) },
	}
	ids := []string{"tbl", "t/1", ".."}
	keys := []string{"a/b c?d", "..", ".", "...", "../x", "%2E", "10.0.0.1"}
	for method, call := range calls {
		for _, id := range ids {
			for _, key := range keys {
				gotMethod, gotID, gotKey = "", "", ""
				if err := call(id, key); err != nil {
					t.Errorf("%s id %q key %q: %v", method, id, key, err)
				} else if gotMethod != method || gotID != id || gotKey != key {
					t.Errorf("%s id %q key %q: server saw %s id %q key %q", method, id, key, gotMethod, gotID, gotKey)
				}
			}
		}
	}
}

func TestUpdateDataTableRowClear(t *testing.T) {
	var got map[string]jsontext.Value
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/tables/{id}/rows/{key}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		w.Write([]byte(`{"Values":{"host":"k"},"Version":2}`))
	})
	c := newTestClient(t, mux)

	if _, err := c.UpdateDataTableRow("tbl", "k", types.DataTableRowPatch{Clear: []string{"ip"}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got["Clear"]) != `["ip"]` {
		t.Errorf("server saw %v, want only Clear", got)
	}
}

func TestDeleteDataTableRowVersion(t *testing.T) {
	var gotVersion string
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/tables/{id}/rows/{key}", func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.URL.Query().Get("expected_version")
	})
	c := newTestClient(t, mux)

	if err := c.DeleteDataTableRow("tbl", "k"); err != nil {
		t.Fatal(err)
	}
	if gotVersion != "" {
		t.Errorf("unconditional delete sent expected_version=%q", gotVersion)
	}
	if err := c.DeleteDataTableRowVersion("tbl", "k", 7); err != nil {
		t.Fatal(err)
	}
	if gotVersion != "7" {
		t.Errorf("expected_version = %q, want 7", gotVersion)
	}
}

func TestListDataTableRowsAddsTableFilter(t *testing.T) {
	var got types.QueryOptions
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/list/table-rows", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		w.Write([]byte(`{"Results":[]}`))
	})
	c := newTestClient(t, mux)

	caller := make([]types.Filter, 1, 4) // spare capacity, so an in-place append would be visible
	caller[0] = types.Filter{Key: "owner", Operation: "=", Values: []any{"alice"}}
	if _, err := c.ListDataTableRows("tbl", types.QueryOptions{Filters: caller}); err != nil {
		t.Fatal(err)
	}
	if len(got.Filters) != 2 || got.Filters[1].Key != "TableID" || got.Filters[1].Values[0] != "tbl" {
		t.Errorf("server saw filters %+v", got.Filters)
	}
	if extra := caller[:2][1]; extra.Key != "" {
		t.Errorf("caller's filter slice was modified: %+v", extra)
	}
}

func TestDataTableRowArgs(t *testing.T) {
	c := newTestClient(t, http.NewServeMux())
	if _, err := c.GetDataTableRow("", "k"); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty table ID: got %v", err)
	}
	if _, err := c.GetDataTableRow("tbl", ""); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("empty key: got %v", err)
	}
	if _, err := c.ListDataTableRows("", types.QueryOptions{}); !errors.Is(err, ErrEmptyID) {
		t.Errorf("empty table ID on list: got %v", err)
	}
}
