/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
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

type SearchHistoryListResponse struct {
	BaseListResponse
	Results []SearchHistoryEntry `json:"results"`
}
