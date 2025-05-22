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

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/ingest/entry"
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

// Search Launch Methods
const (
	LaunchMethodManual          = `manual`
	LaunchMethodDirectQuery     = `directquery`
	LaunchMethodFlow            = `flow`
	LaunchMethodScript          = `script`
	LaunchMethodScheduledSearch = `scheduledSearch`
	LaunchMethodDashboard       = `dashboard`
)

var (
	ErrIllegalMacroCharacter error = errors.New("Illegal character in macro name")
)

// An Element is an item which has been extracted from an entry using the
// data exploration system.
type Element struct {
	Module      string
	Args        string `json:",omitempty"`
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

// ExploreRequest is used to request that the webserver perform a complete cracking of
// all entries in the given range, the webserver will return an array of ExploreResult
type ExploreRequest struct {
	Start int64
	End   int64
}

type ExploreResult struct {
	Elements []Element `json:",omitempty"`
	// This represents the module which generated the result, but
	// individual Elements may have a different module set for
	// purposes of filtering.
	Module      string
	Tag         string
	WordOffsets []WordOffset `json:",omitempty"`
}

// A WordOffset contains two byte indexes into a string, denoting
// the location of a "word" within that string. The usual substring
// convention is followed, so the WordOffset of "foo" in "foo bar"
// is WordOffset{0, 3}, or in standard notation [0, 3).
type WordOffset [2]int

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
	Args   string `json:",omitempty"`
	Path   string // The path to extract
	Name   string // The desired output name
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

// LaunchRequest is a new named type so that we can abstract away the websocket launch requests from the REST requests
type LaunchRequest struct {
	StartSearchRequest
}

// LaunchResponse is used to respond to both Launch and Attach requests
// the type returns metadata about the search as well as
// this contains all the embedded
type LaunchResponse struct {
	SearchSessionID uuid.UUID `json:",omitempty"`
	// RefreshInterval is used to convey and optionally update the minimum interval
	// required in between touching a search session.  This value defines how often a client
	// must refresh thier search session before a search may be expired due to inactivity
	RefreshInterval uint //refresh interval in seconds

	// unified info that is always needed
	SearchID     string     `json:",omitempty"`
	RenderModule string     `json:",omitempty"`
	RenderCmd    string     `json:",omitempty"`
	Info         SearchInfo `json:",omitempty"`
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
	Preview bool `json:",omitempty"`
	//NonTemporal is used to hint that we do not want this query to be temporal IF POSSIBLE
	//some queries cannot respect this, but things like table and some charts can
	NonTemporal bool            `json:",omitempty"`
	Metadata    json.RawMessage `json:",omitempty"`
	Addendum    json.RawMessage `json:",omitempty"`
	Name        string          `json:",omitempty"`
	Filters     []FilterRequest
	LaunchInfo  SearchLaunchInfo // information about how a search was launched
	// Sharing parameters
	GIDs   []int32
	Global bool
}

// The webserver responds yay/nay plus new subprotocols if the search is valid.
// SearchStartRange and SearchEndRange should be strings in RFC3339Nano format
type StartSearchResponse struct {
	Error string `json:",omitempty"`
	// what the user typed
	RawQuery string `json:",omitempty"`
	//what the actual search being processed is after attaching render module
	SearchString         string           `json:",omitempty"`
	RenderModule         string           `json:",omitempty"`
	RenderCmd            string           `json:",omitempty"`
	OutputSearchSubproto string           `json:",omitempty"`
	SearchID             string           `json:",omitempty"`
	SearchStartRange     time.Time        `json:",omitempty"`
	SearchEndRange       time.Time        `json:",omitempty"`
	Background           bool             `json:",omitempty"`
	NonTemporal          bool             `json:",omitempty"`
	CollapsingIndex      int              // index of the first collapsed module
	Metadata             json.RawMessage  `json:",omitempty"`
	Addendum             json.RawMessage  `json:",omitempty"`
	LaunchInfo           SearchLaunchInfo // information about how a search was launched
	QueryTimeSpecified   bool             `json:",omitempty"` // True if the query itself specifies the time spec
	SearchHints
	// Sharing parameters
	GIDs   []int32
	Global bool
}

type SearchSessionIntervalUpdate struct {
	Interval uint
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
	ID                    string //ID of the search
	UID                   int32  //UID of the user that actually kicked off the search
	GID                   int32  `json:",omitempty"` //Group ID the search was assigned to, deprecated, use GIDs instead
	GIDs                  []int32
	Global                bool
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
	QueryTimeSpecified    bool            // True if the query contains start/end constraints
	RenderDownloadFormats []string        `json:",omitempty"`
	Metadata              json.RawMessage `json:",omitempty"` //additional metadata associated with a search
	Name                  string          `json:",omitempty"`
	CollapsingIndex       int
	NoHistory             bool // set to true if this search was launched with the "no history" flag, typically means it is an automated search.
	Background            bool // set to true if this search has been marked as backgrounded.
	MinZoomWindow         uint // what is the smallest minimum zoom window in seconds
	Tags                  []string
	Import                ImportInfo `json:",omitempty"` //information attached if there this search is saved and from an external import
	// Preview indicates that this search is a preview search
	// this means that the query most likely did not cover the entire time range that was originally requested
	// A preview search is used when a user is trying to understand what they have or establish AX relationships
	Preview bool
	// Error is set if the search ended in the ERROR state.
	Error string `json:",omitempty"`

	LaunchInfo SearchLaunchInfo // information about how a search was launched
}

type SearchLaunchInfo struct {
	//what launched the search, manual, directquery, scheduledsearch, etc...
	Method string `json:"method,omitempty"`

	// Reference is the UUID, ID, etc. of the thing that launched the search
	// this is blank for manual queries
	Reference string `json:"reference,omitempty"`

	// Started is the timestamp of when the search was started.  This is used to inform
	// the GUI and/or clients on when the query was actually started.
	Started time.Time `json:"started,omitempty"`

	// Expires marks when when the search should expire/be deleted,
	// it may be the zero value which means never
	Expires time.Time `json:"expires,omitempty"`
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
	GID             int32 // deprecated, use GIDs instead
	GIDs            []int32
	Global          bool
	State           SearchState
	AttachedClients int
	StoredData      int64
	UserQuery       string
	EffectiveQuery  string
	StartRange      time.Time
	EndRange        time.Time
	NoHistory       bool
	Import          ImportInfo
	LaunchInfo      SearchLaunchInfo
	Error           string `json:",omitempty"`
}

type SearchState struct {
	Attached     bool         `json:"attached"`
	Backgrounded bool         `json:"backgrounded"`
	Saved        bool         `json:"saved"`
	Streaming    bool         `json:"streaming"`
	Status       SearchStatus `json:"status"`
}

// String just implements a basic stringer on this type for some of the more simple CLI tooling
func (ss SearchState) String() (r string) {
	r = string(ss.Status)
	if ss.Streaming {
		r = r + "/streaming"
	}
	if ss.Saved {
		r = r + "/saved"
	}
	if ss.Backgrounded {
		r = r + "/backgrounded"
	}
	if ss.Attached {
		r = r + "/attached"
	}
	return
}

type SearchStatus string

const (
	SearchStatusError     SearchStatus = `error`
	SearchStatusCompleted SearchStatus = `completed`
	SearchStatusRunning   SearchStatus = `running`
	SearchStatusPending   SearchStatus = `pending`
)

func (si SearchInfo) StorageSize() int64 {
	return si.StoreSize + si.IndexSize
}

const AllowedMacroChars = "ABCDCEFGHIJKLMNOPQRSTUVWXYZ1234567890_-"

type SearchMacro struct {
	ID          uint64
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
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
			return fmt.Errorf("%w: %c (%U)", ErrIllegalMacroCharacter, char, char)
		}
	}
	return nil
}

// custom Marshallers
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

type SaveSearchPatch struct {
	SearchLaunchInfo
	// these are the supported fields in the free form search metadata; these are used by the GUI
	Name  string `json:"name,omitempty"`
	Notes string `json:"notes,omitempty"`
}

func (p SaveSearchPatch) GetMetadata() json.RawMessage {
	if p.Name == `` && p.Notes == `` {
		return nil
	}
	md := struct {
		Name  string `json:"name,omitempty"`
		Notes string `json:"notes,omitempty"`
	}{
		Name:  p.Name,
		Notes: p.Notes,
	}
	if v, err := json.Marshal(md); err == nil && len(v) > 0 {
		return json.RawMessage(v)
	}
	return nil
}

func (p SaveSearchPatch) MergeLaunchInfo(li *SearchLaunchInfo) (changed bool) {
	if li == nil {
		return
	}
	if p.Method != `` {
		li.Method = p.Method
		changed = true
	}
	if p.Reference != `` {
		li.Reference = p.Reference
		changed = true
	}
	// the expiration is special, we ALWAYS take the experation, even if its the zero value.
	// We do this so that a human clicking save on the GUI ALWAYS blows the search expiration.
	// The use case is that a scheduled search gets results and fires an alert, the alert logic
	// this marks the search as saved and sets an expiration.  A user gets the alert, clicks the search,
	// sees that the results should be kept and clicks save; this action means the user wants to kill
	// the timer, so we wipe the expiration.
	changed = !li.Expires.Equal(p.Expires)
	li.Expires = p.Expires

	//Started is best effort, take whatever isn't zero if there is one
	if li.Started.IsZero() {
		li.Started = p.Started
	}
	return
}
