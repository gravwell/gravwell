/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package kits provides tools for interacting with kit archives directly. Most users
// will not need to deal with this.
package kits

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gravwell/gravwell/v4/client/types"
)

const (
	Version            int   = 3
	ManifestName             = `MANIFEST`
	ManifestSigName          = `SIGNATURE`
	maxManifestSize    int64 = 1024 * 1024
	maxManifestSigSize int64 = 8 * 1024
)

var (
	ErrInvalidSignature = errors.New("Invalid manifest signature")
	ErrEmptyFileName    = errors.New("Empty file name")
	ErrEmptyName        = errors.New("Empty name")
	ErrEmptyContent     = errors.New("Empty data")
	ErrInvalidType      = errors.New("Invalid ItemType")
	ErrInvalidHash      = errors.New("Invalid file hash")
	ErrInvalidVersion   = errors.New("Invalid kit Version")
	ErrManifestMismatch = errors.New("Manifest does not match kit")
	ErrMissingManifest  = errors.New("Kit is missing a manifest")
	ErrMissingSignature = errors.New("Kit is missing a manifest signature")
)

// Manifest contains information about a kit and a listing of items in the kit.
type Manifest struct {
	ID           string
	Name         string
	Desc         string
	Readme       string
	Version      int
	MinVersion   types.CanonicalVersion
	MaxVersion   types.CanonicalVersion
	Icon         string
	Banner       string
	Cover        string
	Items        []types.KitItem
	Dependencies []types.KitDependency
	ConfigMacros []types.KitConfigMacro
}

// Add includes an item in the manifest's item list.
func (m *Manifest) Add(item types.KitItem) error {
	//check type
	if err := item.Validate(); err != nil {
		return err
	}

	//items are keyed by ID; ensure the ID isn't already present. Names are
	//not required to be unique (two items of the same type may share a name).
	for i := range m.Items {
		if m.Items[i].ID == item.ID {
			return fmt.Errorf("item with ID %s already exists", item.ID)
		}
	}
	m.Items = append(m.Items, item)
	return nil
}

func (m *Manifest) checkFileItem(val string) (bool, error) {
	//swing through the item list and ensure that we have an included file with the given name.
	for _, v := range m.Items {
		if v.Type != types.KitAssetFile {
			continue
		}
		if v.ID == val {
			return true, nil
		}
	}
	return false, nil
}

// SetIcon sets the icon field to point at an existing File item in the manifest.
func (m *Manifest) SetIcon(id string) error {
	if ok, err := m.checkFileItem(id); err != nil {
		return err
	} else if !ok {
		//if we hit here we don't actually have the icon
		return fmt.Errorf("Icon file %s is not included in the manifest.  Icons must be included as files", id)
	}
	//if we hit here, we are good
	m.Icon = id
	return nil
}

// SetCover sets the cover field to point at an existing File item in the manifest.
func (m *Manifest) SetCover(id string) error {
	if ok, err := m.checkFileItem(id); err != nil {
		return err
	} else if !ok {
		//if we hit here we don't actually have the cover
		return fmt.Errorf("Cover file %s is not included in the manifest.  Covers must be included as files", id)
	}
	//if we hit here, we are good
	m.Cover = id
	return nil
}

// SetBanner sets the banner field to point at an existing File item in the manifest.
func (m *Manifest) SetBanner(id string) error {
	if ok, err := m.checkFileItem(id); err != nil {
		return err
	} else if !ok {
		//if we hit here we don't actually have the cover
		return fmt.Errorf("Banner file %s is not included in the manifest.  Banners must be included as files", id)
	}
	//if we hit here, we are good
	m.Banner = id
	return nil
}

// CompatibleVersion checks the given version against the minimum and maximum versions
// specified in the manifest. It returns an error if the version is outside the range.
func (m *Manifest) CompatibleVersion(v types.CanonicalVersion) (err error) {
	if !v.Enabled() {
		return
	}
	if m.MinVersion.Enabled() && m.MinVersion.Compare(v) < 0 {
		err = fmt.Errorf("Invalid Gravwell version, at least %s required", m.MinVersion.String())
	} else if m.MaxVersion.Enabled() && m.MaxVersion.Compare(v) > 0 {
		err = fmt.Errorf("Invalid Gravwell version, max supported is %s", m.MaxVersion.String())
	}
	return
}

// Marshal returns a slice of bytes containing indented JSON representing the manifest.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, ``, "\t")
}

// Unmarshal unpacks JSON into the manifest.
func (m *Manifest) Unmarshal(v []byte) error {
	return json.Unmarshal(v, m)
}

// Load reads a JSON-encoded manifest from an io.Reader and unpacks it into the current manifest.
func (m *Manifest) Load(rdr io.Reader) error {
	return json.NewDecoder(rdr).Decode(m)
}

func writeAll(wtr io.Writer, b []byte) (err error) {
	var offset int
	var n int
	for offset < len(b) {
		if n, err = wtr.Write(b[offset:]); err != nil {
			return
		}
		offset += n
	}
	return
}
