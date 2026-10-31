/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

// tagServer serves POST /api/list/tags over tags, using the index of the first
// tag on the next page as the cursor. It records the options of every call.
type tagServer struct {
	tags  []string
	calls []types.QueryOptions
}

func (s *tagServer) mux(t *testing.T) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/list/tags", func(w http.ResponseWriter, r *http.Request) {
		var opts types.QueryOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			t.Errorf("bad request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		s.calls = append(s.calls, opts)

		start := 0
		if opts.CursorID != "" {
			start, _ = strconv.Atoi(opts.CursorID)
		}

		end := len(s.tags)
		if opts.Limit > 0 {
			end = min(end, start+int(opts.Limit))
		}

		resp := types.TagListResponse{Results: []types.Tag{}}
		resp.Type = "tag"
		resp.TotalCount = len(s.tags)
		resp.Offset = start

		for _, name := range s.tags[start:end] {
			resp.Results = append(resp.Results, types.Tag{Name: name})
		}

		if end < len(s.tags) {
			resp.CursorNext = strconv.Itoa(end)
		}

		json.NewEncoder(w).Encode(resp)
	})

	return mux
}

func tagNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("tag%05d", i)
	}

	return names
}

func TestListTags(t *testing.T) {
	srv := &tagServer{tags: tagNames(5)}
	c := newTestClient(t, srv.mux(t))

	opts := types.QueryOptions{Limit: 2, Search: "tag", OrderBy: "Name", OrderDirection: "DESC"}
	resp, err := c.ListTags(opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(srv.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(srv.calls))
	}

	got := srv.calls[0]
	if got.Limit != opts.Limit ||
		got.Search != opts.Search ||
		got.OrderBy != opts.OrderBy ||
		got.OrderDirection != opts.OrderDirection {
		t.Fatalf("server got options %+v, want %+v", got, opts)
	}

	want := []types.Tag{{Name: "tag00000"}, {Name: "tag00001"}}
	if !slices.Equal(resp.Results, want) {
		t.Fatalf("got results %v, want %v", resp.Results, want)
	}

	if resp.Type != "tag" || resp.TotalCount != 5 || resp.CursorNext != "2" {
		t.Fatalf("got Type %q TotalCount %d CursorNext %q", resp.Type, resp.TotalCount, resp.CursorNext)
	}
}

func TestGetTags(t *testing.T) {
	cases := []struct {
		name      string
		tags      int
		wantCalls int
	}{
		{"no tags", 0, 1},
		{"one page", 3, 1},
		{"exactly one full page", types.QueryOptionsMaxLimit, 1},
		{"several pages", 2*types.QueryOptionsMaxLimit + 5, 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := &tagServer{tags: tagNames(c.tags)}
			cli := newTestClient(t, srv.mux(t))

			got, err := cli.GetTags()
			if err != nil {
				t.Fatal(err)
			}

			if got == nil {
				t.Fatal("GetTags returned nil, want a non-nil slice")
			}

			if !slices.Equal(got, srv.tags) {
				t.Fatalf("got %d tags, want %d", len(got), len(srv.tags))
			}

			if len(srv.calls) != c.wantCalls {
				t.Fatalf("got %d calls, want %d", len(srv.calls), c.wantCalls)
			}

			for i, call := range srv.calls {
				if call.Limit != types.QueryOptionsMaxLimit {
					t.Fatalf("call %d asked for Limit %d, want %d", i, call.Limit, types.QueryOptionsMaxLimit)
				}
			}
		})
	}
}

func TestGetTagsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/list/tags", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	c := newTestClient(t, mux)
	if tags, err := c.GetTags(); err == nil {
		t.Fatalf("got tags %v and no error, want an error", tags)
	}
}
