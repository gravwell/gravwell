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
	"sort"
	"time"

	"github.com/google/uuid"
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

type ShardInfo struct {
	Name        string
	Start       time.Time
	End         time.Time
	Entries     uint64           //number of entries in the shard
	Size        uint64           //raw size of data in the shard
	Stored      uint64           //actual disk usage of the shard
	RemoteState ReplicationState `json:",omitempty"`
	Cold        bool             //true if the shard is in the code storage
}

// custom marshaller to deal with the fact that the json marshaller can't handle the "empty" uuid value
func (si ShardInfo) MarshalJSON() ([]byte, error) {
	x := struct {
		Name        string
		Start       time.Time
		End         time.Time
		Entries     uint64
		Size        uint64
		Stored      uint64
		Cold        bool              //true if the shard is in the code storage
		RemoteState *ReplicationState `json:",omitempty"`
	}{
		Name:    si.Name,
		Start:   si.Start,
		End:     si.End,
		Entries: si.Entries,
		Size:    si.Size,
		Stored:  si.Stored,
		Cold:    si.Cold,
	}
	if !si.RemoteState.isEmpty() {
		x.RemoteState = &si.RemoteState
	}
	return json.Marshal(x)
}

type WellInfo struct {
	UUID        uuid.UUID // unique identifier constructed from the indexer UUID and the well ID
	Name        string
	Tags        []string
	Shards      []ShardInfo
	Accelerator string `json:",omitempty"`
	Engine      string `json:",omitempty"`
	Path        string `json:",omitempty"` //hot storage location
	ColdPath    string `json:",omitempty"` //cold storage location
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

type IndexerWellData struct {
	UUID  uuid.UUID
	Wells []WellInfo
	//Key is the UUID of the remote system that we have replicated data for
	//the value is the list of wells and their data
	Replicated map[uuid.UUID][]WellInfo
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

func (v IndexerWellData) MarshalJSON() ([]byte, error) {
	x := struct {
		UUID       uuid.UUID
		Wells      emptyWellList
		Replicated erp
	}{
		UUID:       v.UUID,
		Wells:      emptyWellList(v.Wells),
		Replicated: erp(v.Replicated),
	}

	return json.Marshal(x)
}

type erp map[uuid.UUID][]WellInfo

func (v erp) MarshalJSON() ([]byte, error) {
	if len(v) == 0 {
		return emptyObj, nil
	}
	return json.Marshal(map[uuid.UUID][]WellInfo(v))
}

type eshardList []ShardInfo

func (el eshardList) MarshalJSON() ([]byte, error) {
	if len(el) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]ShardInfo(el))
}

func (wi WellInfo) MarshalJSON() ([]byte, error) {
	type alias WellInfo
	ts := struct {
		alias
		Tags   emptyStrings
		Shards eshardList
	}{
		alias:  alias(wi),
		Tags:   emptyStrings(wi.Tags),
		Shards: eshardList(wi.Shards),
	}
	return json.Marshal(ts)
}

type emptyWellList []WellInfo

func (e emptyWellList) MarshalJSON() ([]byte, error) {
	if len(e) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]WellInfo(e))
}

func (rs ReplicationState) isEmpty() bool {
	return rs.Entries == 0 && rs.Size == 0 && rs.UUID == uuid.Nil
}
