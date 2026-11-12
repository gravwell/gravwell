/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json/v2"
	"sort"
	"time"

	"github.com/google/uuid"
)

var (
	maxJsonTimestamp = time.Date(9999, time.December, 12, 23, 59, 59, 99, time.UTC)
)

type IndexerRequest struct {
	DialString string

	Forwarded bool // If set, do NOT propagate this message to other webservers
}

type ReplicationState struct {
	UUID    uuid.UUID
	Entries uint64
	Size    uint64
}

func (rs ReplicationState) IsZero() bool {
	return rs.Entries == 0 && rs.Size == 0 && rs.UUID == uuid.Nil
}

type ShardInfo struct {
	Name        string
	Start       time.Time
	End         time.Time
	Entries     uint64           //number of entries in the shard
	Size        uint64           //raw size of data in the shard
	Stored      uint64           //actual disk usage of the shard
	RemoteState ReplicationState `json:",omitzero"`
	Cold        bool             //true if the shard is in the code storage
	// a 0-100 value that indicates how fragmented a shard is, 0 is perfect 100 is really bad
	Fragmentation uint
}

// MarshalJSON ensures timestamps are clamped.
func (si ShardInfo) MarshalJSON() ([]byte, error) {
	type alias ShardInfo
	a := alias(si)
	if si.Start.After(maxJsonTimestamp) {
		a.Start = maxJsonTimestamp
	}
	if si.End.After(maxJsonTimestamp) {
		a.End = maxJsonTimestamp
	}

	return json.Marshal(a)
}

type WellInfo struct {
	ID          string // unique identifier constructed from the indexer UUID and the well name
	Name        string
	Tags        []string
	Shards      []ShardInfo
	Accelerator string `json:",omitempty"`
	Engine      string `json:",omitempty"`
	Path        string `json:",omitempty"` //hot storage location
	ColdPath    string `json:",omitempty"` //cold storage location
	// a 0-100 value that indicates how fragmented a shard is, 0 is perfect 100 is really bad
	// for the well this is the mean fragmentation of all shards in the well
	Fragmentation uint
}

func (wi *WellInfo) sort() {
	sort.SliceStable(wi.Shards, func(i, j int) bool {
		return wi.Shards[i].Start.Before(wi.Shards[j].Start)
	})
}

func (wi *WellInfo) Empty() bool {
	if wi == nil {
		return true
	}
	return wi.Name == `` && len(wi.Tags) == 0 && len(wi.Shards) == 0 && wi.Accelerator == `` && wi.Engine == ``
}

type StorageStats struct {
	CoverageStart    time.Time
	CoverageEnd      time.Time
	DataIngestedHot  uint64
	DataIngestedCold uint64
	DataStoredHot    uint64
	DataStoredCold   uint64
	EntryCountHot    uint64
	EntryCountCold   uint64
}

type PerWellStorageStats struct {
	StorageStats
	Accelerator    string
	Engine         string
	PathCold       string
	PathHot        string
	ShardCountCold uint64
	ShardCountHot  uint64
	Tags           []string
	WellName       string
	// a 0-100 value that indicates how fragmented a shard is, 0 is perfect 100 is really bad
	Fragmentation uint
}

type CalendarRequest struct {
	Start time.Time
	End   time.Time
	Wells []string
}

type CalendarEntry struct {
	Date         string
	DataIngested uint64
	EntryCount   uint64
}

type IndexerWellData struct {
	UUID  uuid.UUID
	Wells []WellInfo
	//Key is the UUID of the remote system that we have replicated data for
	//the value is the list of wells and their data
	Replicated map[uuid.UUID][]WellInfo
}

type SearchQueue struct {
	InFlight    int
	MaxInFlight int
	Enqueued    int
	MaxEnqueued int
}

func (iwd *IndexerWellData) Sort() {
	for i := range iwd.Wells {
		iwd.Wells[i].sort()
	}
	for _, v := range iwd.Replicated {
		for i := range v {
			v[i].sort()
		}
	}
}
