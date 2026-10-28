/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"errors"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

func TestPackScheduledSearchRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		search types.Searchable
	}{
		{"saved_query", types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"}},
		{"query_string", types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := types.ScheduledSearch{
				CommonFields: types.CommonFields{
					Name:        "test search",
					Description: "a description",
					ID:          "ss-id",
				},
				AutomationCommonFields: types.AutomationCommonFields{
					Schedule: "* * * * *",
				},
				Search:   tt.search,
				Duration: -3600,
			}

			packed := PackScheduledSearch(ss)
			if packed.Search != tt.search {
				t.Fatalf("PackScheduledSearch did not carry Search through: got %+v, want %+v", packed.Search, tt.search)
			}
			if err := packed.Validate(); err != nil {
				t.Fatalf("Validate failed on a well-formed %s search: %v", tt.name, err)
			}

			unpacked := packed.Unpackage(42, []int32{1, 2})
			if unpacked.Search != tt.search {
				t.Fatalf("Unpackage did not carry Search through: got %+v, want %+v", unpacked.Search, tt.search)
			}
			if unpacked.OwnerID != 42 {
				t.Fatalf("Unpackage did not set OwnerID: got %v", unpacked.OwnerID)
			}
		})
	}
}

func TestPackedScheduledSearchValidate(t *testing.T) {
	base := PackedScheduledSearch{
		Name:     "test search",
		Schedule: "* * * * *",
		Duration: -60,
	}

	tests := []struct {
		name    string
		mutate  func(p PackedScheduledSearch) PackedScheduledSearch
		wantErr error // nil means "no error expected"; non-nil means Validate() must return this error
	}{
		{
			"valid saved_query",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "abc"}
				return p
			},
			nil,
		},
		{
			"valid query_string",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"}
				return p
			},
			nil,
		},
		{
			"unknown kind",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: "template", ID: "abc"}
				return p
			},
			ErrInvalidSearchKind,
		},
		{
			"empty kind",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				return p
			},
			ErrInvalidSearchKind,
		},
		{
			"saved_query missing ID",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: types.SearchableKindSavedQuery}
				return p
			},
			ErrMissingSearchID,
		},
		{
			"query_string missing QueryString",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: types.SearchableKindQueryString}
				return p
			},
			ErrMissingSearchQueryString,
		},
		{
			"non-negative duration",
			func(p PackedScheduledSearch) PackedScheduledSearch {
				p.Search = types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"}
				p.Duration = 0
				return p
			},
			ErrInvalidDuration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.mutate(base)
			err := p.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() returned nil error, expected %v", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
