/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"time"
)

// SavedQuery is a stored Gravwell query. This replaces SearchLibrary in the old types.
type SavedQuery struct {
	CommonFields

	Query              string
	SuggestedTimeframe SavedQueryTimeframe
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SavedQuery) MarshalJSON() ([]byte, error) {
	type dummySavedQuery SavedQuery
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummySavedQuery(s))
}

type SavedQueryTimeframe struct {
	Duration  string
	End       time.Time
	Start     time.Time
	Timeframe string
	Timezone  string
}

type SavedQueryListResponse struct {
	BaseListResponse
	Results []SavedQuery
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SavedQueryListResponse) MarshalJSON() ([]byte, error) {
	type dummySavedQueryListResponse SavedQueryListResponse
	s.Results = nonNilSlice(s.Results)
	s.BaseListResponse = s.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummySavedQueryListResponse(s))
}
