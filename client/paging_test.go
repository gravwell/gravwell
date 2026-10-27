/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client_test

import (
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
)

// maxFakeCalls bounds the calls a fakeListing will answer.
// Helps prevent an errant test from OoM-ing
const maxFakeCalls = 2000

var errRunaway = errors.New("fake listing called too many times")

// fakeListing serves count macros named "0".."<n-1>",
// using the index of the first macro on the next page as the cursor.
// It records the options of every call it receives.
//
// Rejects options that fail QueryOptions.Validate (ex: a Limit < 1), like the backend should.
type fakeListing struct {
	count int
	calls []types.QueryOptions
}

func (f *fakeListing) list(opts types.QueryOptions) (types.MacroListResponse, error) {
	f.calls = append(f.calls, opts)
	if len(f.calls) > maxFakeCalls {
		return types.MacroListResponse{}, errRunaway
	} else if err := opts.Validate(); err != nil {
		return types.MacroListResponse{}, err
	}
	start := 0
	if opts.CursorID != "" {
		var err error
		if start, err = strconv.Atoi(opts.CursorID); err != nil {
			return types.MacroListResponse{}, err
		}
	}
	end := min(start+int(opts.Limit), f.count)

	var resp types.MacroListResponse
	resp.TotalCount = f.count
	for i := start; i < end; i++ {
		resp.Results = append(resp.Results, types.Macro{CommonFields: types.CommonFields{Name: strconv.Itoa(i)}})
	}
	if end < f.count {
		resp.CursorNext = strconv.Itoa(end)
	}
	return resp, nil
}

func names(ms []types.Macro) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

func wantNames(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = strconv.Itoa(i)
	}
	return out
}

func TestPages(t *testing.T) {
	t.Run("FollowsCursorAndKeepsOptions", func(t *testing.T) {
		f := &fakeListing{count: 25}
		opts := types.QueryOptions{Limit: 10, Search: "keep me", All: true}
		var got []types.Macro
		var pages int
		for page, err := range client.Pages(opts, f.list) {
			if err != nil {
				t.Fatal(err)
			}
			pages++
			got = append(got, page.Results...)
		}
		if pages != 3 {
			t.Fatalf("expected 3 pages, got %d", pages)
		}
		if !slices.Equal(names(got), wantNames(25)) {
			t.Fatalf("unexpected results: %v", names(got))
		}
		// every call should match the original options, save for the cursor
		for i, c := range f.calls {
			c.CursorID = ""
			if !equalOpts(c, opts) {
				t.Fatalf("call %d altered options: %+v", i, f.calls[i])
			}
		}
	})
	t.Run("StopsOnError", func(t *testing.T) {
		boom := errors.New("boom")
		var calls int
		list := func(types.QueryOptions) (types.MacroListResponse, error) {
			calls++
			return types.MacroListResponse{}, boom
		}
		for _, err := range client.Pages(types.QueryOptions{}, list) {
			if !errors.Is(err, boom) {
				t.Fatalf("expected %v, got %v", boom, err)
			}
		}
		if calls != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})
	t.Run("StalledCursor", func(t *testing.T) {
		list := func(types.QueryOptions) (types.MacroListResponse, error) {
			var resp types.MacroListResponse
			resp.CursorNext = "same"
			return resp, nil
		}
		var lastErr error
		var pages int
		for _, err := range client.Pages(types.QueryOptions{}, list) {
			pages++
			lastErr = err
		}
		if !errors.Is(lastErr, client.ErrCursorStalled) {
			t.Fatalf("expected %v, got %v", client.ErrCursorStalled, lastErr)
		} else if pages != 3 { // first page, second page (which repeats the cursor), then the error
			t.Fatalf("expected 3 iterations, got %d", pages)
		}
	})
}

func TestCollect(t *testing.T) {
	const maxLimit = types.QueryOptionsMaxLimit
	tests := []struct {
		name      string
		itemCount int   // number of items the listing holds
		qoLimit   int32 // page size passed in opts.Limit
		maxCount  int
		wantCount int
		wantCalls int
	}{
		{"Empty", 0, 0, 0, 0, 1},
		{"DefaultPageSize", 5, 0, 0, 5, 1},
		{"DefaultPageSizeManyPages", 2*types.QueryOptionsDefaultLimit + 1, 0, 0, 2*types.QueryOptionsDefaultLimit + 1, 3},
		{"ExplicitPageSize", 25, 10, 0, 25, 3},
		{"CountIsMultipleOfPageSize", 20, 10, 0, 20, 2},
		{"MaxPageSize", 2*maxLimit + 1, maxLimit, 0, 2*maxLimit + 1, 3},
		{"NegativeMaxCountCollectsAll", 25, 10, -1, 25, 3},
		{"MaxCountUnderPageSize", 50, 10, 5, 5, 1},
		{"MaxCountSpansPages", 50, 10, 25, 25, 3},
		{"MaxCountOnPageBoundary", 50, 10, 20, 20, 2},
		{"MaxCountOverAvailable", 5, 10, 10, 5, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeListing{count: tt.itemCount}
			got, err := client.Collect(types.QueryOptions{Limit: tt.qoLimit}, f.list, tt.maxCount)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(got), wantNames(tt.wantCount)) {
				t.Fatalf("expected %d results in order, got %d: %v", tt.wantCount, len(got), names(got))
			}
			if len(f.calls) != tt.wantCalls {
				t.Fatalf("expected %d calls, got %d", tt.wantCalls, len(f.calls))
			}
			// the fake reports an accurate TotalCount, so items should have been sized exactly
			if cap(got) != len(got) {
				t.Fatalf("expected a capacity of %d, got %d", len(got), cap(got))
			}
			// every call should use the requested page size (or the default, if unset)
			wantLimit := tt.qoLimit
			if wantLimit == 0 {
				wantLimit = types.QueryOptionsDefaultLimit
			}
			for i, c := range f.calls {
				if c.Limit != wantLimit {
					t.Fatalf("call %d used a page size of %d, expected %d", i, c.Limit, wantLimit)
				}
			}
		})
	}
	t.Run("InvalidPageSize", func(t *testing.T) {
		for _, limit := range []int32{-1, maxLimit + 1} {
			f := &fakeListing{count: 5}
			if _, err := client.Collect(types.QueryOptions{Limit: limit}, f.list, 0); !errors.Is(err, types.ErrBadQueryOptionsLimit) {
				t.Fatalf("limit %d: expected %v, got %v", limit, types.ErrBadQueryOptionsLimit, err)
			}
		}
	})
	t.Run("TotalCountUnderreported", func(t *testing.T) {
		f := &fakeListing{count: 25}
		list := func(opts types.QueryOptions) (types.MacroListResponse, error) {
			resp, err := f.list(opts)
			resp.TotalCount = 0
			return resp, err
		}
		got, err := client.Collect(types.QueryOptions{Limit: 10}, list, -1)
		if err != nil {
			t.Fatal(err)
		} else if !slices.Equal(names(got), wantNames(25)) {
			t.Fatalf("expected all 25 results, got %v", names(got))
		}
	})
	t.Run("ErrorOnFirstPage", func(t *testing.T) {
		boom := errors.New("boom")
		list := func(types.QueryOptions) (types.MacroListResponse, error) {
			return types.MacroListResponse{}, boom
		}
		if got, err := client.Collect(types.QueryOptions{}, list, 10); !errors.Is(err, boom) {
			t.Fatalf("expected %v, got %v", boom, err)
		} else if len(got) != 0 {
			t.Fatalf("expected no results, got %v", names(got))
		}
	})
	t.Run("ErrorMidwayReturnsPartialResults", func(t *testing.T) {
		boom := errors.New("boom")
		f := &fakeListing{count: 25}
		list := func(opts types.QueryOptions) (types.MacroListResponse, error) {
			if len(f.calls) == 1 { // fail the second page
				return types.MacroListResponse{}, boom
			}
			return f.list(opts)
		}
		got, err := client.Collect(types.QueryOptions{Limit: 10}, list, 0)
		if !errors.Is(err, boom) {
			t.Fatalf("expected %v, got %v", boom, err)
		} else if !slices.Equal(names(got), wantNames(10)) {
			t.Fatalf("expected the first page's results, got %v", names(got))
		}
	})
}

// equalOpts compares the fields of QueryOptions that are comparable.
func equalOpts(a, b types.QueryOptions) bool {
	return a.Type == b.Type && a.IncludeDeleted == b.IncludeDeleted && a.Version == b.Version &&
		a.All == b.All && a.OrderBy == b.OrderBy && a.OrderDirection == b.OrderDirection &&
		a.CursorID == b.CursorID && a.Limit == b.Limit && a.Search == b.Search &&
		len(a.Filters) == len(b.Filters)
}
