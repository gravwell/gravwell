/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

// SearchHistoryEntry represents a stored search history entry in the registry.
// This replaces the search history implementation from the webstore package.
type SearchHistoryEntry struct {
	CommonFields

	UserQuery      string
	EffectiveQuery string
	Launched       time.Time
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SearchHistoryEntry) MarshalJSON() ([]byte, error) {
	type dummySearchHistoryEntry SearchHistoryEntry
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummySearchHistoryEntry(s))
}

type SearchHistoryListResponse struct {
	BaseListResponse
	Results []SearchHistoryEntry
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SearchHistoryListResponse) MarshalJSON() ([]byte, error) {
	type dummySearchHistoryListResponse SearchHistoryListResponse
	s.Results = nonNilSlice(s.Results)
	s.BaseListResponse = s.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummySearchHistoryListResponse(s))
}
