/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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

// ScheduledSearch represents a scheduled search, including rules, description,
// etc.
type ScheduledSearch struct {
	ID          int32
	GUID        uuid.UUID
	Groups      []int32
	Global      bool
	Name        string // the name of this scheduled search
	Description string // freeform description
	Labels      []string
	Owner       int32  // uid of owner
	Schedule    string // when to run: a cron spec
	Timezone    string // a location to use for the timezone, e.g. "America/New_York"
	Updated     time.Time
	Disabled    bool // scheduling is disabled, do not run this search/script/flow
	OneShot     bool // Set this flag to 'true' to make the search fire ONCE
	DebugMode   bool // set this to true to enable debug mode
	Synced      bool // used internally to indicate the scheduled search has been synced

	// if true, search agent will attempt to "backfill" missed runs since
	// the more recent of Updated or LastRun.
	BackfillEnabled bool

	// This sets what kind of scheduled "thing" it is: search, script, or flow
	ScheduledType string

	// Fields for scheduled searches
	SearchString       string        // The actual search to run
	Duration           int64         // How many seconds back to search, MUST BE NEGATIVE
	SearchSinceLastRun bool          // If set, ignore Duration and run from last run time to now.
	Alert              bool          // set to true for scheduled queries that are a basis for an alert
	AlertConfig        AlertMetadata `json:",omitempty"` //if scheduled search is an alert, this config adds values

	// For scheduled scripts
	Script         string     // If set, execute the contents rather than running SearchString
	ScriptLanguage ScriptLang // what script type is this: anko, go

	// For scheduled flows
	Flow            string                 // The flow specification itself
	FlowNodeResults map[int]FlowNodeResult // results for each node in the flow

	// For Reactive scheduling (flows and scripts only)
	Reactive       bool            // do not schedule, instead this is fired externally
	ReactiveInputs []ReactiveInput //set of inputs that will be available to the flow at start

	// These fields are updated by the search agent after it runs a search
	PersistentMaps  map[string]map[string]interface{}
	LastRun         time.Time
	LastRunDuration time.Duration // how many nanoseconds did it take
	LastSearchIDs   []string      // the IDs of the most recently performed searches
	LastError       string        // any error from the last run of the scheduled search
	DebugOutput     []byte        // output of the script if debugmode was enabled
}

type AlertMetadata struct {
	Name     string            //name of the alert - REQUIRED
	Category string            //category of the alert - OPTIONAL
	Level    string            //level of the alert (info, warn, high, critical) - REQUIRED
	Target   uuid.UUID         //The flow or script that will be fired if the query has results (may be blank)
	Metadata map[string]string `json:",omitempty"` //set of additional free form keys that can be attached to an alert
}

// ReactiveInput represents a configured key that must be present in an reactive flow
// The name indicates the name to be injected into the payload of a flow or the global environment for a script
type ReactiveInput struct {
	Required   bool   // if set to true then the reactive flow will not start if the value is missing
	Name       string // name of the value to be injected
	DebugValue string // temporary debug value to be injected into the payload when doing debug/testing runs
}

type FlowNodeResult struct {
	Payload map[string]interface{}
	Log     string
	Error   string
	Start   int64 // unix nanoseconds
	End     int64 // unix nanoseconds
	// The first node executed has sequence 0, the next is sequence 1, etc.
	// Nodes which were not executed have Sequence = SEQ_NODE_NOT_EXECUTED
	Sequence int
}

type ScheduledSearchParseRequest struct {
	Version int
	Script  string
}

type ScheduledSearchParseResponse struct {
	OK          bool
	Error       string `json:",omitempty"`
	ErrorLine   int
	ErrorColumn int
}

type FlowParseRequest struct {
	Flow string
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

func (ss *ScheduledSearch) TypeName() string {
	// if the type is set, use that
	if ss.ScheduledType != "" {
		return ss.ScheduledType
	}
	if len(ss.Script) > 0 {
		return "script"
	}
	return "search"
}

func (ss *ScheduledSearch) Dedup() {
	gidmap := make(map[int32]bool)
	for _, gid := range ss.Groups {
		gidmap[gid] = true
	}
	var newgids []int32
	for gid, val := range gidmap {
		if val {
			newgids = append(newgids, gid)
		}
	}
	ss.Groups = newgids
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

func (s ScheduledSearch) Equal(v ScheduledSearch) bool {
	if s.ID != v.ID || s.GUID != v.GUID || s.Name != v.Name ||
		s.Description != v.Description || s.Owner != v.Owner ||
		s.Schedule != v.Schedule || s.Timezone != v.Timezone ||
		s.Disabled != v.Disabled || s.OneShot != v.OneShot ||
		s.Global != v.Global || s.DebugMode != v.DebugMode {
		return false
	}
	if s.SearchString != v.SearchString ||
		s.Duration != v.Duration ||
		s.SearchSinceLastRun != v.SearchSinceLastRun ||
		s.Script != v.Script {
		return false
	}
	if len(s.Groups) != len(v.Groups) || len(s.Labels) != len(v.Labels) || len(s.LastSearchIDs) != len(v.LastSearchIDs) {
		return false
	}
	for i, g := range s.Groups {
		if v.Groups[i] != g {
			return false
		}
	}
	for i, l := range s.Labels {
		if l != v.Labels[i] {
			return false
		}
	}
	for i, id := range s.LastSearchIDs {
		if id != v.LastSearchIDs[i] {
			return false
		}
	}
	if s.LastRun != v.LastRun || s.LastRunDuration != v.LastRunDuration || s.LastError != v.LastError {
		return false
	}
	if !bytes.Equal(v.DebugOutput, s.DebugOutput) {
		return false
	}

	if (s.PersistentMaps == nil) != (v.PersistentMaps == nil) {
		return false
	} else if s.PersistentMaps == nil {
		return true //both are nil
	}
	//just check the first level of keys
	for k, val := range s.PersistentMaps {
		if vv, ok := v.PersistentMaps[k]; !ok {
			return false
		} else if len(val) != len(vv) {
			return false
		}
	}

	return true
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
