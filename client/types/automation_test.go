/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	v2 "encoding/json/v2"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/stretchr/testify/require"
)

// TestScheduledSearchSearchMarshal locks in the OAD contract for ScheduledSearch.Search:
// a saved_query Kind marshals Kind+ID with no QueryString key, and a query_string Kind
// marshals Kind+QueryString with no ID key. Neither variant is hydrated with the other's
// data -- that's the whole point of the composite field.
func TestScheduledSearchSearchMarshal(t *testing.T) {
	tests := []struct {
		name     string
		search   types.Searchable
		expected map[string]any
	}{
		{
			"saved_query kind",
			types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"},
			map[string]any{"Kind": "saved_query", "ID": "saved-query-id"},
		},
		{
			"query_string kind",
			types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"},
			map[string]any{"Kind": "query_string", "QueryString": "tag=default"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ss := types.ScheduledSearch{Search: tt.search}
			b, err := v2.Marshal(&ss)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, v2.Unmarshal(b, &decoded))

			search, ok := decoded["Search"].(map[string]any)
			require.True(t, ok, "Search field missing or not an object in %s", b)
			require.Equal(t, tt.expected, search)
		})
	}
}

// TestScheduledSearchToPatch confirms ToPatch carries the Search field through, and that
// an unset Search patch field is omitted from the marshaled JSON per the Optional/omitzero
// contract (see optional_test.go).
func TestScheduledSearchToPatch(t *testing.T) {
	ss := types.ScheduledSearch{
		Search: types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "abc"},
	}
	patch := ss.ToPatch()
	require.True(t, patch.Search.IsSet())
	require.Equal(t, ss.Search, patch.Search.Value())

	t.Run("unset Search is omitted from marshal", func(t *testing.T) {
		var p types.ScheduledSearchPatch
		p.Duration = types.NewOptional[int64](-60)
		b, err := v2.Marshal(&p)
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, v2.Unmarshal(b, &decoded))
		_, present := decoded["Search"]
		require.False(t, present, "Search key should be omitted when unset, got %s", b)
	})

	t.Run("set Search is included in marshal", func(t *testing.T) {
		var p types.ScheduledSearchPatch
		p.Search = types.NewOptional(types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"})
		b, err := v2.Marshal(&p)
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, v2.Unmarshal(b, &decoded))
		search, ok := decoded["Search"].(map[string]any)
		require.True(t, ok, "Search field missing or not an object in %s", b)
		require.Equal(t, map[string]any{"Kind": "query_string", "QueryString": "tag=default"}, search)
	})
}
