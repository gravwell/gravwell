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
	"hash/fnv"
	"io"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	// base universal requests
	REQ_GET_ENTRIES     uint32 = 0x10
	REQ_STREAMING       uint32 = 0x11
	REQ_TS_RANGE        uint32 = 0x12
	REQ_GET_RAW_ENTRIES uint32 = 0x13

	// data exploration requests
	REQ_GET_EXPLORE_ENTRIES uint32 = 0xf010
	REQ_EXPLORE_TS_RANGE    uint32 = 0xf012

	// Stats Requests
	REQ_STATS_SIZE         uint32 = 0x7F000001 //how many values are there
	REQ_STATS_RANGE        uint32 = 0x7F000002 //give first and last stats values
	REQ_STATS_GET          uint32 = 0x7F000003 //get all stats available given a count
	REQ_STATS_GET_RANGE    uint32 = 0x7F000004 //get all stats over a time range
	REQ_STATS_GET_SUMMARY  uint32 = 0x7F000005 //get a single  stats entry
	REQ_STATS_GET_LOCATION uint32 = 0x7F000006 //get the current location of the search
	REQ_STATS_GET_OVERVIEW uint32 = 0x7F000007 //Get just an array of entry counts and byte counts over a time range
	// Search Metadata requests
	REQ_SEARCH_METADATA uint32 = 0x10001

	// base universal responses
	RESP_GET_ENTRIES     uint32 = 0x10
	RESP_STREAMING       uint32 = 0x11
	RESP_TS_RANGE        uint32 = 0x12
	RESP_GET_RAW_ENTRIES uint32 = 0x13

	// data exploration responses
	RESP_GET_EXPLORE_ENTRIES uint32 = 0xf010
	RESP_EXPLORE_TS_RANGE    uint32 = 0xf012

	// Stats Responses
	RESP_STATS_SIZE         uint32 = 0x7F000001
	RESP_STATS_RANGE        uint32 = 0x7F000002
	RESP_STATS_GET          uint32 = 0x7F000003
	RESP_STATS_GET_RANGE    uint32 = 0x7F000004
	RESP_STATS_GET_SUMMARY  uint32 = 0x7F000005
	RESP_STATS_GET_LOCATION uint32 = 0x7F000006
	RESP_STATS_GET_OVERVIEW uint32 = 0x7F000007

	// Search Metadata responses
	RESP_SEARCH_METADATA uint32 = 0x10001

	STATS_MASK    uint32 = 0xFF000000
	STATS_MASK_ID uint32 = 0x7F000000
)

const (
	DownloadJSON       string = `json`       //encode as JSON
	DownloadCSV        string = `csv`        //standard CSV file
	DownloadText       string = `text`       //just text...
	DownloadPCAP       string = `pcap`       //format as a full blown PCAP-NG file
	DownloadLookupData string = `lookupdata` //GOB encoded table that can be passed back to the "lookup" module
	DownloadIPExists   string = `ipexist`    //IPExist encoded bitblock (https://github.com/gravwell/ipexist)
	DownloadArchive    string = `archive`    //a reimportable archive that is the complete renderer dataset

	DownloadDataTypeString string = `string`
	DownloadDataTypeSlice  string = `slice`
	DownloadDataTypeIP     string = `IP`
	DownloadDataTypeEV     string = `EV`

	RenderNameRaw        string = `raw`
	RenderNameHex        string = `hex`
	RenderNameText       string = `text`
	RenderNamePcap       string = `pcap`
	RenderNameTable      string = `table`
	RenderNameGauge      string = `gauge`
	RenderNameNumbercard string = `numbercard`
	RenderNameChart      string = `chart`
	RenderNameFdg        string = `fdg`
	RenderNameStackGraph string = `stackgraph`
	RenderNamePointmap   string = `pointmap`
	RenderNameHeatmap    string = `heatmap`
	RenderNameP2P        string = `point2point`

	MetadataTypeRaw    string = `raw`
	MetadataTypeNumber string = `number`
)

type TimeRange struct {
	StartTS entry.Timestamp `json:",omitempty"`
	EndTS   entry.Timestamp `json:",omitempty"`
}

type EntryRange struct {
	StartTS entry.Timestamp `json:",omitempty"`
	EndTS   entry.Timestamp `json:",omitempty"`
	First   uint64
	Last    uint64
}

// BaseRequest contains elements common to all renderer requests.
type BaseRequest struct {
	ID         uint32
	Stats      *SearchStatsRequest `json:",omitempty"`
	EntryRange *EntryRange         `json:",omitempty"`
	Addendum   json.RawMessage     `json:",omitempty"`
}

// BaseResponse contains elements common to all renderer request responses.
type BaseResponse struct {
	ID         uint32
	Stats      *SearchStatsResponse      `json:",omitempty"`
	Addendum   json.RawMessage           `json:",omitempty"`
	SearchInfo *SearchInfo               `json:",omitempty"`
	EntryRange *EntryRange               `json:",omitempty"`
	Metadata   *SearchMetadata           `json:",omitempty"`
	Tags       map[string]entry.EntryTag `json:",omitempty"`
	Error      string                    `json:",omitempty"`

	// Finished is true when the query has completed.
	Finished bool

	// EntryCount is the number of entries which *entered* the renderer.
	EntryCount uint64
	// For some renderers, the EntryCount accurately represents the total
	// number of results available. This field is set to 'true' in that case,
	// meaning the EntryCount number can be displayed alongside the results
	// without confusion.
	EntryCountValid bool

	// If set, there are more entries for the given timeframe available.
	// For non-condensing this means EntryCount > request.Last
	// For condensing, this means that given the range, there are values
	// available after the Last range.
	AdditionalEntries bool

	// Indicates that the query results exceeded the on-disk storage limits.
	OverLimit bool
	// Indicates the range of entries that were dropped due to storage limits.
	LimitDroppedRange TimeRange
}

func (br BaseResponse) Err() error {
	if br.Error != `` {
		return errors.New(br.Error)
	}
	return nil
}

// We have a generic StatsRequest type that ONLY implements the BaseRequest.
// This is so that clients can ask about stats without knowing about specific
// renderers.
type StatsRequest struct {
	BaseRequest
}

type StatsResponse struct {
	BaseResponse
}

// IndexerPingResponse contains a map of states for all configured indexers.
type IndexerPingResponse struct {
	Error  string            `json:",omitempty"`
	States map[string]string `json:",omitempty"`
}

// SysDescResp contains a map of SysInfo (used in the System Overview) objects
// for each connected system in a Gravwell deployment.
type SysDescResp struct {
	Error        string             `json:",omitempty"`
	Descriptions map[string]SysInfo `json:",omitempty"`
}

type SysStats struct {
	Error string        `json:",omitempty"`
	Stats *HostSysStats `json:",omitempty"`
}

type SysStatResponse struct {
	Error string              `json:",omitempty"`
	Stats map[string]SysStats `json:",omitempty"`
}

type IndexerStats struct {
	UUID        uuid.UUID // unique well ID based on the indexer UUID and the well ID assigned at indexer startup
	Data        uint64
	Entries     uint64
	Path        string
	Cold        bool
	Accelerator string `json:",omitempty"`
	Extractor   string `json:",omitempty"`
}

type IndexManagerStats struct {
	Name  string
	Stats []IndexerStats
}

type IdxStats struct {
	UUID       uuid.UUID
	Error      string              `json:",omitempty"`
	IndexStats []IndexManagerStats `json:",omitempty"`
}

type IdxStatResponse struct {
	Error string              `json:",omitempty"`
	Stats map[string]IdxStats `json:",omitempty"`
}

type IngestStats struct {
	QuotaUsed    uint64 // Quota used so far
	QuotaMax     uint64 // Total quota
	TotalCount   uint64 //Total Entries since the ingest server started
	TotalSize    uint64 //Total Data since the ingest server started
	LastDayCount uint64 //total entries in last 24 hours
	LastDaySize  uint64 //total ingested in last 24 hours
	Ingesters    []IngesterStats
	Missing      []ingest.IngesterState //ingesters that have been seen before but not actively connected now
}

type IngesterStats struct {
	RemoteAddress string
	Count         uint64
	Size          uint64
	Uptime        time.Duration
	Tags          []string
	Name          string
	Version       string
	UUID          string
	State         ingest.IngesterState
}

type IngesterStatsResponse struct {
	Error string                 `json:",omitempty"`
	Stats map[string]IngestStats `json:",omitempty"`
}

type SearchStatsRequest struct {
	SetCount int64           `json:",omitempty"`
	SetStart entry.Timestamp `json:",omitempty"`
	SetEnd   entry.Timestamp `json:",omitempty"`
	Addendum json.RawMessage `json:",omitempty"`
}

type SearchStatsResponse struct {
	Addendum    json.RawMessage   `json:",omitempty"`
	RangeStart  entry.Timestamp   `json:",omitempty"`
	RangeEnd    entry.Timestamp   `json:",omitempty"`
	Current     entry.Timestamp   `json:",omitempty"`
	Set         []StatSet         `json:",omitempty"`
	OverviewSet []OverviewStatSet `json:",omitempty"`
	Size        int               `json:",omitempty"`
}

type StatSet struct {
	Stats     []SearchModuleStats
	TS        entry.Timestamp
	populated bool
}

type OverviewStatSet struct {
	Count uint64
	Bytes uint64
	TS    entry.Timestamp
}

type OverviewStats struct {
	//indicates where in the search span the query currently is, can be used for progress
	SearchPosition entry.Timestamp
	//indicates if the search is finished
	Finished bool
	// Indicates that the query results exceeded the on-disk storage limits.
	OverLimit bool
	// Indicates the range of entries that were dropped due to storage limits.
	LimitDroppedRange TimeRange
	// For some renderers, the EntryCount accurately represents the total
	// number of results available. This field is set to 'true' in that case,
	// meaning the EntryCount number can be displayed alongside the results
	// without confusion.
	EntryCountValid bool
	Stats           []OverviewStatSet `json:",omitempty"`
}

type SearchMetadataNumber struct {
	Count uint
	Min   float64
	Max   float64
}

type SearchMetadataRaw struct {
	Map   map[string]uint
	Other uint
}

type SearchMetadataEntry struct {
	Name   string
	Type   string
	Number SearchMetadataNumber `json:",omitempty"`
	Raw    SearchMetadataRaw    `json:",omitempty"`
}

type SourceMetadataEntry struct {
	IP    string
	Count uint
}

type SearchMetadata struct {
	ValueStats  []SearchMetadataEntry `json:",omitempty"`
	SourceStats []SourceMetadataEntry `json:",omitempty"`
	TagStats    map[string]uint       `json:",omitempty"`
}

func (ss *StatSet) AddParts(ts entry.Timestamp, stats []SearchModuleStats) {
	if !ss.populated {
		if ss.TS.IsZero() {
			ss.TS = ts
		}
		if len(stats) > 0 {
			ss.Stats = make([]SearchModuleStats, len(stats))
		}
		for i := range stats {
			ss.Stats[i].Name = stats[i].Name
			ss.Stats[i].Args = stats[i].Args
		}
	}
	ss.populated = true
	if len(ss.Stats) != len(stats) {
		return
	}
	for i := range ss.Stats {
		ss.Stats[i].InputCount += stats[i].InputCount
		ss.Stats[i].OutputCount += stats[i].OutputCount
		ss.Stats[i].InputBytes += stats[i].InputBytes
		ss.Stats[i].OutputBytes += stats[i].OutputBytes
		if stats[i].Duration > ss.Stats[i].Duration {
			ss.Stats[i].Duration = stats[i].Duration
		}
	}
}

func (ss *StatSet) Add(s *StatSet) {
	ss.AddParts(s.TS, s.Stats)
}

func (ss *StatSet) AddStats(stats []SearchModuleStats) {
	ss.AddParts(ss.TS, stats)
}

func (ss *StatSet) Populated() bool {
	return ss.populated
}

func (tr TimeRange) IsEmpty() bool {
	return (tr.StartTS.IsZero() && tr.EndTS.IsZero())
}

func (tr *TimeRange) RoundToSecond() {
	tr.StartTS.Nsec = 0
	tr.EndTS.Nsec = 0
}

func (tr *TimeRange) Swap() {
	tr.StartTS, tr.EndTS = tr.EndTS, tr.StartTS
}

func (tr *TimeRange) DecodeJSON(r io.Reader) error {
	if err := json.NewDecoder(r).Decode(tr); err != nil {
		if err == io.EOF {
			tr.StartTS = entry.Timestamp{}
			tr.EndTS = entry.Timestamp{}
			return nil
		}
		return err
	}
	return nil
}

func (is IngesterStats) Hash() uint64 {
	sort.SliceStable(is.Tags, func(i, j int) bool {
		return is.Tags[i] < is.Tags[j]
	})
	n := fnv.New64()
	for i := range is.Tags {
		io.WriteString(n, is.Tags[i])
	}
	io.WriteString(n, is.RemoteAddress)
	return n.Sum64()
}

func (is IngesterStats) MarshalJSON() ([]byte, error) {
	type alias IngesterStats
	return json.Marshal(&struct {
		alias
		Tags emptyStrings
	}{
		alias: alias(is),
		Tags:  emptyStrings(is.Tags),
	})
}

func UniqueIngesters(sts []IngestStats) (r uint64) {
	mp := map[uint64]bool{}
	for _, st := range sts {
		for _, ig := range st.Ingesters {
			hsh := ig.Hash()
			if _, ok := mp[hsh]; !ok {
				r++
				mp[hsh] = true
			}
		}
	}
	return
}

func (m *RenderModuleInfo) MarshalJSON() ([]byte, error) {
	type alias RenderModuleInfo
	return json.Marshal(&struct {
		alias
		Examples emptyStrings
	}{
		alias:    alias(*m),
		Examples: emptyStrings(m.Examples),
	})
}

type emptyEntries []SearchEntry

func (ee emptyEntries) MarshalJSON() ([]byte, error) {
	if len(ee) == 0 {
		return emptyList, nil
	}
	return json.Marshal(([]SearchEntry)(ee))
}

type emptyIngesterStats []IngesterStats

func (eis emptyIngesterStats) MarshalJSON() ([]byte, error) {
	if len(eis) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]IngesterStats(eis))
}

type emptyIngesterStates []ingest.IngesterState

func (eis emptyIngesterStates) MarshalJSON() ([]byte, error) {
	if len(eis) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]ingest.IngesterState(eis))
}

func (is IngestStats) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		QuotaUsed  uint64
		QuotaMax   uint64
		TotalCount uint64
		TotalSize  uint64
		Ingesters  emptyIngesterStats
		Missing    emptyIngesterStates
	}{
		QuotaUsed:  is.QuotaUsed,
		QuotaMax:   is.QuotaMax,
		TotalCount: is.TotalCount,
		TotalSize:  is.TotalSize,
		Ingesters:  emptyIngesterStats(is.Ingesters),
		Missing:    emptyIngesterStates(is.Missing),
	})
}

func (rr RawResponse) MarshalJSON() ([]byte, error) {
	type alias RawResponse
	return json.Marshal(&struct {
		alias
		Entries emptyEntries
	}{
		alias:   alias(rr),
		Entries: emptyEntries(rr.Entries),
	})
}

func (tr *TimeRange) UnmarshalJSON(d []byte) error {
	if len(d) == 0 {
		return nil
	}
	type alias TimeRange
	var a alias
	if err := json.Unmarshal(d, &a); err != nil {
		return err
	}
	tr.StartTS = a.StartTS
	tr.EndTS = a.EndTS
	return nil
}

func (ssr SearchStatsRequest) MarshalJSON() ([]byte, error) {
	type alias SearchStatsRequest
	return json.Marshal(&struct {
		alias
		SetStart *entry.Timestamp `json:",omitempty"`
		SetEnd   *entry.Timestamp `json:",omitempty"`
	}{
		alias:    alias(ssr),
		SetStart: tsPointer(ssr.SetStart),
		SetEnd:   tsPointer(ssr.SetEnd),
	})
}

func tsPointer(t entry.Timestamp) *entry.Timestamp {
	if t.IsZero() {
		return nil
	}
	return &t
}
