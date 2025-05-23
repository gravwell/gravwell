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
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

var (
	ErrNameChartableMismatch = errors.New("Name lengths do not match chartable lengths")
	ChartableNaN             = ChartableDataPoint(math.NaN())
)

type ChartableDataPoint float64

type Chartable struct {
	Data []ChartableDataPoint
	TS   entry.Timestamp
}

type ChartableSet []Chartable

func (cs ChartableSet) Len() int { return len(cs) }
func (cs ChartableSet) TS(i int) entry.Timestamp {
	if i >= len(cs) || i < 0 {
		return entry.Timestamp{}
	}
	return cs[i].TS
}

func (cs ChartableSet) Sec(i int) int64 {
	return cs.TS(i).Sec
}

func (cs *ChartableSet) Add(c Chartable) {
	*cs = append(*cs, c)
}

func (cs *ChartableSet) Reset() {
	*cs = []Chartable{}
}

type KeyComponents struct {
	Keys []string
}

// ChartableValueSet is what is returned when we have a request for data
// the length of Names MUST BE the same length as each set of Values in each Set
type ChartableValueSet struct {
	Names      []string
	KeyComps   []KeyComponents `json:",omitempty"`
	Categories []string        `json:",omitempty"`
	Values     ChartableSet
}

// AddKeyComponents preps the ChartableValueSet with the appropriate key material
func (cvs *ChartableValueSet) AddKeyComponents(name, cat string, keys []string) error {
	for i := range cvs.Names {
		if cvs.Names[i] == name {
			return fmt.Errorf(`Name "%s" already exists`, name)
		}
	}
	addCat := true
	for i := range cvs.Categories {
		if cvs.Categories[i] == cat {
			addCat = false
			break
		}
	}
	cvs.Names = append(cvs.Names, name)
	if addCat {
		cvs.Categories = append(cvs.Categories, cat)
	}
	cvs.KeyComps = append(cvs.KeyComps, KeyComponents{Keys: keys})
	return nil
}

type ChartRequest struct {
	BaseRequest
}

type ChartResponse struct {
	BaseResponse
	Entries ChartableValueSet
}

// nst is a name sort translator, it sorts a name set and then fixes up the
// values in order of the Chartable in a ChartableValueSet.
type nst struct {
	trans []nsi
	temp  []ChartableDataPoint
}

type nsi struct {
	name string
	idx  int
}

func newNST(names []string) nst {
	v := make([]nsi, len(names))
	for i := range names {
		v[i].name = names[i]
		v[i].idx = i
	}
	sort.SliceStable(v, func(i, j int) bool {
		return v[i].name < v[j].name
	})
	return nst{
		trans: v,
		temp:  make([]ChartableDataPoint, len(names)),
	}
}

func (n nst) names() (nms []string) {
	nms = make([]string, len(n.trans))
	for i := range n.trans {
		nms[i] = n.trans[i].name
	}
	return
}

func (n nst) translate(c Chartable) error {
	if len(c.Data) != len(n.trans) {
		return ErrNameChartableMismatch
	}
	for i := range n.trans {
		n.temp[i] = c.Data[n.trans[i].idx]
	}
	copy(c.Data, n.temp)
	return nil
}

// Sort is a little helper that picks the right sort based on the data types exposed
// if there is only one set (one time slice for things like non time-series charts) it sorts by value
// if there is more than one time slice, it sortsby name
func (cvs *ChartableValueSet) Sort() error {
	if cvs == nil {
		return errors.New("nil ChartableValueSet")
	} else if len(cvs.Values) == 1 {
		//return cvs.SortByValue()
		if err := cvs.SortByValue(); err != nil {
			fmt.Println("failed to sort by value:", err)
			return err
		}
	}
	return cvs.SortByNames()
}

// SortByNames will sort the chartable data by name, keeping values coordinated
func (cvs *ChartableValueSet) SortByNames() error {
	if len(cvs.Names) == 0 {
		return nil
	}
	nst := newNST(cvs.Names)
	for i := range cvs.Values {
		if err := nst.translate(cvs.Values[i]); err != nil {
			return err
		}
	}
	cvs.Names = nst.names()
	return nil
}

// SortByValue will sort the series by the the first value in the chartable data set
// this will return an error if there is more than one time slice of data
func (cvs *ChartableValueSet) SortByValue() error {
	if len(cvs.Values) > 1 {
		return errors.New("ChartableValueSet.SortByValue does not support multiple time slices")
	} else if len(cvs.Values[0].Data) != len(cvs.Names) {
		return errors.New("ChartableValueSet series names do not match data value names")
	}

	sort.Sort(swapper{cvs: cvs})
	return nil
}

type swapper struct {
	cvs *ChartableValueSet
}

func (s swapper) Len() int {
	return len(s.cvs.Values[0].Data)
}

func (s swapper) Less(i, j int) bool {
	return s.cvs.Values[0].Data[i] < s.cvs.Values[0].Data[j]
}

func (s swapper) Swap(i, j int) {
	s.cvs.Values[0].Data[i], s.cvs.Values[0].Data[j] = s.cvs.Values[0].Data[j], s.cvs.Values[0].Data[i]
	s.cvs.Names[i], s.cvs.Names[j] = s.cvs.Names[j], s.cvs.Names[i]
}

func (cdp ChartableDataPoint) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(cdp)) {
		return jsonNull, nil
	}
	return json.Marshal(float64(cdp))
}

func (cdp ChartableDataPoint) IsNaN() bool {
	return math.IsNaN(float64(cdp))
}

type chartableDataPoints []ChartableDataPoint

func (cd chartableDataPoints) MarshalJSON() ([]byte, error) {
	if len(cd) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]ChartableDataPoint(cd))
}

func (cvs ChartableValueSet) MarshalJSON() ([]byte, error) {
	type alias ChartableValueSet
	return json.Marshal(&struct {
		Names  emptyStrings
		Values chtbls
	}{
		Names:  emptyStrings(cvs.Names),
		Values: chtbls(cvs.Values),
	})
}

type chtbls []Chartable

func (cs chtbls) MarshalJSON() ([]byte, error) {
	if len(cs) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]Chartable(cs))
}

type chtbl Chartable

func (cs chtbl) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TS   entry.Timestamp
		Data chartableDataPoints
	}{
		TS:   cs.TS,
		Data: chartableDataPoints(cs.Data),
	})
}
