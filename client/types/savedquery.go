/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"time"
)

// SavedQuery is a stored Gravwell query. This replaces SearchLibrary in the old types.
type SavedQuery struct {
	CommonFields

	Query              string
	SuggestedTimeframe SavedQueryTimeframe
}

// SavedQueryPatch is the type used to request an update to an existing SavedQuery.
type SavedQueryPatch struct {
	CommonFieldsPatch
	Query              Optional[string]              `json:",omitzero"`
	SuggestedTimeframe Optional[SavedQueryTimeframe] `json:",omitzero"`
}

type SavedQueryTimeframe struct {
	Duration  string
	End       time.Time
	Start     time.Time
	Timeframe string
	Timezone  string
}

// ToPatch converts sq into a SavedQueryPatch with every field set.
func (sq SavedQuery) ToPatch() SavedQueryPatch {
	return SavedQueryPatch{
		CommonFieldsPatch:  sq.CommonFields.ToPatch(),
		Query:              NewOptional(sq.Query),
		SuggestedTimeframe: NewOptional(sq.SuggestedTimeframe),
	}
}

type SavedQueryListResponse struct {
	BaseListResponse
	Results []SavedQuery
}
