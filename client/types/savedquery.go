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

type SavedQueryTimeframe struct {
	Duration  string    `json:"durationString"`
	End       time.Time `json:"end"`
	Start     time.Time `json:"start"`
	Timeframe string    `json:"timeframe"`
	Timezone  string    `json:"timezone"`
}

type SavedQueryListResponse struct {
	BaseListResponse
	Results []SavedQuery `json:"results"`
}
