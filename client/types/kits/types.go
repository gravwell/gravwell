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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

// PackedMacro is a stripped-down representation of a macro object for inclusion in a kit.
type PackedMacro struct {
	Name        string
	Description string
	Expansion   string
	Labels      []string
}

// PackSearchMacro turns a regular SearchMacro object into a PackedMacro.
func PackSearchMacro(m *types.SearchMacro) (p PackedMacro) {
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
	VersionNumber int // resource version #, increment at each Write
	ResourceName  string
	Description   string
	Size          uint64
	Hash          []byte
	Data          []byte
}

// PackResourceUpdate takes a ResourceUpdate (which contains a complete description of a
// resource, including its contents) and converts it into a PackedResource.
func PackResourceUpdate(ru types.ResourceUpdate) (p PackedResource) {
	p = PackedResource{
		VersionNumber: ru.Metadata.VersionNumber,
		ResourceName:  ru.Metadata.ResourceName,
		Description:   ru.Metadata.Description,
		Size:          ru.Metadata.Size,
		Hash:          ru.Metadata.Hash,
		Data:          ru.Data,
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
	}{
		VersionNumber: p.VersionNumber,
		ResourceName:  p.ResourceName,
		Description:   p.Description,
		Size:          p.Size,
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
	Script                 string `json:",omitempty"` // If set, execute the contents rather than running SearchString
	DefaultDeploymentRules types.ScriptDeployConfig
}

// PackScheduledSearch converts a ScheduledSearch into a PackedScheduledSearch for inclusion in a kit.
func PackScheduledSearch(ss *types.ScheduledSearch) (p PackedScheduledSearch) {
	p = PackedScheduledSearch{
		Name:         ss.Name,
		Description:  ss.Description,
		Schedule:     ss.Schedule,
		SearchString: ss.SearchString,
		Duration:     ss.Duration,
		Script:       ss.Script,
		Labels:       ss.Labels,
	}
	return
}

// TypeName returns either "script" or "search" depending on the type of the PackedScheduledSearch.
func (pss *PackedScheduledSearch) TypeName() string {
	if len(pss.Script) > 0 {
		return "script"
	}
	return "search"
}

// Validate checks the fields of the PackedScheduledSearch.
func (pss *PackedScheduledSearch) Validate() error {
	if pss.Name == `` {
		return fmt.Errorf("Missing scheduled %v name", pss.TypeName())
	} else if pss.Schedule == `` {
		return errors.New("Missing schedule")
	} else if pss.SearchString != `` && pss.Script != `` {
		return errors.New("SearchString and Script are both populated")
	} else if pss.SearchString != `` && pss.Duration >= 0 {
		return errors.New("Duration is invalid for SearchString, must be negative")
	}
	return nil
}

// Unpackage expands a PackedScheduledSearch into a ScheduledSearch.
func (pss *PackedScheduledSearch) Unpackage(uid, gid int32) (ss types.ScheduledSearch) {
	ss.Owner = uid
	if gid != 0 {
		ss.Groups = []int32{gid}
	}
	ss.Name = pss.Name
	ss.Description = pss.Description
	ss.Schedule = pss.Schedule
	ss.SearchString = pss.SearchString
	ss.Duration = pss.Duration
	ss.Script = pss.Script
	ss.Labels = pss.Labels
	return
}

// JSONMetadata returns additional info about the PackedScheduledSearch in JSON format.
func (pss *PackedScheduledSearch) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name                   string
		Description            string
		Schedule               string
		SearchString           string `json:",omitempty"`
		Duration               int64  `json:",omitempty"`
		Script                 string `json:",omitempty"`
		DefaultDeploymentRules types.ScriptDeployConfig
	}{
		Name:                   pss.Name,
		Description:            pss.Description,
		Schedule:               pss.Schedule,
		SearchString:           pss.SearchString,
		Duration:               pss.Duration,
		Script:                 pss.Script,
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
