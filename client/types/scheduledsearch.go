/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ScriptAnko ScriptLang = 0 // default is anko
	ScriptGo   ScriptLang = 1 // new hotness is go

	ScheduledTypeSearch string = "search"
	ScheduledTypeScript string = "script"
	ScheduledTypeFlow   string = "flow"

	SEQ_NODE_NOT_EXECUTED = 9999999
)

type ScriptLang uint

type ScriptDeployConfig struct {
	Disabled       bool
	RunImmediately bool
}

var (
	ErrUnknownScriptLanguage = errors.New("Unknown script language")
)

type ScheduledError struct {
	Error     string
	Timestamp time.Time
}

type FlowNodeResult struct {
	Payload map[string]interface{}
	ID      int    // the node ID
	Type    string // the type of node, e.g. RunQuery
	Log     string
	Error   string
	Start   int64 // unix nanoseconds
	End     int64 // unix nanoseconds
	// The first node executed has sequence 0, the next is sequence 1, etc.
	// Nodes which were not executed have Sequence = SEQ_NODE_NOT_EXECUTED
	Sequence int
}

type ScheduledScriptParseRequest struct {
	Version ScriptLang
	Script  string
}

type ScheduledScriptParseResponse struct {
	OK          bool
	Error       string `json:",omitempty"`
	ErrorLine   int
	ErrorColumn int
}

type FlowParseRequest struct {
	Flow       string
	DebugEvent *Event // If provided, this will be set as `event` in the flow payload for parsing.
}

type FlowParseResponse struct {
	OK bool

	// Error and ErrorNode are now deprecated; look at the Failures map
	// to see if there were parse problems. They are retained for compatibility.
	Error     string `json:",omitempty"`
	ErrorNode int    // the node which failed to parse (ignore if Error is empty)

	OutputPayloads map[int]map[string]interface{}
	InitialPayload map[string]interface{} // the payload which gets passed to nodes with no dependencies
	Failures       map[int]NodeParseFailure
}

// NodeParseFailure represents all problems encountered during a node's Parse phase
type NodeParseFailure struct {
	Errors []NodeParseError
}

// Error returns an error string for the NodeParseFailure. It just returns the first error
// if there are multiple errors; to handle it better, walk the Errors array yourself.
func (f NodeParseFailure) Error() string {
	if len(f.Errors) > 0 {
		// just print the first error
		return f.Errors[0].String()
	}
	return ""
}

// AddError registers a new error. It can take regular errors, NodeParseError, or NodeParseFailure.
func (f *NodeParseFailure) AddError(e error) {
	if e == nil {
		return
	}
	switch t := e.(type) {
	case NodeParseError:
		f.Errors = append(f.Errors, t)
	case *NodeParseError:
		f.Errors = append(f.Errors, *t)
	case NodeParseFailure:
		f.Errors = append(f.Errors, t.Errors...)
	case *NodeParseFailure:
		f.Errors = append(f.Errors, t.Errors...)
	default:
		f.Errors = append(f.Errors, NodeParseError{Err: e.Error()})
	}
}

// ErrCount returns the number of errors registered.
func (f *NodeParseFailure) ErrCount() int {
	return len(f.Errors)
}

// NodeParseError represents a single problem encountered during the parse phase,
// e.g. an un-set config field. Field represents which config field, if any, was
// the source of the problem; if unset, the error was of a more general nature.
type NodeParseError struct {
	Err   string
	Field string `json:",omitempty"`
}

func (f NodeParseError) Error() string {
	return f.String()
}

func (f NodeParseError) String() string {
	if f.Field != "" {
		return fmt.Sprintf("%v: %v", f.Field, f.Err)
	}
	return f.Err
}

type UserMailConfig struct {
	Server             string
	Port               int
	Username           string
	Password           string
	UseTLS             bool
	InsecureSkipVerify bool
}

type UserMail struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Body        string
	Attachments []UserMailAttachment
}

func (um UserMail) Validate() error {
	if um.From == `` {
		return errors.New("Missing from")
	}
	if len(um.To) == 0 {
		return errors.New("no recepients")
	}
	for _, v := range um.To {
		if v == `` {
			return errors.New("Invalid recepient")
		}
	}
	for _, v := range um.Attachments {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type UserMailAttachment struct {
	Name    string
	Content []byte
}

func (uma UserMailAttachment) Validate() error {
	if uma.Name == `` {
		return errors.New("Invalid attachment name")
	}
	return nil
}

const scheduledScriptAnko string = `anko`
const scheduledScriptGo string = `go`

func (sl ScriptLang) String() string {
	switch sl {
	case ScriptAnko:
		return scheduledScriptAnko
	case ScriptGo:
		return scheduledScriptGo
	}
	return `UNKNOWN`
}

func (sl ScriptLang) Valid() (err error) {
	switch sl {
	case ScriptAnko:
	case ScriptGo:
	default:
		err = ErrUnknownScriptLanguage
	}
	return
}

func ParseScriptLang(v string) (l ScriptLang, err error) {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case scheduledScriptAnko:
		l = ScriptAnko
	case scheduledScriptGo:
		l = ScriptGo
	default:
		err = ErrUnknownScriptLanguage
	}
	return
}

// AutomationCommonFields defines fields which exist on all types of
// automations.  They are mostly related to scheduling.
type AutomationCommonFields struct {
	Schedule string // when to run: a cron spec
	Timezone string // a location to use for the timezone,
	// e.g. "America/New_York" if Disabled is true, the automation
	// will never execute on schedule (but may be run manually)
	Disabled bool
	// if BackfilleEnabled is true, search agent will attempt to
	// "backfill" missed runs since the more recent of Updated or
	// LastRun.
	BackfillEnabled bool
}

// AutomationResultsCommonFields specifies fields which exist in all types of automation *results*, mainly errors and the time of last execution.
type AutomationResultsCommonFields struct {
	// These fields will be updated by the search agent after the search runs.
	LastRun         time.Time
	LastRunDuration time.Duration    // how many nanoseconds did it take
	LastSearchIDs   []string         // the IDs of the most recently performed searches
	LastError       string           // any error from the last run of the scheduled search
	ErrorHistory    []ScheduledError // a list of previously-occurring errors
}

// ScheduledSearch represents a Gravwell query to be run on a schedule.
type ScheduledSearch struct {
	CommonFields

	AutomationCommonFields

	SearchReference    string // A reference to a saved query item by ID. If SearchString is populated on a GET, it represents the query referenced by SearchReference.
	SearchString       string // The actual search to run. If SearchReference is populated on a GET, SearchString represents the query referenced by SearchReference.
	Duration           int64  // How many seconds back to search, MUST BE NEGATIVE
	SearchSinceLastRun bool   // If set, ignore Duration and run from last run time to now.
	TimeframeOffset    int64  // How many seconds to offset the search timeframe, MUST BE NEGATIVE
}

// ScheduledSearchResults represents the results of a ScheduledSearch execution.
type ScheduledSearchResults struct {
	CommonFields

	AutomationResultsCommonFields

	ScheduledSearchID string // references the ScheduledSearch this result belongs to
}

// ScheduledScript represents an Anko or Go script to run on a schedule.
type ScheduledScript struct {
	CommonFields

	AutomationCommonFields

	Script         string     // If set, execute the contents rather than running SearchString
	ScriptLanguage ScriptLang // what script type is this: anko, go
}

// ScheduledScriptResults represents the results of a ScheduledScript execution.
type ScheduledScriptResults struct {
	CommonFields

	AutomationResultsCommonFields

	ScheduledScriptID string // references the ScheduledScript this result belongs to
	DebugOutput       []byte // output of the script if debugmode was enabled
}

// Flow represents a flow-type automation to run on a schedule.
type Flow struct {
	CommonFields

	AutomationCommonFields

	Flow string // The flow specification itself
}

// FlowResults represents the results of a Flow execution.
type FlowResults struct {
	CommonFields

	AutomationResultsCommonFields

	FlowID          string                 // references the Flow this result belongs to
	FlowNodeResults map[int]FlowNodeResult // results for each node in the flow
	DebugOutput     []byte                 // output of the script if debugmode was enabled
}

// AutomationDebugRequest is what gets submitted to the webserver when we're requesting a debug run of an automation.
type AutomationDebugRequest struct {
	OneShot    bool   // Set this flag to 'true' to make the search fire ONCE
	DebugMode  bool   // set this to true to enable debug mode
	DebugEvent *Event // If provided, this will be inserted as `event` into the flow payload.
}

// ScheduledSearchListResponse is the response type for listing scheduled searches.
type ScheduledSearchListResponse struct {
	BaseListResponse
	Results []ScheduledSearch `json:"results"`
}

// ScheduledSearchResultsListResponse is the response type for listing scheduled search results.
type ScheduledSearchResultsListResponse struct {
	BaseListResponse
	Results []ScheduledSearchResults `json:"results"`
}

// ScheduledScriptListResponse is the response type for listing scheduled searches.
type ScheduledScriptListResponse struct {
	BaseListResponse
	Results []ScheduledScript `json:"results"`
}

// ScheduledScriptResultsListResponse is the response type for listing scheduled search results.
type ScheduledScriptResultsListResponse struct {
	BaseListResponse
	Results []ScheduledScriptResults `json:"results"`
}

// FlowListResponse is the response type for listing scheduled searches.
type FlowListResponse struct {
	BaseListResponse
	Results []Flow `json:"results"`
}

// FlowResultsListResponse is the response type for listing scheduled search results.
type FlowResultsListResponse struct {
	BaseListResponse
	Results []FlowResults `json:"results"`
}
