/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/client/types/kits"
)

// TestScheduledSearchWriteReadRoundTrip confirms writeScheduledSearch/readScheduledSearch
// round-trip both Search kinds, and specifically that a saved_query search never gets a
// sibling ".search" file written for it (it has no text to break out), while a
// query_string search does.
func TestScheduledSearchWriteReadRoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		search         types.Searchable
		wantSearchFile bool
	}{
		{
			"query_string writes a .search file",
			types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"},
			true,
		},
		{
			"saved_query writes no .search file",
			types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			id := "ss1"
			in := kits.PackedScheduledSearch{
				Name:     "test search",
				Schedule: "* * * * *",
				Duration: -60,
				ID:       id,
				Search:   tt.search,
			}

			if err := writeScheduledSearch(dir, id, in); err != nil {
				t.Fatalf("writeScheduledSearch failed: %v", err)
			}

			searchPath := filepath.Join(dir, "scheduled", id+".search")
			_, statErr := os.Stat(searchPath)
			gotSearchFile := statErr == nil
			if gotSearchFile != tt.wantSearchFile {
				t.Fatalf("expected .search file present=%v, got present=%v (stat err: %v)", tt.wantSearchFile, gotSearchFile, statErr)
			}

			out, err := readScheduledSearch(dir, id)
			if err != nil {
				t.Fatalf("readScheduledSearch failed: %v", err)
			}
			if out.Search != tt.search {
				t.Fatalf("round trip did not preserve Search: got %+v, want %+v", out.Search, tt.search)
			}
			if out.Name != in.Name || out.Schedule != in.Schedule || out.Duration != in.Duration {
				t.Fatalf("round trip did not preserve other fields: got %+v, want %+v", out, in)
			}
		})
	}
}
