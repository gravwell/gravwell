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
	"time"

	"github.com/google/uuid"
)

const (
	ScriptVersionAnko int = 0 // default is anko
)

type ScriptDeployConfig struct {
	Disabled       bool
	RunImmediately bool
}

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
	Disabled    bool
	OneShot     bool // Set this flag to 'true' to make the search fire ONCE
	DebugMode   bool // set this to true to enable debug mode
	Synced      bool

	SearchString       string // The actual search to run
	Duration           int64  // How many seconds back to search, MUST BE NEGATIVE
	SearchSinceLastRun bool   // If set, ignore Duration and run from last run time to now.
	Script             string // If set, execute the contents rather than running SearchString

	// These fields are updated by the search agent after it runs a search
	PersistentMaps  map[string]map[string]interface{}
	LastRun         time.Time
	LastRunDuration time.Duration // how many nanoseconds did it take
	LastSearchIDs   []string      // the IDs of the most recently performed searches
	LastError       string        // any error from the last run of the scheduled search
	DebugOutput     []byte        // output of the script if debugmode was enabled
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

func (ss *ScheduledSearch) TypeName() string {
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
