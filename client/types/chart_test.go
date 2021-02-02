/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"sort"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestEmptyChartableValueSet(t *testing.T) {
	var cvs ChartableValueSet
	if err := cvs.SortByNames(); err != nil {
		t.Fatal(err)
	}

	cvs.Names = []string{`a`, `b`, `c`}
	if err := cvs.SortByNames(); err != nil {
		t.Fatal(err)
	}

	//test one that SHOULD fail
	cvs.Values = []Chartable{
		Chartable{
			TS:   entry.Now(),
			Data: []ChartableDataPoint{1, 2}, //too short, should error
		},
	}

	if err := cvs.SortByNames(); err != ErrNameChartableMismatch {
		t.Fatal("Failed to catch name mismatch: ", err)
	}
}

func TestSinglChartableValueSet(t *testing.T) {
	cvs := ChartableValueSet{
		Names: []string{`c`, `b`, `a`},
		Values: []Chartable{
			Chartable{
				TS:   entry.Now(),
				Data: []ChartableDataPoint{3, 2, 1},
			},
			Chartable{
				TS:   entry.Now(),
				Data: []ChartableDataPoint{13, 12, 11},
			},
		},
	}
	//sort by name
	if err := cvs.SortByNames(); err != nil {
		t.Fatal(err)
	}
	//check that the values are in order
	for _, v := range cvs.Values {
		d := v.Data
		ok := sort.SliceIsSorted(d, func(i, j int) bool {
			return d[i] < d[j]
		})
		if !ok {
			t.Fatal("Not sorted", d, cvs.Names)
		}
	}
}
