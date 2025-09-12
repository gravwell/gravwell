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

	"github.com/google/uuid"
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

// StrictDashboard is a dashboard where we actually unpack all the contents.
// We're going to use this for migration to the registry.
type StrictDashboard struct {
	ID          uint64
	Name        string
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
	Description string
	Created     time.Time
	Updated     time.Time
	Data        DashboardContents
	Labels      []string
	GUID        string `json:",omitempty"`
	Trivial     bool   `json:",omitempty"`
	Synced      bool
}

type DashboardContents struct {
	LiveUpdateInterval int                `json:"liveUpdateInterval,omitempty"`
	LinkZooming        bool               `json:"linkZooming,omitempty"`
	Grid               DashboardGrid      `json:"grid,omitempty"`
	Searches           []DashboardSearch  `json:"searches,omitempty"`
	Tiles              []DashboardTile    `json:"tiles,omitempty"`
	Timeframe          DashboardTimeframe `json:"timeframe,omitempty"`
	Version            int                `json:"version,omitempty"`
	LastDataUpdate     time.Time          `json:"lastDataUpdate,omitempty"`
}

type DashboardTimeframe struct {
	DurationString string    `json:"durationString"`
	Timeframe      string    `json:"timeframe"`
	Timezone       string    `json:"timezone"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
}

type DashboardGrid struct {
	Gutter       int `json:"gutter,omitempty"`
	Margin       int `json:"margin,omitempty"`
	BorderWidth  int `json:"borderWidth,omitempty"`
	BorderRadius int `json:"borderRadius,omitempty"`
}

type DashboardSearch struct {
	Alias     string             `json:"alias"`
	Timeframe DashboardTimeframe `json:"timeframe"`
	Query     string             `json:"query,omitempty"`
	SearchID  int                `json:"searchID,omitempty"`
	Color     string             `json:"color,omitempty"`
	Reference DashboardReference `json:"reference"`
}

type DashboardReference struct {
	ID     uuid.UUID                `json:"id"`
	Type   string                   `json:"type"`
	Extras DashboardReferenceExtras `json:"extras"`
}

type DashboardReferenceExtras struct {
	DefaultValue string `json:"defaultValue,omitempty"`
}

type DashboardTile struct {
	ID              int                      `json:"id,omitempty"`
	Title           string                   `json:"title"`
	Renderer        string                   `json:"renderer"`
	HideZoom        bool                     `json:"hideZoom,omitempty"`
	Span            DashboardTileSpan        `json:"span"`
	SearchesIndex   int                      `json:"searchesIndex"`
	RendererOptions DashboardRendererOptions `json:"rendererOptions"`
}

type DashboardTileSpan struct {
	Col int `json:"col"`
	Row int `json:"row"`
	X   int `json:"x,omitempty"`
	Y   int `json:"y,omitempty"`
}

type DashboardRendererOptions struct {
	XAxisSplitLine string                         `json:",omitempty"`
	YAxisSplitLine string                         `json:",omitempty"`
	IncludeOther   string                         `json:",omitempty"`
	Stack          string                         `json:",omitempty"`
	Smoothing      string                         `json:",omitempty"`
	Orientation    string                         `json:",omitempty"`
	ConnectNulls   string                         `json:",omitempty"`
	Precision      string                         `json:",omitempty"`
	LogScale       string                         `json:",omitempty"`
	Range          string                         `json:",omitempty"`
	Rotate         string                         `json:",omitempty"`
	Labels         string                         `json:",omitempty"`
	Background     string                         `json:",omitempty"`
	Values         DashboardRendererOptionsValues `json:"values"`
}

type DashboardRendererOptionsValues struct {
	Smoothing   string
	Orientation string
	Columns     []string `json:"columns,omitempty"`
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
