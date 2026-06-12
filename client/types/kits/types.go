/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/client/types"
)

// PackedFile is a stripped-down representation of a file for inclusion in a kit.
type PackedFile struct {
	ID          string
	Name        string
	Description string
	Labels      []string
	Size        uint64 `json:",omitempty"`
	Hash        string `json:",omitempty"`
	Data        []byte `json:",omitempty"`
}

// PackFile takes a File and its contents and converts them into a PackedFile.
func PackFile(f types.File, content []byte) (p PackedFile) {
	p = PackedFile{
		ID:          f.ID,
		Name:        f.Name,
		Description: f.Description,
		Labels:      f.Labels,
		Size:        f.Size,
		Data:        content,
		Hash:        f.Hash,
	}

	return
}

// Validate checks the contents of a PackedFile for validity.
func (p *PackedFile) Validate() error {
	if len(p.Name) == 0 {
		return errors.New("Invalid file name")
	} else if p.Size != uint64(len(p.Data)) {
		return errors.New("mismatched data and data size")
	}
	if len(p.Data) == 0 && len(p.Hash) == 0 {
		return nil //short circuit, if its empty there is no hash
	}
	p.Size = uint64(len(p.Data))
	return nil
}

// PackedMacro is a stripped-down representation of a macro object for inclusion in a kit.
type PackedMacro struct {
	ID          string
	Name        string
	Description string
	Expansion   string `json:",omitempty"`
	Labels      []string
}

// PackMacro turns a regular Macro object into a PackedMacro.
func PackMacro(m types.Macro) (p PackedMacro) {
	p = PackedMacro{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Expansion:   m.Expansion,
		Labels:      m.Labels,
	}
	return
}

// Validate ensures that the fields of the PackedMacro are valid.
func (pm *PackedMacro) Validate() error {
	if pm.Name == `` {
		return errors.New("Missing macro name")
	} else if pm.ID == `` {
		return errors.New("Missing macro ID")
	} else if pm.Expansion == `` {
		return errors.New("Missing macro expansion")
	}
	return nil
}

// PackedResource is a stripped-down representation of a resource for inclusion in a kit.
type PackedResource struct {
	ID            string
	Name          string
	Description   string
	Labels        []string
	Size          uint64
	Hash          string
	Data          []byte
	ContentType   string
	FileExtension string
}

// PackResourceUpdate takes a ResourceUpdate (which contains a complete description of a
// resource, including its contents) and converts it into a PackedResource.
func PackResourceUpdate(ru types.ResourceUpdate) (p PackedResource) {
	p = PackedResource{
		ID:            ru.Metadata.ID,
		Name:          ru.Metadata.Name,
		Description:   ru.Metadata.Description,
		Labels:        ru.Metadata.Labels,
		Size:          ru.Metadata.Size,
		Hash:          ru.Metadata.Hash,
		Data:          ru.Bytes(),
		ContentType:   ru.Metadata.ContentType,
		FileExtension: ru.Metadata.FileExtension,
	}

	return
}

// Validate checks the contents of a PackedResource for validity.
func (p *PackedResource) Validate() error {
	if len(p.Name) == 0 {
		return errors.New("Invalid resource name")
	} else if p.Size != uint64(len(p.Data)) {
		return errors.New("mismatched data and data size")
	}
	if len(p.Data) == 0 && len(p.Hash) == 0 {
		return nil //short circuit, if its empty there is no hash
	}
	return nil
}

// PackedScheduledSearch is a stripped-down representation of a scheduled search for inclusion in a kit.
type PackedScheduledSearch struct {
	Name        string // the name of this scheduled search
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	SearchString    string `json:",omitempty"` // The actual search to run
	Duration        int64  `json:",omitempty"` // How many seconds back to search, MUST BE NEGATIVE
	ID              string // A unique ID for this scheduled search. Useful for detecting and handling upgrades.
	SearchReference string // Used if we're referencing a search query asset by ID instead of including the search directly.
}

// PackScheduledSearch converts a ScheduledSearch into a PackedScheduledSearch for inclusion in a kit.
func PackScheduledSearch(ss types.ScheduledSearch) (p PackedScheduledSearch) {
	p = PackedScheduledSearch{
		ID:              ss.ID,
		Name:            ss.Name,
		Description:     ss.Description,
		Schedule:        ss.Schedule,
		SearchString:    ss.SearchString,
		Duration:        ss.Duration,
		Labels:          ss.Labels,
		SearchReference: ss.SearchReference,
	}
	return
}

// Validate checks the fields of the PackedScheduledSearch.
func (pss *PackedScheduledSearch) Validate() error {
	if pss.Name == `` {
		return fmt.Errorf("Missing name")
	} else if pss.Schedule == `` {
		return errors.New("Missing schedule")
	} else if pss.SearchString != `` && pss.Duration >= 0 {
		return errors.New("Duration is invalid for SearchString, must be negative")
	} else if pss.SearchReference != "" {
		if pss.Duration >= 0 {
			return errors.New("Duration is invalid for SearchReference, must be negative")
		}
	}
	return nil
}

// Unpackage expands a PackedScheduledSearch into a ScheduledSearch.
func (pss *PackedScheduledSearch) Unpackage(uid int32, gids []int32) (ss types.ScheduledSearch) {
	ss.OwnerID = uid
	ss.Readers.GIDs = gids
	ss.Name = pss.Name
	ss.Description = pss.Description
	ss.Schedule = pss.Schedule
	ss.SearchString = pss.SearchString
	ss.Duration = pss.Duration
	ss.Labels = pss.Labels
	ss.ID = pss.ID
	ss.SearchReference = pss.SearchReference
	return
}

// PackedScheduledScript is a stripped-down representation of a scheduled script for inclusion in a kit.
type PackedScheduledScript struct {
	ID          string
	Name        string // the name of this scheduled script
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	Script         string `json:",omitempty"`
	ScriptLanguage types.ScriptLang
}

// PackScheduledScript converts a ScheduledScript into a PackedScheduledScript for inclusion in a kit.
func PackScheduledScript(ss types.ScheduledScript) (p PackedScheduledScript) {
	p = PackedScheduledScript{
		ID:             ss.ID,
		Name:           ss.Name,
		Description:    ss.Description,
		Schedule:       ss.Schedule,
		Labels:         ss.Labels,
		Script:         ss.Script,
		ScriptLanguage: ss.ScriptLanguage,
	}
	return
}

// Validate checks the fields of the PackedScheduledScript.
func (pss *PackedScheduledScript) Validate() error {
	if pss.Name == `` {
		return fmt.Errorf("Missing name")
	} else if pss.Schedule == `` {
		return errors.New("Missing schedule")
	} else if pss.Script == `` {
		return errors.New("Missing script")
	}
	return nil
}

// Unpackage expands a PackedScheduledScript into a ScheduledScript.
func (pss *PackedScheduledScript) Unpackage(uid int32, gids []int32) (ss types.ScheduledScript) {
	ss.ID = pss.ID
	ss.OwnerID = uid
	ss.Readers.GIDs = gids
	ss.Name = pss.Name
	ss.Description = pss.Description
	ss.Labels = pss.Labels
	ss.Schedule = pss.Schedule
	ss.Script = pss.Script
	ss.ScriptLanguage = pss.ScriptLanguage
	return
}

// PackedFlow is a stripped-down representation of a flow for inclusion in a kit.
type PackedFlow struct {
	ID          string
	Name        string // the name of this flow
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	Flow string
}

// PackFlow converts a Flow into a PackedFlow for inclusion in a kit.
func PackFlow(ss types.Flow) (p PackedFlow) {
	p = PackedFlow{
		ID:          ss.ID,
		Name:        ss.Name,
		Description: ss.Description,
		Schedule:    ss.Schedule,
		Labels:      ss.Labels,
		Flow:        ss.Flow,
	}
	return
}

// Validate checks the fields of the PackedFlow.
func (pss *PackedFlow) Validate() error {
	if pss.Name == `` {
		return fmt.Errorf("Missing name")
	} else if pss.Schedule == `` {
		return errors.New("Missing schedule")
	} else if pss.Flow == `` {
		return errors.New("Missing flow")
	}
	return nil
}

// Unpackage expands a PackedFlow into a Flow.
func (pss *PackedFlow) Unpackage(uid int32, gids []int32) (ss types.Flow) {
	ss.ID = pss.ID
	ss.OwnerID = uid
	ss.Readers.GIDs = gids
	ss.Name = pss.Name
	ss.Description = pss.Description
	ss.Labels = pss.Labels
	ss.Schedule = pss.Schedule
	ss.Flow = pss.Flow
	return
}

// PackedDashboard is a stripped-down type used for dashboards in kits.
type PackedDashboard struct {
	ID          string
	Name        string
	Description string
	Grid        types.DashboardGrid
	LinkZooming bool
	LiveUpdate  types.DashboardLiveUpdateSettings
	Searches    map[string]types.DashboardSearchable
	Tiles       map[string]types.DashboardTile
	Timeframe   types.DashboardTimeframe
	Labels      []string
}

// PackDashboard converts a Dashboard into a PackedDashboard.
func PackDashboard(d types.Dashboard) (pd PackedDashboard) {
	pd.ID = d.ID
	pd.Name = d.Name
	pd.Description = d.Description
	pd.Grid = d.Grid
	pd.LinkZooming = d.LinkZooming
	pd.LiveUpdate = d.LiveUpdate
	pd.Searches = d.Searches
	pd.Tiles = d.Tiles
	pd.Timeframe = d.Timeframe
	pd.Labels = d.Labels
	return
}

// Validate checks the fields of the PackedDashboard.
func (pd *PackedDashboard) Validate() error {
	if pd.Name == `` {
		return fmt.Errorf("Missing dashboard name")
	}
	return nil
}

// PackedPlaybook is a stripped-down representation of a playbook for inclusion in a kit.
type PackedPlaybook struct {
	ID            string
	Name          string
	Description   string
	Body          string
	Cover         string
	Banner        string
	AuthorName    string
	AuthorEmail   string
	AuthorCompany string
	AuthorURL     string
	Labels        []string
}

// PackPlaybook converts a Playbook into a PackedPlaybook for inclusion in a kit.
func PackPlaybook(pb types.Playbook) (p PackedPlaybook) {
	p = PackedPlaybook{
		ID:            pb.ID,
		Name:          pb.Name,
		Description:   pb.Description,
		Body:          pb.Body,
		Cover:         pb.Cover,
		Banner:        pb.Banner,
		AuthorName:    pb.AuthorName,
		AuthorEmail:   pb.AuthorEmail,
		AuthorCompany: pb.AuthorCompany,
		AuthorURL:     pb.AuthorURL,
		Labels:        pb.Labels,
	}
	return
}

// Validate checks the fields of the PackedPlaybook.
func (pp *PackedPlaybook) Validate() error {
	if pp.Name == `` {
		return fmt.Errorf("missing playbook name")
	}
	return nil
}

// Unpackage expands a PackedPlaybook into a Playbook.
func (pp *PackedPlaybook) Unpackage(uid int32, gids []int32) (pb types.Playbook) {
	pb.ID = pp.ID
	pb.OwnerID = uid
	pb.Readers.GIDs = gids
	pb.Name = pp.Name
	pb.Description = pp.Description
	pb.Body = pp.Body
	pb.Cover = pp.Cover
	pb.Banner = pp.Banner
	pb.Labels = pp.Labels
	pb.AuthorName = pp.AuthorName
	pb.AuthorEmail = pp.AuthorEmail
	pb.AuthorCompany = pp.AuthorCompany
	pb.AuthorURL = pp.AuthorURL
	return
}

type PackedActionable struct {
	ID          string
	Name        string
	Description string
	Data        types.ActionableContent
	Disabled    bool
	Labels      []string
}

func PackActionable(t types.Actionable) (put PackedActionable) {
	put.ID = t.ID
	put.Name = t.Name
	put.Description = t.Description
	put.Data = t.Contents
	put.Labels = t.Labels
	put.Disabled = t.Disabled
	return
}

// PackedAlert is a stripped-down representation of an alert for inclusion in a kit.
type PackedAlert struct {
	ID                 string
	Name               string
	Description        string
	Labels             []string
	Disabled           bool
	Consumers          []types.AlertConsumer
	Dispatchers        []types.AlertDispatcher
	IngestBlocked      bool
	MaxEvents          int
	SaveSearchDuration int32
	SaveSearchEnabled  bool
	Schemas            types.AlertSchemas
	TargetTag          string
	UserMetadata       map[string]interface{}
}

// PackAlert converts an Alert into a PackedAlert for inclusion in a kit.
func PackAlert(a types.Alert) (p PackedAlert) {
	p = PackedAlert{
		ID:                 a.ID,
		Name:               a.Name,
		Description:        a.Description,
		Labels:             a.Labels,
		Disabled:           a.Disabled,
		Consumers:          a.Consumers,
		Dispatchers:        a.Dispatchers,
		IngestBlocked:      a.IngestBlocked,
		MaxEvents:          a.MaxEvents,
		SaveSearchDuration: a.SaveSearchDuration,
		SaveSearchEnabled:  a.SaveSearchEnabled,
		Schemas:            a.Schemas,
		TargetTag:          a.TargetTag,
		UserMetadata:       a.UserMetadata,
	}
	return
}

// Validate checks the fields of the PackedAlert.
func (pa *PackedAlert) Validate() error {
	if pa.Name == `` {
		return errors.New("Missing alert name")
	}
	return nil
}

// PackedSavedQuery is a stripped-down representation of a saved query for inclusion in a kit.
type PackedSavedQuery struct {
	ID                 string
	Name               string
	Description        string
	Labels             []string
	Query              string
	SuggestedTimeframe types.SavedQueryTimeframe
}

// PackSavedQuery converts a SavedQuery into a PackedSavedQuery for inclusion in a kit.
func PackSavedQuery(sq types.SavedQuery) (p PackedSavedQuery) {
	p = PackedSavedQuery{
		ID:                 sq.ID,
		Name:               sq.Name,
		Description:        sq.Description,
		Labels:             sq.Labels,
		Query:              sq.Query,
		SuggestedTimeframe: sq.SuggestedTimeframe,
	}
	return
}

// Validate checks the fields of the PackedSavedQuery.
func (psq *PackedSavedQuery) Validate() error {
	if psq.Name == `` {
		return errors.New("Missing saved query name")
	} else if psq.Query == `` {
		return errors.New("Missing query")
	}
	return nil
}

// PackedAX is a stripped-down representation of an auto-extractor for inclusion in a kit.
type PackedAX struct {
	ID          string
	Name        string
	Description string
	Labels      []string
	Module      string
	Params      string `json:",omitempty"`
	Args        string `json:",omitempty"`
	Tags        []string
}

// PackAX converts an AX into a PackedAX for inclusion in a kit.
func PackAX(a types.AX) (p PackedAX) {
	p = PackedAX{
		ID:          a.ID,
		Name:        a.Name,
		Description: a.Description,
		Labels:      a.Labels,
		Module:      a.Module,
		Params:      a.Params,
		Args:        a.Args,
		Tags:        a.Tags,
	}
	return
}

// Validate checks the fields of the PackedAX.
func (pa *PackedAX) Validate() error {
	if pa.Name == `` {
		return errors.New("Missing auto-extractor name")
	} else if pa.Module == `` {
		return errors.New("Missing auto-extractor module")
	} else if len(pa.Tags) == 0 {
		return errors.New("Missing auto-extractor tags")
	}
	return nil
}

type PackedUserTemplate struct {
	ID          string
	Name        string
	Description string
	Query       string
	Variables   []types.TemplateVariable
	Labels      []string
}

func PackTemplate(t types.Template) (put PackedUserTemplate) {
	put.ID = t.ID
	put.Name = t.Name
	put.Description = t.Description
	put.Query = t.Query
	put.Variables = t.Variables
	put.Labels = t.Labels
	return
}
