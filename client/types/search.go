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
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	// Universal requests
	REQ_CLOSE          uint32 = 0x1
	REQ_ENTRY_COUNT    uint32 = 0x3
	REQ_SEARCH_DETAILS uint32 = 0x4
	REQ_SEARCH_TAGS    uint32 = 0x5

	// Universal responses
	RESP_ERROR          uint32 = 0xFFFFFFFF
	RESP_CLOSE          uint32 = 0x1
	RESP_ENTRY_COUNT    uint32 = 0x3
	RESP_SEARCH_DETAILS uint32 = 0x4
	RESP_SEARCH_TAGS    uint32 = 0x5
)

var (
	ErrIllegalMacroCharacter error = errors.New("Illegal macro name character")
)

// An Element is an item which has been extracted from an entry using the
// data exploration system.
type Element struct {
	Name        string
	Path        string
	Value       interface{}
	SubElements []Element `json:",omitempty"`
	Filters     []string
}

// A GenerateAXRequest contains a tag name and a set of entries.
// It is used by clients to request all possible extractions from the given entries.
// All entries should have the same tag.
type GenerateAXRequest struct {
	Tag     string
	Entries []SearchEntry
}

// A GenerateAXResponse contains an autoextractor definition
// and corresponding Element extractions as gathered from a single extraction module
type GenerateAXResponse struct {
	Extractor AXDefinition
	// Confidence is a range from 0 to 10, with 10 meaning "we are very confident"
	// and 0 meaning "we didn't extract anything of worth".
	// Some modules, like xml, will return values lower than 10 even if they extracted
	// lots of data, because other modules like winlog should take precedence if they
	// succeed.
	Confidence float64
	Entries    []SearchEntry
	Explore    []ExploreResult
}

type ExploreResult struct {
	Elements []Element
	Module   string
	Tag      string
}

type PingReq struct {
	X error `json:",omitempty"`
}

// ModuleHint contain "hints" about modules, populated during the init and
// parse phase. Hints contain information such as what enumerated values are
// used and produced and what resources are needed by a given module.
type ModuleHint struct {
	Name            string
	ProducedEVs     []string
	ConsumedEVs     []string
	ResourcesNeeded []string
	Condensing      bool
}

type SearchHints struct {
	CollapsingIndex  int      // index of the first collapsed module
	RenderModule     string   `json:",omitempty"`
	TimeZoomDisabled bool     //Renderer does not support zooming around data based on time
	Tags             []string //the tags involved in the search
	ModuleHints      []ModuleHint
}

type FilterRequest struct {
	Tag    string
	Module string
	Path   string
	Op     string
	Value  string
}

type ParseSearchRequest struct {
	SearchString string
	Sequence     uint64
	Filters      []FilterRequest
}

type ParseSearchResponse struct {
	Sequence    uint64
	GoodQuery   bool
	ParseError  string `json:",omitempty"`
	ParsedQuery string `json:",omitempty"`
	RawQuery    string `json:",omitempty"`
	ModuleIndex int
	SearchHints
}

// StartSearchRequest represents a search that is sent to the search controller
// in the webserver.
type StartSearchRequest struct {
	SearchString string
	SearchStart  string
	SearchEnd    string
	Background   bool
	NoHistory    bool `json:",omitempty"`
	//Preview indicates that the renderer should only capture enough to show some usage of data
	//A raw, text, hex renderer will grab a few hundred or thousand entries
	//charts will grab enough to draw something useful
	//everything else will get "enough"
	Preview  bool            `json:",omitempty"`
	Metadata json.RawMessage `json:",omitempty"`
	Addendum json.RawMessage `json:",omitempty"`
	Name     string          `json:",omitempty"`

	Filters []FilterRequest
}

// The webserver responds yay/nay plus new subprotocols if the search is valid.
// SearchStartRange and SearchEndRange should be strings in RFC3339Nano format
type StartSearchResponse struct {
	Error string `json:",omitempty"`
	// what the user typed
	RawQuery string `json:",omitempty"`
	//what the actual search being processed is after attaching render module
	SearchString         string          `json:",omitempty"`
	RenderModule         string          `json:",omitempty"`
	RenderCmd            string          `json:",omitempty"`
	OutputSearchSubproto string          `json:",omitempty"`
	SearchID             string          `json:",omitempty"`
	SearchStartRange     time.Time       `json:",omitempty"`
	SearchEndRange       time.Time       `json:",omitempty"`
	Background           bool            `json:",omitempty"`
	CollapsingIndex      int             // index of the first collapsed module
	Metadata             json.RawMessage `json:",omitempty"`
	Addendum             json.RawMessage `json:",omitempty"`
	SearchHints
}

// Once a search has begin, an ACK is sent.
type StartSearchAck struct {
	Ok                   bool
	OutputSearchSubproto string `json:",omitempty"`
	OutputStatsSubproto  string `json:",omitempty"`
}

// Request to reattach to a search.
type AttachSearchRequest struct {
	ID string
}

// AttachSearchResponse contains the subproto and SearchInfo object when
// attaching to a search.
type AttachSearchResponse struct {
	Error       string      `json:",omitempty"` //error if not
	Subproto    string      `json:",omitempty"` //the new subprotocol
	RendererMod string      `json:",omitempty"` //the renderer in use
	RendererCmd string      `json:",omitempty"` //the renderer commands
	Info        *SearchInfo `json:",omitempty"` //info if available
}

// SearchInfo contains information about a search, including the search
// parameters, status, and metadata.
type SearchInfo struct {
	ID                    string          //ID of the search
	UID                   int32           //UID of the user that actually kicked off the search
	GID                   int32           `json:",omitempty"` //Group ID the search was assigned to
	UserQuery             string          //query provided by the user on search
	EffectiveQuery        string          //the effective query that was actually used
	StartRange            time.Time       //start time range
	EndRange              time.Time       //end time range
	Descending            bool            //the direction the search is progressing (Descending is the standard)
	Started               time.Time       //time when the search was kicked off
	LastUpdate            time.Time       //last timestamp we saw (tells us where indexers are working)
	Duration              time.Duration   //Amount of time required to complete the search
	StoreSize             int64           //size of the main storage file
	IndexSize             int64           //size of an extra index file
	ItemCount             int64           //How many items have been stored
	TimeZoomDisabled      bool            //Renderer does not support zooming around data based on time
	RenderDownloadFormats []string        `json:",omitempty"`
	Metadata              json.RawMessage `json:",omitempty"` //additional metadata associated with a search
	Name                  string          `json:",omitempty"`
	CollapsingIndex       int
	NoHistory             bool // set to true if this search was launched with the "no history" flag, typically means it is an automated search.
	MinZoomWindow         uint // what is the smallest minimum zoom window in seconds
	Tags                  []string
	Import                ImportInfo `json:",omitempty"` //information attached if there this search is saved and from an external import
	// Preview indicates that this search is a preview search
	// this means that the query most likely did not cover the entire time range that was originally requested
	// A preview search is used when a user is trying to understand what they have or establish AX relationships
	Preview bool
}

type ImportInfo struct {
	Imported  bool
	Time      time.Time //timestamp of when the results were imported
	BatchName string    //potential import batch name
	BatchInfo string    //potential import batch notes

}

type StatsUpdate struct {
	Stats    *SearchModuleStatsUpdate
	ClientID string
}

type SearchCtrlStatus struct {
	ID              string
	UID             int32
	GID             int32
	State           string
	AttachedClients int
	StoredData      int64
}

func (si SearchInfo) StorageSize() int64 {
	return si.StoreSize + si.IndexSize
}

const AllowedMacroChars = "ABCDCEFGHIJKLMNOPQRSTUVWXYZ1234567890_-"

type SearchMacro struct {
	ID          uint64
	UID         int32
	GIDs        []int32
	Global      bool
	Name        string
	Description string
	Expansion   string
	Labels      []string
	LastUpdated time.Time
	Synced      bool
}

func CheckMacroName(name string) error {
	for _, char := range name {
		if !strings.Contains(AllowedMacroChars, string(char)) {
			return fmt.Errorf("%w: %v", ErrIllegalMacroCharacter, char)
		}
	}
	return nil
}

//custom Marshallers
func (si SearchInfo) MarshalJSON() ([]byte, error) {
	type alias SearchInfo
	return json.Marshal(struct {
		alias
		Duration string `json:",omitempty"`
	}{
		alias:    alias(si),
		Duration: si.Duration.String(),
	})
}

func (si *SearchInfo) UnmarshalJSON(data []byte) error {
	type aalias SearchInfo
	type alias struct {
		aalias
		Duration string `json:",omitempty"`
	}
	var v alias
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if len(v.Duration) > 0 {
		dur, err := time.ParseDuration(v.Duration)
		if err != nil {
			return err
		}
		si.Duration = dur
	}
	*si = SearchInfo(v.aalias)
	return nil
}

type emptyStatSet []StatSet

func (ess emptyStatSet) MarshalJSON() ([]byte, error) {
	if len(ess) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]StatSet(ess))
}

func (ssr SearchStatsResponse) MarshalJSON() ([]byte, error) {
	type alias SearchStatsResponse
	return json.Marshal(&struct {
		Size       int
		Set        emptyStatSet
		RangeStart *entry.Timestamp `json:",omitempty"`
		RangeEnd   *entry.Timestamp `json:",omitempty"`
		Current    *entry.Timestamp `json:",omitempty"`
	}{
		Size:       ssr.Size,
		Set:        emptyStatSet(ssr.Set),
		RangeStart: tsPointer(ssr.RangeStart),
		RangeEnd:   tsPointer(ssr.RangeEnd),
		Current:    tsPointer(ssr.Current),
	})
}
