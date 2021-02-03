/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"time"
)

type SearchLog struct {
	UID            int32  //who started the search
	GID            int32  //what group the search was assigned to, if any
	UserQuery      string //what the user actually typed
	EffectiveQuery string //what was actually run
	Launched       time.Time
	Synced         bool
}

func (sl SearchLog) Equal(v SearchLog) bool {
	return sl.UID == v.UID && sl.GID == v.GID && sl.UserQuery == v.UserQuery && sl.EffectiveQuery == v.EffectiveQuery && sl.Launched == v.Launched
}

type SortableSearchLog []SearchLog

func (s SortableSearchLog) Len() int           { return len(s) }
func (s SortableSearchLog) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s SortableSearchLog) Less(i, j int) bool { return s[i].Launched.Before(s[j].Launched) }
