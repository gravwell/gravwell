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

type PackedMacro struct {
	Name        string
	Description string
	Expansion   string
	Labels      []string
}

func PackSearchMacro(m *types.SearchMacro) (p PackedMacro) {
	p = PackedMacro{
		Name:        m.Name,
		Description: m.Description,
		Expansion:   m.Expansion,
		Labels:      m.Labels,
	}
	return
}

func (pm *PackedMacro) Validate() error {
	if pm.Name == `` {
		return errors.New("Missing macro name")
	} else if pm.Expansion == `` {
		return errors.New("Missing macro expansion")
	}
	return nil
}

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

type PackedResource struct {
	VersionNumber int // resource version #, increment at each Write
	ResourceName  string
	Description   string
	Size          uint64
	Hash          []byte
	Data          []byte
}

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

func (pss *PackedScheduledSearch) TypeName() string {
	if len(pss.Script) > 0 {
		return "script"
	}
	return "search"
}

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

// type used for dashboards in packages
type PackedDashboard struct {
	UUID        string
	Name        string
	Description string
	Data        types.RawObject
	Labels      []string
}

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
