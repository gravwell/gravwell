/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

type TableRow struct {
	TS  entry.Timestamp
	Row []string
}

type TableRowSet []TableRow
type TableValueSet struct {
	Columns []string
	Rows    TableRowSet
}

type TableRequest struct {
	BaseRequest
}

type TableResponse struct {
	BaseResponse
	Entries TableValueSet
}

// Gauge renderer
type GaugeValue struct {
	Name      string
	Magnitude float64
	Min       float64
	Max       float64
}

type GaugeRequest struct {
	BaseRequest
}

type GaugeResponse struct {
	BaseResponse
	Entries []GaugeValue
}

// Compare a table to this one. Return false on cols/rows if they do not match.
// If rows do not match, idx will have the index of the first offending row.
func (t *TableValueSet) Compare(u *TableValueSet) (cols bool, rows bool, idx int) {
	if len(t.Columns) != len(u.Columns) {
		return false, false, -1
	}
	for i, v := range t.Columns {
		if v != u.Columns[i] {
			return false, false, -1
		}
	}

	if len(t.Rows) != len(u.Rows) {
		return true, false, -1
	}
	for i, v := range t.Rows {
		for j, w := range v.Row {
			if w != u.Rows[i].Row[j] {
				return true, false, i
			}
		}
	}
	return true, true, 0
}

func (t TableValueSet) MarshalJSON() ([]byte, error) {
	type alias TableValueSet
	return json.Marshal(&struct {
		alias
		Columns emptyStrings
	}{
		alias:   alias(t),
		Columns: emptyStrings(t.Columns),
	})
}

func (r TableRow) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TS  entry.Timestamp
		Row emptyStrings
	}{
		TS:  r.TS,
		Row: emptyStrings(r.Row),
	})
}
