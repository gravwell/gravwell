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
	"time"

	"github.com/gravwell/gravwell/v3/utils"
)

// Dashboard type used for relaying data back and forth to frontend.
type Dashboard struct {
	ID          uint64
	Name        string
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
	Description string
	Created     time.Time
	Updated     time.Time
	Data        RawObject
	Labels      []string
	GUID        string `json:",omitempty"`
	Trivial     bool   `json:",omitempty"`
	Synced      bool
}

// DashboardAdd is used to push new dashboards.
type DashboardAdd struct {
	Name        string
	Description string
	Data        RawObject
	Labels      []string
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
}

// DashboardPost is used in sending a new dashboard to the marketplace.
type DashboardPost struct {
	Name string
	Desc string
	JSON []byte
	User string
	Tags []string
}

// DashboardGet is used to get a dashboard from the marketplace.
type DashboardGet struct {
	Name     string
	Desc     string
	JSON     []byte
	User     string
	Score    int `json:",omitempty"`
	Version  int `json:",omitempty"`
	GUID     string
	Created  time.Time
	Updated  time.Time
	Customer string
	Tags     []string
}

// DashboardComment is used to send and retrieve comments.
type DashboardComment struct {
	ID      int
	UID     int
	Name    string
	Comment string
}

func EncodeDashboardAdd(name, desc string, obj interface{}) (*DashboardAdd, error) {
	msg, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return &DashboardAdd{
		Name:        name,
		Description: desc,
		Data:        RawObject(msg),
	}, nil
}

type Dashboards []Dashboard

func (d Dashboard) Equal(v Dashboard) bool {
	if d.ID != v.ID || d.Name != v.Name || d.UID != v.UID {
		return false
	}
	if d.Description != v.Description || d.GUID != v.GUID {
		return false
	}
	if len(d.GIDs) != len(v.GIDs) || len(d.Labels) != len(v.Labels) {
		return false
	}
	if d.Global != v.Global {
		return false
	}
	if !d.WriteAccess.Equal(v.WriteAccess) {
		return false
	}
	for i, l := range d.Labels {
		if l != v.Labels[i] {
			return false
		}
	}
	return utils.Int32SlicesEqual(d.GIDs, v.GIDs)
}

// JSON custom marshallers
type dbaddMarshaler struct {
	Name        string
	Description string
	Data        RawObject
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
}

func (d DashboardAdd) MarshalJSON() ([]byte, error) {
	var data RawObject
	if len(d.Data) == 0 {
		data = emptyRawObj
	} else {
		data = RawObject(d.Data)
	}
	dba := dbaddMarshaler{
		Name:        d.Name,
		Description: d.Description,
		Data:        data,
		UID:         d.UID,
		GIDs:        d.GIDs,
		Global:      d.Global,
		WriteAccess: d.WriteAccess,
	}
	return json.Marshal(dba)
}

func (d *DashboardAdd) UnmarshalObject(obj interface{}) error {
	return json.Unmarshal(d.Data, obj)
}

func (d *Dashboard) UnmarshalObject(obj interface{}) error {
	return json.Unmarshal(d.Data, obj)
}

func (d Dashboards) MarshalJSON() ([]byte, error) {
	if len(d) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]Dashboard(d))
}
