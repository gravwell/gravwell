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
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type SearchModuleStatsUpdate struct {
	Stats []SearchModuleStats
	TS    entry.Timestamp
}

type SearchModuleStats struct {
	ModuleStatsUpdate
	Name, Args string
	started    entry.Timestamp
	stopped    entry.Timestamp
}

type ModuleStatsUpdate struct {
	InputCount, OutputCount uint64
	InputBytes, OutputBytes uint64
	Duration                time.Duration
	ScratchWritten          uint64 // Bytes of scratch written
}

func (m *ModuleStatsUpdate) Size() (sz int64) {
	sz = 3 * 16 //counts, duration
	return
}

func (s *SearchModuleStats) Size() int64 {
	return int64(len(s.Name)+len(s.Args)) + s.ModuleStatsUpdate.Size()
}

func (sms SearchModuleStats) JSON() ([]byte, error) {
	b, err := json.Marshal(sms)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (sms *ModuleStatsUpdate) AddIn(count, bts uint64) {
	sms.InputCount += count
	sms.InputBytes += bts
}

func (sms *ModuleStatsUpdate) AddOut(count, bts uint64) {
	sms.OutputCount += count
	sms.OutputBytes += bts
}

func (sms *ModuleStatsUpdate) Add(v ModuleStatsUpdate) {
	sms.InputCount += v.InputCount
	sms.InputBytes += v.InputBytes
	sms.OutputCount += v.OutputCount
	sms.OutputBytes += v.OutputBytes
}

func (sms ModuleStatsUpdate) Equal(t ModuleStatsUpdate) bool {
	return (sms.InputCount == t.InputCount) && (sms.OutputCount == t.OutputCount) &&
		(sms.InputBytes == t.InputBytes) && (sms.OutputBytes == t.OutputBytes) &&
		(sms.Duration == t.Duration)
}

func (sms SearchModuleStats) Equal(t SearchModuleStats) bool {
	return sms.Name == t.Name && sms.ModuleStatsUpdate.Equal(t.ModuleStatsUpdate)
}

func (s *SearchModuleStats) ResetCounters() {
	s.InputCount = 0
	s.OutputCount = 0
	s.InputBytes = 0
	s.OutputBytes = 0
}

func (s *SearchModuleStatsUpdate) Size() (sz int64) {
	if s == nil {
		return
	}
	sz = 16 //two int64s
	for i := range s.Stats {
		sz += s.Stats[i].Size()
	}
	return
}

func (s *SearchModuleStatsUpdate) Add(smsu *SearchModuleStatsUpdate) error {
	update := smsu.Stats
	if s.TS.IsZero() {
		s.TS = smsu.TS
	}
	if len(s.Stats) == 0 {
		//adding to a nil base just means we treat the update as the new golden copy
		s.Stats = make([]SearchModuleStats, len(update))
		copy(s.Stats, update)
		return nil
	}
	if len(s.Stats) != len(update) {
		return errors.New("stat length mismatch")
	}
	for i := 0; i < len(s.Stats); i++ {
		s.Stats[i].InputCount += update[i].InputCount
		s.Stats[i].OutputCount += update[i].OutputCount
		s.Stats[i].InputBytes += update[i].InputBytes
		s.Stats[i].OutputBytes += update[i].OutputBytes
		if update[i].Duration > s.Stats[i].Duration {
			s.Stats[i].Duration = update[i].Duration
		}
	}
	if s.TS.Before(smsu.TS) {
		s.TS = smsu.TS
	}
	return nil
}

func (s *SearchModuleStatsUpdate) AddUpdate(mu []ModuleStatsUpdate) error {
	if len(s.Stats) != len(mu) {
		return errors.New("stat length mismatch")
	}
	for i := 0; i < len(s.Stats); i++ {
		s.Stats[i].InputCount += mu[i].InputCount
		s.Stats[i].OutputCount += mu[i].OutputCount
		s.Stats[i].InputBytes += mu[i].InputBytes
		s.Stats[i].OutputBytes += mu[i].OutputBytes
		if mu[i].Duration > s.Stats[i].Duration {
			s.Stats[i].Duration = mu[i].Duration
		}
	}
	return nil
}

func (s *SearchModuleStatsUpdate) Append(sms SearchModuleStats) {
	s.Stats = append(s.Stats, sms)
}

func (s *SearchModuleStatsUpdate) IsNull() bool {
	if s == nil {
		return true
	}
	if s.Stats == nil || s.TS.IsZero() {
		return true
	}
	return false
}

func (s SearchModuleStatsUpdate) Copy() SearchModuleStatsUpdate {
	return SearchModuleStatsUpdate{
		TS:    s.TS,
		Stats: append([]SearchModuleStats{}, s.Stats...),
	}
}

func (s *SearchModuleStatsUpdate) ResetCounters() {
	for i := range s.Stats {
		s.Stats[i].ResetCounters()
	}
}

// CopyZero hands back a SearchModuleStatsUpdate structure zeroed out with ONLY the TS and the module names
// we use this for one-off search modules that relay stats in a strange manner due to condensing
func (s *SearchModuleStatsUpdate) CopyZero() SearchModuleStatsUpdate {
	st := make([]SearchModuleStats, len(s.Stats))
	for i, v := range s.Stats {
		st[i].Name = v.Name
		st[i].Args = v.Args
		st[i].Duration = v.Duration
		st[i].started = v.started
		st[i].stopped = v.stopped
	}
	return SearchModuleStatsUpdate{
		TS:    s.TS,
		Stats: st,
	}
}

func (m *SearchModuleStatsUpdate) MarshalJSON() ([]byte, error) {
	type alias SearchModuleStatsUpdate
	return json.Marshal(&struct {
		alias
		Stats sms
	}{
		alias: alias(*m),
		Stats: sms(m.Stats),
	})
}

func (m *StatSet) MarshalJSON() ([]byte, error) {
	type alias StatSet
	return json.Marshal(&struct {
		alias
		Stats sms
	}{
		alias: alias(*m),
		Stats: sms(m.Stats),
	})
}

func (m *IndexManagerStats) MarshalJSON() ([]byte, error) {
	type alias IndexManagerStats
	return json.Marshal(&struct {
		alias
		Stats ls
	}{
		alias: alias(*m),
		Stats: ls(m.Stats),
	})
}

func (m *IdxStats) MarshalJSON() ([]byte, error) {
	type alias IdxStats
	return json.Marshal(&struct {
		alias
		IndexStats is
	}{
		alias:      alias(*m),
		IndexStats: is(m.IndexStats),
	})
}

type is []IndexManagerStats

func (i is) MarshalJSON() ([]byte, error) {
	if len(i) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]IndexManagerStats(i))
}

type ls []IndexerStats

func (m ls) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]IndexerStats(m))
}

type sms []SearchModuleStats

func (m sms) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]SearchModuleStats(m))
}
