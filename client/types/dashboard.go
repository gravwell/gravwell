/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Dashboard defines tiles and searches to show a dashboard in the UI.
type Dashboard struct {
	CommonFields
	Grid        DashboardGrid
	LinkZooming bool
	LiveUpdate  DashboardLiveUpdateSettings
	Searches    map[string]DashboardSearchable
	Tiles       map[string]DashboardTile
	Timeframe   DashboardTimeframe
}

// DashboardLiveUpdateSettings describes Live Update behavior for a Dashboard.
type DashboardLiveUpdateSettings struct {
	Enabled  bool
	Interval string
}

// DashboardTimeframe represents a timeframe: a relative or absolute period of time.
// Kind determines the variant: "range" uses Start and End (RFC3339 or datemath strings),
// "preview" uses only Kind.
type DashboardTimeframe struct {
	Kind  string
	Start string `json:",omitempty"`
	End   string `json:",omitempty"`
}

// DashboardGrid describes top-level grid layout options for a Dashboard.
type DashboardGrid struct {
	Gutter       float64 `json:",omitempty"`
	Margin       float64 `json:",omitempty"`
	BorderWidth  float64 `json:",omitempty"`
	BorderRadius float64 `json:",omitempty"`
}

// DashboardSearchable describes a searchable thing associated with a Dashboard,
// used to drive one or more dashboard tiles.
type DashboardSearchable struct {
	Color             string `json:",omitempty"`
	Name              string `json:",omitempty"`
	Reference         Searchable
	TimeframeOverride *DashboardTimeframe `json:",omitempty"`
}

// Searchable represents a searchable thing that can launch or attach to a search.
// Kind determines the variant: "template", "saved_query", "scheduled_search" use ID;
// "query_string" uses QueryString.
type Searchable struct {
	Kind        string
	ID          string `json:",omitempty"`
	QueryString string `json:",omitempty"`
}

// DashboardTile describes a tile that is part of a Dashboard.
type DashboardTile struct {
	Name            string
	Renderer        string
	RendererOptions map[string]interface{} `json:",omitempty"`
	SearchKey       string
	ShowZoom        bool
	TileConfig      DashboardTileConfig
}

// DashboardTileConfig describes the position and size of a tile within the grid.
type DashboardTileConfig struct {
	Height float64
	Width  float64
	X      float64 `json:",omitempty"`
	Y      float64 `json:",omitempty"`
}

type DashboardListResponse struct {
	BaseListResponse
	Results []Dashboard `json:"results"`
}
