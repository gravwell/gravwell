/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

type AutomationDeployConfig struct {
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

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r FlowNodeResult) MarshalJSON() ([]byte, error) {
	type dummyFlowNodeResult FlowNodeResult
	r.Payload = nonNilMap(r.Payload)
	return json.Marshal(dummyFlowNodeResult(r))
}

type ScheduledScriptParseRequest struct {
	ScriptLanguage ScriptLang
	Script         string
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

	OutputPayloads map[int]map[string]interface{}

	// InitialPayload defines a payload to be passed in to any
	// nodes with no dependencies, i.e. the nodes which will run
	// first.
	InitialPayload map[string]interface{}

	// Failures contains a map of node ID to failure for any nodes
	// which failed to parse.  Note that parsing continues as much
	// as possible, but if a node fails to parse, any nodes
	// downstream of it will perforce be skipped.
	Failures map[int]NodeParseFailure
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r FlowParseResponse) MarshalJSON() ([]byte, error) {
	type dummyFlowParseResponse FlowParseResponse
	r.OutputPayloads = nonNilMap(r.OutputPayloads)
	r.InitialPayload = nonNilMap(r.InitialPayload)
	r.Failures = nonNilMap(r.Failures)
	return json.Marshal(dummyFlowParseResponse(r))
}

// NodeParseFailure represents all problems encountered during a node's Parse phase
type NodeParseFailure struct {
	Errors []NodeParseError
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (f NodeParseFailure) MarshalJSON() ([]byte, error) {
	type dummyNodeParseFailure NodeParseFailure
	f.Errors = nonNilSlice(f.Errors)
	return json.Marshal(dummyNodeParseFailure(f))
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

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (um UserMail) MarshalJSON() ([]byte, error) {
	type dummyUserMail UserMail
	um.To = nonNilSlice(um.To)
	um.Cc = nonNilSlice(um.Cc)
	um.Bcc = nonNilSlice(um.Bcc)
	um.Attachments = nonNilSlice(um.Attachments)
	return json.Marshal(dummyUserMail(um))
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

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (uma UserMailAttachment) MarshalJSON() ([]byte, error) {
	type dummyUserMailAttachment UserMailAttachment
	uma.Content = nonNilSlice(uma.Content)
	return json.Marshal(dummyUserMailAttachment(uma))
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
	// if BackfillEnabled is true, search agent will attempt to
	// "backfill" missed runs since the more recent of Updated or
	// LastRun.
	BackfillEnabled bool
}

// AutomationResultsCommonFields specifies fields which exist in all types of automation *results*, mainly errors and the time of last execution.
type AutomationResultsCommonFields struct {
	// These fields will be updated by the search agent after the search runs.
	LastRun         time.Time
	LastRunDuration time.Duration // how many nanoseconds did it take
	LastSearchIDs   []string      // the IDs of the most recently performed searches
	LastError       string        // any error from the last run of the scheduled search
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
	LatestResults      *ScheduledSearchResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s ScheduledSearch) MarshalJSON() ([]byte, error) {
	type dummyScheduledSearch ScheduledSearch
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummyScheduledSearch(s))
}

func (s *ScheduledSearch) GetLastRunTime() (lastRun time.Time) {
	if s.LatestResults == nil {
		return time.Time{}
	}
	lastRun = s.LatestResults.LastRun
	return
}

func (s *ScheduledSearch) GetLastSearchIDs() []string {
	if s.LatestResults == nil {
		return []string{}
	}
	return s.LatestResults.LastSearchIDs
}

// ScheduledSearchResults represents the results of a ScheduledSearch execution.
type ScheduledSearchResults struct {
	CommonFields

	AutomationResultsCommonFields

	ScheduledSearchID string // references the ScheduledSearch this result belongs to
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledSearchResults) MarshalJSON() ([]byte, error) {
	type dummyScheduledSearchResults ScheduledSearchResults
	r.CommonFields = r.CommonFields.MakeNilSlices()
	r.AutomationResultsCommonFields = r.AutomationResultsCommonFields.MakeNilSlices()
	return json.Marshal(dummyScheduledSearchResults(r))
}

// ScheduledScript represents an Anko or Go script to run on a schedule.
type ScheduledScript struct {
	CommonFields

	AutomationCommonFields

	Script         string     // If set, execute the contents rather than running SearchString
	ScriptLanguage ScriptLang // what script type is this: anko, go
	LatestResults  *ScheduledScriptResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s ScheduledScript) MarshalJSON() ([]byte, error) {
	type dummyScheduledScript ScheduledScript
	s.CommonFields = s.CommonFields.MakeNilSlices()
	return json.Marshal(dummyScheduledScript(s))
}

func (s *ScheduledScript) GetLastRunTime() (lastRun time.Time) {
	if s.LatestResults == nil {
		return time.Time{}
	}
	lastRun = s.LatestResults.LastRun
	return
}

func (s *ScheduledScript) GetLastSearchIDs() []string {
	if s.LatestResults == nil {
		return []string{}
	}
	return s.LatestResults.LastSearchIDs
}

func (s *ScheduledScript) GetLastPersistentMaps() map[string]map[string]any {
	if s.LatestResults == nil {
		return map[string]map[string]any{}
	}
	return s.LatestResults.PersistentMaps
}

// ScheduledScriptResults represents the results of a ScheduledScript execution.
type ScheduledScriptResults struct {
	CommonFields

	AutomationResultsCommonFields

	ScheduledScriptID string                    // references the ScheduledScript this result belongs to
	DebugOutput       []byte                    // output of the script if debugmode was enabled
	PersistentMaps    map[string]map[string]any // a place to stash variables between runs
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledScriptResults) MarshalJSON() ([]byte, error) {
	type dummyScheduledScriptResults ScheduledScriptResults
	r.CommonFields = r.CommonFields.MakeNilSlices()
	r.AutomationResultsCommonFields = r.AutomationResultsCommonFields.MakeNilSlices()
	r.DebugOutput = nonNilSlice(r.DebugOutput)
	r.PersistentMaps = nonNilMap(r.PersistentMaps)
	return json.Marshal(dummyScheduledScriptResults(r))
}

// Flow represents a flow-type automation to run on a schedule.
type Flow struct {
	CommonFields

	AutomationCommonFields

	Flow          string // The flow specification itself
	LatestResults *FlowResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (f Flow) MarshalJSON() ([]byte, error) {
	type dummyFlow Flow
	f.CommonFields = f.CommonFields.MakeNilSlices()
	return json.Marshal(dummyFlow(f))
}

func (f *Flow) GetLastRunTime() (lastRun time.Time) {
	if f.LatestResults == nil {
		return time.Time{}
	}
	lastRun = f.LatestResults.LastRun
	return
}

func (f *Flow) GetLastSearchIDs() []string {
	if f.LatestResults == nil {
		return []string{}
	}
	return f.LatestResults.LastSearchIDs
}

func (f *Flow) GetLastPersistentMaps() map[string]map[string]any {
	if f.LatestResults == nil {
		return map[string]map[string]any{}
	}
	return f.LatestResults.PersistentMaps
}

// FlowResults represents the results of a Flow execution.
type FlowResults struct {
	CommonFields

	AutomationResultsCommonFields

	FlowID          string                    // references the Flow this result belongs to
	FlowNodeResults map[int]FlowNodeResult    // results for each node in the flow
	DebugOutput     []byte                    // output of the script if debugmode was enabled
	PersistentMaps  map[string]map[string]any // a place to stash variables between runs
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r FlowResults) MarshalJSON() ([]byte, error) {
	type dummyFlowResults FlowResults
	r.CommonFields = r.CommonFields.MakeNilSlices()
	r.AutomationResultsCommonFields = r.AutomationResultsCommonFields.MakeNilSlices()
	r.FlowNodeResults = nonNilMap(r.FlowNodeResults)
	r.DebugOutput = nonNilSlice(r.DebugOutput)
	r.PersistentMaps = nonNilMap(r.PersistentMaps)
	return json.Marshal(dummyFlowResults(r))
}

// AutomationDebugRequest is what gets submitted to the webserver when we're requesting a debug run of an automation.
type AutomationDebugRequest struct {
	DebugMode  bool   // set this to true to enable debug mode
	DebugEvent *Event // If provided, this will be inserted as `event` into the flow payload.
}

// ScheduledSearchListResponse is the response type for listing scheduled searches.
type ScheduledSearchListResponse struct {
	BaseListResponse
	Results []ScheduledSearch
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledSearchListResponse) MarshalJSON() ([]byte, error) {
	type dummyScheduledSearchListResponse ScheduledSearchListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyScheduledSearchListResponse(r))
}

// ScheduledSearchResultsListResponse is the response type for listing scheduled search results.
type ScheduledSearchResultsListResponse struct {
	BaseListResponse
	Results []ScheduledSearchResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledSearchResultsListResponse) MarshalJSON() ([]byte, error) {
	type dummyScheduledSearchResultsListResponse ScheduledSearchResultsListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyScheduledSearchResultsListResponse(r))
}

// ScheduledScriptListResponse is the response type for listing scheduled searches.
type ScheduledScriptListResponse struct {
	BaseListResponse
	Results []ScheduledScript
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledScriptListResponse) MarshalJSON() ([]byte, error) {
	type dummyScheduledScriptListResponse ScheduledScriptListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyScheduledScriptListResponse(r))
}

// ScheduledScriptResultsListResponse is the response type for listing scheduled search results.
type ScheduledScriptResultsListResponse struct {
	BaseListResponse
	Results []ScheduledScriptResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ScheduledScriptResultsListResponse) MarshalJSON() ([]byte, error) {
	type dummyScheduledScriptResultsListResponse ScheduledScriptResultsListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyScheduledScriptResultsListResponse(r))
}

// FlowListResponse is the response type for listing scheduled searches.
type FlowListResponse struct {
	BaseListResponse
	Results []Flow
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r FlowListResponse) MarshalJSON() ([]byte, error) {
	type dummyFlowListResponse FlowListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyFlowListResponse(r))
}

// FlowResultsListResponse is the response type for listing scheduled search results.
type FlowResultsListResponse struct {
	BaseListResponse
	Results []FlowResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r FlowResultsListResponse) MarshalJSON() ([]byte, error) {
	type dummyFlowResultsListResponse FlowResultsListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyFlowResultsListResponse(r))
}

// SearchAgentCheckin is the type sent by the searchagent to the
// webserver. It contains the search agent's config and information
// about tasks the search agent is currently executing.
type SearchAgentCheckin struct {
	Cfg       SearchAgentConfig
	Active    []string // automations currently running/pending
	Cancelled []string // acknowledgement of automations cancelled

	SearchResults []ScheduledSearchResults
	ScriptResults []ScheduledScriptResults
	FlowResults   []FlowResults
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (c SearchAgentCheckin) MarshalJSON() ([]byte, error) {
	type dummySearchAgentCheckin SearchAgentCheckin
	c.Active = nonNilSlice(c.Active)
	c.Cancelled = nonNilSlice(c.Cancelled)
	c.SearchResults = nonNilSlice(c.SearchResults)
	c.ScriptResults = nonNilSlice(c.ScriptResults)
	c.FlowResults = nonNilSlice(c.FlowResults)
	return json.Marshal(dummySearchAgentCheckin(c))
}

type SearchAgentCheckinResponse struct {
	Cancel     []string // List of automations to cancel
	SearchJobs []SearchJob
	ScriptJobs []ScriptJob
	FlowJobs   []FlowJob
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r SearchAgentCheckinResponse) MarshalJSON() ([]byte, error) {
	type dummySearchAgentCheckinResponse SearchAgentCheckinResponse
	r.Cancel = nonNilSlice(r.Cancel)
	r.SearchJobs = nonNilSlice(r.SearchJobs)
	r.ScriptJobs = nonNilSlice(r.ScriptJobs)
	r.FlowJobs = nonNilSlice(r.FlowJobs)
	return json.Marshal(dummySearchAgentCheckinResponse(r))
}

// SearchAgentInfo contains information about an individual search agent.
type SearchAgentInfo struct {
	Cfg         SearchAgentConfig
	LastCheckin time.Time
	ActiveJobs  []string // IDs of automations currently running on this search agent
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (i SearchAgentInfo) MarshalJSON() ([]byte, error) {
	type dummySearchAgentInfo SearchAgentInfo
	i.ActiveJobs = nonNilSlice(i.ActiveJobs)
	return json.Marshal(dummySearchAgentInfo(i))
}

// SearchAgentStatus returns what the webserver knows about searchagents.
type SearchAgentStatus struct {
	LastCheckin     time.Time
	Warning         bool
	SchedulerStatus SchedulerStatus
}

// SchedulerStatus reports information from automation job scheduler,
// such as which search agents it has seen.
type SchedulerStatus struct {
	SearchAgents []SearchAgentInfo
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (s SchedulerStatus) MarshalJSON() ([]byte, error) {
	type dummySchedulerStatus SchedulerStatus
	s.SearchAgents = nonNilSlice(s.SearchAgents)
	return json.Marshal(dummySchedulerStatus(s))
}

// DoAgentsAllowNetworkFunctions returns true if all known/active
// search agents allow network functions in scripts/flows.  If no
// search agents have checked in, assume networking is allowed.
func (s SchedulerStatus) DoAgentsAllowNetworkFunctions() bool {
	for _, a := range s.SearchAgents {
		if a.Cfg.Disable_Network_Script_Functions {
			return false
		}
	}
	return true
}

// MostRecentCheckin returns the time of the most recent searchagent checkin.
func (s SchedulerStatus) MostRecentCheckin() time.Time {
	var latest time.Time
	for _, a := range s.SearchAgents {
		if a.LastCheckin.After(latest) {
			latest = a.LastCheckin
		}
	}
	return latest
}

type SearchJob struct {
	Search  ScheduledSearch
	RunID   string
	EndTime time.Time
	OneShot bool // true if user-requested or alert-triggered
	Debug   AutomationDebugRequest
	Event   *Event
}

type ScriptJob struct {
	Script  ScheduledScript
	RunID   string
	EndTime time.Time
	OneShot bool // true if user-requested or alert-triggered
	Debug   AutomationDebugRequest
	Event   *Event
}

type FlowJob struct {
	Flow    Flow
	RunID   string
	EndTime time.Time
	OneShot bool // true if user-requested or alert-triggered
	Debug   AutomationDebugRequest
	Event   *Event
}
