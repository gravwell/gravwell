/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package kits provides tools for interacting with kit archives directly. Most users
// will not need to deal with this.
package kits

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

const (
	Version            uint  = 3
	ManifestName             = `MANIFEST`
	ManifestSigName          = `SIGNATURE`
	maxManifestSize    int64 = 1024 * 1024
	maxManifestSigSize int64 = 8 * 1024

	_none           ItemType = 0
	Resource        ItemType = 1
	ScheduledSearch ItemType = 2
	Dashboard       ItemType = 3
	Extractor       ItemType = 4
	Pivot           ItemType = 5
	Template        ItemType = 6
	File            ItemType = 7
	Macro           ItemType = 8
	SearchLibrary   ItemType = 9
	License         ItemType = 10
	Playbook        ItemType = 11
	External        ItemType = 0xffff
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

type item struct {
	tp  ItemType
	nm  string
	ext string
}

var (
	itemSet = []item{
		item{tp: _none, nm: `NONE`, ext: ``},
		item{tp: External, nm: `external resource`, ext: `external_resource`},
		item{tp: Resource, nm: `resource`, ext: `resource`},
		item{tp: ScheduledSearch, nm: `scheduled search`, ext: `scheduled_search`},
		item{tp: Dashboard, nm: `dashboard`, ext: `dashboard`},
		item{tp: Extractor, nm: `autoextractor`, ext: `autoextractor`},
		item{tp: Pivot, nm: `pivot`, ext: `pivot`},
		item{tp: Template, nm: `template`, ext: `template`},
		item{tp: File, nm: `file`, ext: `file`},
		item{tp: Macro, nm: `macro`, ext: `macro`},
		item{tp: SearchLibrary, nm: `searchlibrary`, ext: `searchlibrary`},
		item{tp: License, nm: `license`, ext: `license`},
		item{tp: Playbook, nm: `playbook`, ext: `playbook`},
	}
)

type ItemType int

// Manifest contains information about a kit and a listing of items in the kit.
type Manifest struct {
	ID           string
	Name         string
	Desc         string
	Readme       string
	Version      uint
	MinVersion   types.CanonicalVersion
	MaxVersion   types.CanonicalVersion
	Icon         string
	Banner       string
	Cover        string
	Items        []Item
	Dependencies []types.KitDependency
	ConfigMacros []types.KitConfigMacro
}

// Item describes a single object within the kit. Note that it does not contain the actual body
// of the object, it just describes the item name and type, and gives a hash which can be used
// to verify item integrity.
type Item struct {
	Name string            //the name given to the item (script name, dashboard name, etc...)
	Type ItemType          //type specifier
	Hash [sha256.Size]byte //hash in the bundle
}

// Add includes an item in the manifest's item list.
func (m *Manifest) Add(item Item) error {
	//check type
	if !item.Type.Valid() {
		return ErrInvalidType
	}

	//ensure the item Filename and name/type don't already exist
	for i := range m.Items {
		if m.Items[i].Filename() == item.Filename() {
			return fmt.Errorf("File name %s already exists", item.Filename())
		}
		if m.Items[i].Type == item.Type && m.Items[i].Name == item.Name {
			return fmt.Errorf("The %s named %s already exists", item.Type, item.Name)
		}
	}
	m.Items = append(m.Items, item)
	return nil
}

func (m *Manifest) checkFileItem(val string) (bool, error) {
	//check that the argument is a UUID
	if _, err := uuid.Parse(val); err != nil {
		return false, err
	}

	//swing through the item list and ensure that we have an included file
	//with the appropriate UUID (basically if you are declaring an icon, we better have that file)
	for _, v := range m.Items {
		if v.Type != File {
			continue
		}
		if v.Name == val {
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

// TranslateType converts a string (e.g. "scheduled search") into an ItemType.
func TranslateType(tp string) (it ItemType, err error) {
	tp = strings.ToLower(strings.TrimSpace(tp))
	for _, v := range itemSet {
		if v.nm == tp {
			it = v.tp
			return
		}
	}
	err = fmt.Errorf("%s is an unknown type", tp)
	return
}

// TranslateExt translates a file extension (e.g. "dashboard") into an ItemType.
func TranslateExt(ext string) (it ItemType, err error) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	for _, v := range itemSet {
		if v.ext == ext {
			it = v.tp
			return
		}
	}
	err = fmt.Errorf("%s is an unknown type extension", ext)
	return
}

// String returns the human-friendly name for the item type. Note that these
// names may contain spaces e.g. "scheduled search".
func (it ItemType) String() string {
	for _, v := range itemSet {
		if it == v.tp {
			return v.nm
		}
	}
	return `UNKNOWN`
}

// Ext returns a file extension for the item type. These will not contain spaces.
func (it ItemType) Ext() string {
	for _, v := range itemSet {
		if v.tp == it {
			return v.ext
		}
	}
	return `UNKNOWN`
}

// Valid returns true if an ItemType is valid.
func (it ItemType) Valid() bool {
	return (it >= 0 && int(it) < (len(itemSet)-1)) || it == External
}

type itemstruct struct {
	Name string
	Type ItemType
	Hash string
}

// MarshalJSON packs an Item into JSON encoding.
func (i Item) MarshalJSON() ([]byte, error) {
	x := itemstruct{
		Name: i.Name,
		Type: i.Type,
		Hash: hex.EncodeToString(i.Hash[0:sha256.Size]),
	}
	return json.Marshal(x)
}

// UnmarshalJSON unpacks an Item from JSON encoding.
func (i *Item) UnmarshalJSON(v []byte) (err error) {
	var x itemstruct
	if err = json.Unmarshal(v, &x); err != nil {
		return
	}
	var hsh []byte
	if hsh, err = hex.DecodeString(x.Hash); err != nil {
		return
	}
	if len(hsh) != sha256.Size {
		return ErrInvalidHash
	}
	i.Name = x.Name
	i.Type = x.Type
	copy(i.Hash[0:sha256.Size], hsh)
	return
}

// Filename returns a suitable filename for the item.
func (i Item) Filename() string {
	return i.Name + `.` + i.Type.Ext()
}

// Equal returns true if the two items have matching names, types, and hashes.
func (i Item) Equal(ni Item) bool {
	return i.Name == ni.Name && i.Type == ni.Type && i.Hash == ni.Hash
}

// String returns the item's name for printing.
func (i Item) String() string {
	return i.Name
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
