/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"time"
)

// Dashboard defines tiles and searches to show a dashboard in the UI.
type Dashboard struct {
	CommonFields
	Data DashboardContents
}

type DashboardContents struct {
	LiveUpdateInterval int                `json:"liveUpdateInterval,omitempty"`
	LinkZooming        bool               `json:"linkZooming,omitempty"`
	Grid               DashboardGrid      `json:"grid,omitempty"`
	Searches           []DashboardSearch  `json:"searches,omitempty"`
	Tiles              []DashboardTile    `json:"tiles,omitempty"`
	Timeframe          DashboardTimeframe `json:"timeframe,omitempty"`
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
	ID     string                   `json:"id"`
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
