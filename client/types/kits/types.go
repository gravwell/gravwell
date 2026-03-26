/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package kits

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v4/client/types"

	"github.com/google/uuid"
)

// PackedFile is a stripped-down representation of a file for inclusion in a kit.
type PackedFile struct {
	ID            string
	VersionNumber int // file version #, increment at each Write
	Name          string
	Description   string
	Labels        []string
	Size          uint64
	Hash          []byte
	Data          []byte
}

// PackFileFull takes a FileFull (which contains a complete description of a file, including contents) and converts it into a PackedFile.
func PackFileFull(ff types.FileFull) (p PackedFile) {
	p = PackedFile{
		ID:            ff.ID,
		VersionNumber: ff.Version,
		Name:          ff.Name,
		Description:   ff.Description,
		Labels:        ff.Labels,
		Size:          ff.Size,
		Data:          ff.Content,
	}
	if ff.File.Hash != "" {
		p.Hash, _ = hex.DecodeString(ff.File.Hash)
	}
	if p.VersionNumber == 0 {
		p.VersionNumber = 1
	}
	return
}

// Validate checks the contents of a PackedFile for validity.
func (p *PackedFile) Validate() error {
	if p.VersionNumber <= 0 {
		return errors.New("Invalid version number")
	} else if len(p.Name) == 0 {
		return errors.New("Invalid file name")
	} else if p.Size != uint64(len(p.Data)) {
		return errors.New("mismatched data and data size")
	}
	if len(p.Data) == 0 && len(p.Hash) == 0 {
		return nil //short circuit, if its empty there is no hash
	}
	hsh := md5.Sum(p.Data)
	if len(hsh) != len(p.Hash) {
		return errors.New("invalid data hash")
	} else {
		for i := range p.Hash {
			if p.Hash[i] != hsh[i] {
				return errors.New("Bad data hash")
			}
		}
	}
	return nil
}

// JSONMetadata returns additional information about the file.
func (p *PackedFile) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		VersionNumber int
		Name          string
		Description   string
		Size          uint64
		Labels        []string
	}{
		VersionNumber: p.VersionNumber,
		Name:          p.Name,
		Description:   p.Description,
		Size:          p.Size,
		Labels:        p.Labels,
	})
	return json.RawMessage(b), err
}

// PackedMacro is a stripped-down representation of a macro object for inclusion in a kit.
type PackedMacro struct {
	Name        string
	Description string
	Expansion   string `json:",omitempty"`
	Labels      []string
}

// PackSearchMacro turns a regular SearchMacro object into a PackedMacro.
func PackSearchMacro(m *types.Macro) (p PackedMacro) {
	p = PackedMacro{
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
	} else if pm.Expansion == `` {
		return errors.New("Missing macro expansion")
	}
	return nil
}

// JSONMetadata returns additional information about the macro.
func (pm *PackedMacro) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name        string
		Description string
		Expansion   string
	}{
		Name:        pm.Name,
		Description: pm.Description,
		Expansion:   pm.Expansion,
	})
	return json.RawMessage(b), err
}

// PackedResource is a stripped-down representation of a resource for inclusion in a kit.
type PackedResource struct {
	ID            string
	VersionNumber int // resource version #, increment at each Write
	ResourceName  string
	Description   string
	Labels        []string
	Size          uint64
	Hash          []byte
	Data          []byte
	ContentType   string
}

// PackResourceUpdate takes a ResourceUpdate (which contains a complete description of a
// resource, including its contents) and converts it into a PackedResource.
func PackResourceUpdate(ru types.ResourceUpdate) (p PackedResource) {
	p = PackedResource{
		ID:            ru.Metadata.ID,
		VersionNumber: ru.Metadata.Version,
		ResourceName:  ru.Metadata.Name,
		Description:   ru.Metadata.Description,
		Labels:        ru.Metadata.Labels,
		Size:          ru.Metadata.Size,
		Data:          ru.Bytes(),
		ContentType:   ru.Metadata.ContentType,
	}
	if ru.Metadata.Hash != "" {
		p.Hash, _ = hex.DecodeString(ru.Metadata.Hash)
	}
	if p.VersionNumber == 0 {
		p.VersionNumber = 1
	}
	return
}

// Validate checks the contents of a PackedResource for validity.
func (p *PackedResource) Validate() error {
	if p.VersionNumber <= 0 {
		return errors.New("Invalid version number")
	} else if len(p.ResourceName) == 0 {
		return errors.New("Invalid resource name")
	} else if p.Size != uint64(len(p.Data)) {
		return errors.New("mismatched data and data size")
	}
	if len(p.Data) == 0 && len(p.Hash) == 0 {
		return nil //short circuit, if its empty there is no hash
	}
	hsh := md5.Sum(p.Data)
	if len(hsh) != len(p.Hash) {
		return errors.New("invalid data hash")
	} else {
		for i := range p.Hash {
			if p.Hash[i] != hsh[i] {
				return errors.New("Bad data hash")
			}
		}
	}
	return nil
}

// JSONMetadata returns additional information about the resource.
func (p *PackedResource) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		VersionNumber int
		ResourceName  string
		Description   string
		Size          uint64
		Labels        []string
	}{
		VersionNumber: p.VersionNumber,
		ResourceName:  p.ResourceName,
		Description:   p.Description,
		Size:          p.Size,
		Labels:        p.Labels,
	})
	return json.RawMessage(b), err
}

// PackedScheduledSearch is a stripped-down representation of a scheduled search for inclusion in a kit.
type PackedScheduledSearch struct {
	Name        string // the name of this scheduled search
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	SearchString           string `json:",omitempty"` // The actual search to run
	Duration               int64  `json:",omitempty"` // How many seconds back to search, MUST BE NEGATIVE
	DefaultDeploymentRules types.ScriptDeployConfig
	ID                     string // A unique ID for this scheduled search. Useful for detecting and handling upgrades.
	SearchReference        string // Used if we're referencing a search query asset by ID instead of including the search directly.
}

// PackScheduledSearch converts a ScheduledSearch into a PackedScheduledSearch for inclusion in a kit.
func PackScheduledSearch(ss *types.ScheduledSearch) (p PackedScheduledSearch) {
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

// JSONMetadata returns additional info about the PackedScheduledSearch in JSON format.
func (pss *PackedScheduledSearch) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name                   string
		Description            string
		Schedule               string
		SearchString           string `json:",omitempty"`
		SearchReference        string `json:",omitempty"`
		Duration               int64  `json:",omitempty"`
		DefaultDeploymentRules types.ScriptDeployConfig
	}{
		Name:                   pss.Name,
		Description:            pss.Description,
		Schedule:               pss.Schedule,
		SearchString:           pss.SearchString,
		SearchReference:        pss.SearchReference,
		Duration:               pss.Duration,
		DefaultDeploymentRules: pss.DefaultDeploymentRules,
	})
	return json.RawMessage(b), err
}

// PackedScheduledScript is a stripped-down representation of a scheduled script for inclusion in a kit.
type PackedScheduledScript struct {
	ID          string
	Name        string // the name of this scheduled script
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	Script                 string `json:",omitempty"`
	ScriptLanguage         types.ScriptLang
	DefaultDeploymentRules types.ScriptDeployConfig
}

// PackScheduledScript converts a ScheduledScript into a PackedScheduledScript for inclusion in a kit.
func PackScheduledScript(ss *types.ScheduledScript) (p PackedScheduledScript) {
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

// JSONMetadata returns additional info about the PackedScheduledScript in JSON format.
func (pss *PackedScheduledScript) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name                   string
		Description            string
		Schedule               string
		DefaultDeploymentRules types.ScriptDeployConfig
	}{
		Name:                   pss.Name,
		Description:            pss.Description,
		Schedule:               pss.Schedule,
		DefaultDeploymentRules: pss.DefaultDeploymentRules,
	})
	return json.RawMessage(b), err
}

// PackedFlow is a stripped-down representation of a flow for inclusion in a kit.
type PackedFlow struct {
	ID          string
	Name        string // the name of this flow
	Description string // freeform description
	Labels      []string
	Schedule    string // when to run: a cron spec

	Flow                   string
	DefaultDeploymentRules types.ScriptDeployConfig
}

// PackFlow converts a Flow into a PackedFlow for inclusion in a kit.
func PackFlow(ss *types.Flow) (p PackedFlow) {
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

// JSONMetadata returns additional info about the PackedFlow in JSON format.
func (pss *PackedFlow) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name                   string
		Description            string
		Schedule               string
		DefaultDeploymentRules types.ScriptDeployConfig
	}{
		Name:                   pss.Name,
		Description:            pss.Description,
		Schedule:               pss.Schedule,
		DefaultDeploymentRules: pss.DefaultDeploymentRules,
	})
	return json.RawMessage(b), err
}

// PackedDashboard is a stripped-down type used for dashboards in kits.
type PackedDashboard struct {
	UUID        string
	Name        string
	Description string
	Data        types.RawObject
	Labels      []string
}

// PackDashboard converts a Dashboard into a PackedDashboard.
func PackDashboard(d types.Dashboard) (pd PackedDashboard) {
	if pd.UUID = d.GUID; pd.UUID == `` {
		pd.UUID = uuid.New().String()
	}
	pd.Name = d.Name
	pd.Description = d.Description
	pd.Data = d.Data
	pd.Labels = d.Labels
	return

}

// Validate checks the fields of the PackedDashboard.
func (pd *PackedDashboard) Validate() error {
	if pd.Name == `` {
		return fmt.Errorf("Missing dashboard name")
	} else if len(pd.Data) == 0 {
		return fmt.Errorf("Empty dashboard")
	}
	return nil
}

// JSONMetadata returns additional info about the PackedDashboard in JSON format.
func (pd *PackedDashboard) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		UUID        string
		Name        string
		Description string
	}{
		UUID:        pd.UUID,
		Name:        pd.Name,
		Description: pd.Description,
	})
	return json.RawMessage(b), err
}
