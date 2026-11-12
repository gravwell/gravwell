/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scheduled

import (
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
)

// TestApplySearchEdit checks that editing a scheduled search in gwcli doesn't wipe out a
// saved-query reference just because you changed some other field.
func TestApplySearchEdit(t *testing.T) {
	tests := []struct {
		name   string
		item   types.ScheduledSearch
		val    string
		expect types.Searchable
	}{
		{
			"saved query, value unchanged: leave it alone",
			types.ScheduledSearch{Search: types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"}},
			"saved-query-id",
			types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"},
		},
		{
			"saved query, value changed: becomes a raw query",
			types.ScheduledSearch{Search: types.Searchable{Kind: types.SearchableKindSavedQuery, ID: "saved-query-id"}},
			"tag=default",
			types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"},
		},
		{
			"raw query, value unchanged: leave it alone",
			types.ScheduledSearch{Search: types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"}},
			"tag=default",
			types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"},
		},
		{
			"raw query, value changed: text is updated",
			types.ScheduledSearch{Search: types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=default"}},
			"tag=other",
			types.Searchable{Kind: types.SearchableKindQueryString, QueryString: "tag=other"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := tt.item
			applySearchEdit(&item, tt.val)
			if item.Search != tt.expect {
				t.Fatalf("applySearchEdit(%+v, %q) = %+v, want %+v", tt.item.Search, tt.val, item.Search, tt.expect)
			}
		})
	}
}
