/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/utils"
)

const (
	emptyContentType = `empty`
)

type Access struct {
	Global bool
	GIDs   []int32
}

func (a Access) Equal(b Access) bool {
	if a.Global != b.Global {
		return false
	}
	if !utils.Int32SlicesEqual(a.GIDs, b.GIDs) {
		return false
	}
	return true
}

// Thing is an object wrapper to store items in the datastore, a common class of blobs.
type Thing struct {
	UUID        uuid.UUID
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
	Contents    []byte

	Updated time.Time
	Synced  bool
}

type ThingHeader struct {
	ThingUUID   uuid.UUID `json:",omitempty"`
	UID         int32
	GIDs        []int32 `json:",omitempty"`
	Global      bool
	WriteAccess Access
}

func (t *Thing) Header() ThingHeader {
	if t.WriteAccess.GIDs == nil {
		t.WriteAccess.GIDs = []int32{}
	}
	return ThingHeader{
		ThingUUID:   t.UUID,
		UID:         t.UID,
		GIDs:        t.GIDs,
		Global:      t.Global,
		WriteAccess: t.WriteAccess,
	}
}

func (t *Thing) Encode() ([]byte, error) {
	bb := bytes.NewBuffer(nil)
	if err := gob.NewEncoder(bb).Encode(t); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
}

func (t *Thing) Decode(v []byte) error {
	bb := bytes.NewBuffer(v)
	if err := gob.NewDecoder(bb).Decode(t); err != nil {
		return err
	}
	return nil
}

func (t *Thing) EncodeContents(obj interface{}) error {
	bb := bytes.NewBuffer(nil)
	if err := gob.NewEncoder(bb).Encode(obj); err != nil {
		return err
	}
	t.Contents = bb.Bytes()
	return nil
}

func (t *Thing) DecodeContents(obj interface{}) error {
	bb := bytes.NewBuffer(t.Contents)
	if err := gob.NewDecoder(bb).Decode(obj); err != nil {
		return err
	}
	return nil
}

// TemplateContents is what goes in the template's Contents field. This is entirely
// the domain of the GUI.
type TemplateContents struct {
	Query     string             `json:"query,omitempty"`
	Variables []TemplateVariable `json:"variables"`
}

type TemplateVariable struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Required     bool   `json:"required"`
	DefaultValue string `json:"defaultValue"`
	PreviewValue string `json:"previewValue"`
}

// UserTemplate is what is stored in the "thing" object, it is encoded into Contents
type UserTemplate struct {
	GUID        uuid.UUID
	Name        string
	Description string
	Contents    TemplateContents
	Labels      []string
}

func (t UserTemplate) WireUserTemplate(thing Thing) WireUserTemplate {
	return WireUserTemplate{
		GUID:        t.GUID,
		ThingHeader: thing.Header(),
		Updated:     thing.Updated,
		Labels:      t.Labels,
		Name:        t.Name,
		Description: t.Description,
		Contents:    t.Contents,
	}
}

// WireUserTemplate is constructed from the UserTemplate and the details in the Thing
// struct. This is what we send to the user via the API.
type WireUserTemplate struct {
	ThingHeader
	GUID        uuid.UUID
	Name        string
	Description string
	Contents    TemplateContents
	Updated     time.Time
	Labels      []string
}

func (w WireUserTemplate) UserTemplate() (ut UserTemplate) {
	return UserTemplate{
		GUID:        w.GUID,
		Name:        w.Name,
		Description: w.Description,
		Contents:    w.Contents,
		Labels:      w.Labels,
	}
}

func (w WireUserTemplate) Thing() (t Thing, err error) {
	t.UUID = w.ThingUUID
	t.UID = w.UID
	t.GIDs = w.GIDs
	t.Global = w.Global
	t.WriteAccess = w.WriteAccess
	if t.WriteAccess.GIDs == nil {
		t.WriteAccess.GIDs = []int32{}
	}
	t.Updated = w.Updated
	//do not set the synced value
	err = t.EncodeContents(w.UserTemplate())
	return
}

// PackedUserTemplate type used for templates in packages
type PackedUserTemplate struct {
	UUID        string
	Name        string
	Description string
	Data        TemplateContents
	Labels      []string
}

type oldTemplateContents struct {
	Query               string `json:"query"`
	Variable            string `json:"variable"`
	VariableLabel       string `json:"variableLabel"`
	VariableDescription string `json:"variableDescription"`
	Required            bool   `json:"required"`
	TestValue           string `json:"testValue"`
}

type newPackedUserTemplate PackedUserTemplate
type oldPackedUserTemplate struct {
	UUID        string
	Name        string
	Description string
	Data        oldTemplateContents
	Labels      []string
}

func (t UserTemplate) Pack() (put PackedUserTemplate) {
	if put.UUID = t.GUID.String(); put.UUID == `` {
		put.UUID = uuid.New().String()
	}
	put.Name = t.Name
	put.Description = t.Description
	put.Data = t.Contents
	put.Labels = t.Labels
	return
}

func (put *PackedUserTemplate) UnmarshalJSON(data []byte) error {
	// First try the current type
	var nt newPackedUserTemplate
	if err := json.Unmarshal(data, &nt); err != nil {
		// something is majorly wrong.
		return err
	}
	if len(nt.Data.Variables) > 0 {
		*put = PackedUserTemplate(nt)
		return nil
	}
	// If there are no variables in the result, try the old way.
	var ot oldPackedUserTemplate
	if err := json.Unmarshal(data, &ot); err != nil {
		// something is majorly wrong.
		return err
	}
	put.UUID = ot.UUID
	put.Name = ot.Name
	put.Description = ot.Description
	put.Labels = ot.Labels
	put.Data.Query = ot.Data.Query
	put.Data.Variables = []TemplateVariable{{
		Name:         ot.Data.Variable,
		Label:        ot.Data.VariableLabel,
		Description:  ot.Data.VariableDescription,
		Required:     ot.Data.Required,
		DefaultValue: ot.Data.TestValue,
	}}
	return nil
}

func (put *PackedUserTemplate) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		UUID        string
		Name        string
		Description string
	}{
		UUID:        put.UUID,
		Name:        put.Name,
		Description: put.Description,
	})
	return json.RawMessage(b), err
}

// Pivot is what is stored in the "thing" object, it is encoded into Contents
type Pivot struct {
	GUID        uuid.UUID
	Name        string
	Description string
	Contents    RawObject
	Labels      []string
	Disabled    bool
}

func (t Pivot) WirePivot(thing Thing) WirePivot {
	return WirePivot{
		GUID:        t.GUID,
		ThingHeader: thing.Header(),
		Updated:     thing.Updated,
		Name:        t.Name,
		Description: t.Description,
		Contents:    t.Contents,
		Labels:      t.Labels,
		Disabled:    t.Disabled,
	}
}

// WirePivot is constructed from the Pivot and the details in the Thing
// struct. This is what we send to the user via the API.
type WirePivot struct {
	ThingHeader
	GUID        uuid.UUID
	Name        string
	Description string
	Updated     time.Time
	Contents    RawObject
	Labels      []string
	Disabled    bool
}

// Thing Generates an encoded Thing from the WirePivot structure
func (wp WirePivot) Thing() (t Thing, err error) {
	t.UUID = wp.ThingUUID
	t.UID = wp.UID
	t.GIDs = wp.GIDs
	t.Global = wp.Global
	t.WriteAccess = wp.WriteAccess
	if t.WriteAccess.GIDs == nil {
		t.WriteAccess.GIDs = []int32{}
	}
	t.Updated = wp.Updated
	//do not set the synced value

	err = t.EncodeContents(wp.Pivot())
	return
}

// Pivot creates a Pivot structure from the WiredPivot
func (wp WirePivot) Pivot() Pivot {
	return Pivot{
		GUID:        wp.GUID,
		Name:        wp.Name,
		Description: wp.Description,
		Contents:    wp.Contents,
		Labels:      wp.Labels,
		Disabled:    wp.Disabled,
	}
}

type PackedPivot struct {
	UUID        string
	Name        string
	Description string
	Data        RawObject
	Labels      []string
}

func (t Pivot) Pack() (put PackedPivot) {
	if put.UUID = t.GUID.String(); put.UUID == `` {
		put.UUID = uuid.New().String()
	}
	put.Name = t.Name
	put.Description = t.Description
	put.Data = t.Contents
	put.Labels = t.Labels
	return
}

func (put *PackedPivot) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		UUID        string
		Name        string
		Description string
	}{
		UUID:        put.UUID,
		Name:        put.Name,
		Description: put.Description,
	})
	return json.RawMessage(b), err
}

// UserFile is what is actually stored in the thing object, it is encoded into contents
type UserFile struct {
	GUID     uuid.UUID
	Name     string
	Desc     string
	Contents []byte `json:",omitempty"`
	Labels   []string
}

type WireUserFile struct {
	ThingHeader
	UserFile
	Updated time.Time
}

func (w WireUserFile) Thing() (t Thing, err error) {
	t.UUID = w.ThingUUID
	t.UID = w.UID
	t.GIDs = w.GIDs
	t.Global = w.Global
	t.WriteAccess = w.WriteAccess
	if t.WriteAccess.GIDs == nil {
		t.WriteAccess.GIDs = []int32{}
	}
	t.Updated = w.Updated
	//do not set the synced value
	err = t.EncodeContents(w.UserFile)
	return
}

// UserFileDetails is a structure that is used to relay additional ownership information about a UserFile object
// This structure is populated via the things metadata, and does not contain any of the contents
type UserFileDetails struct {
	GUID        uuid.UUID
	ThingUUID   uuid.UUID
	UID         int32
	GIDs        []int32
	Global      bool
	WriteAccess Access
	Size        int64  //size of the file
	Type        string //content type as determined by the http content type detector
	Name        string
	Desc        string
	Updated     time.Time
	Labels      []string
}

func (ufd *UserFileDetails) String() string {
	if ufd.Name != `` {
		return ufd.Name
	}
	return ufd.GUID.String()
}

func (uf *UserFile) Info() (sz int64, tp string) {
	if sz = int64(len(uf.Contents)); sz > 0 {
		tp = http.DetectContentType(uf.Contents)
	} else {
		tp = emptyContentType
	}
	return
}

func (uf *UserFile) JSONMetadata() (json.RawMessage, error) {
	st := &struct {
		UUID        string
		Name        string
		Description string
		Size        int64
		ContentType string
	}{
		UUID:        uf.GUID.String(),
		Name:        uf.Name,
		Description: uf.Desc,
	}
	st.Size, st.ContentType = uf.Info()
	b, err := json.Marshal(st)
	return json.RawMessage(b), err
}

// WireSearchLibrary is what we actually send back and forth over the API
type WireSearchLibrary struct {
	ThingHeader
	SearchLibrary
	Can     Actions
	Updated time.Time
}

type Actions struct {
	Delete bool
	Modify bool
	Share  bool
}

func (wsl WireSearchLibrary) Thing() (t Thing, err error) {
	t.UUID = wsl.ThingUUID
	t.UID = wsl.UID
	t.GIDs = wsl.GIDs
	t.Global = wsl.Global
	t.WriteAccess = wsl.WriteAccess
	if t.WriteAccess.GIDs == nil {
		t.WriteAccess.GIDs = []int32{}
	}
	t.Updated = wsl.Updated

	err = t.EncodeContents(wsl.SearchLibrary)
	return
}

// SearchLibrary is a structure to store a search string and optional set of info
// The GUI uses this to build up a search library with info about a search
type SearchLibrary struct {
	Name        string
	Description string
	Query       string `json:",omitempty"`
	GUID        uuid.UUID
	Labels      []string  `json:",omitempty"`
	Metadata    RawObject `json:",omitempty"`
}

func (sl SearchLibrary) Equal(other SearchLibrary) (ok bool) {
	ok = sl.Name == other.Name && sl.Description == other.Description && sl.Query == other.Query && sl.GUID == other.GUID
	if !ok {
		return
	}
	if ok = bytes.Equal(sl.Metadata, other.Metadata); !ok {
		return
	}
	if ok = len(sl.Labels) == len(other.Labels); !ok {
		return
	}
	for i := range sl.Labels {
		if ok = (sl.Labels[i] == other.Labels[i]); !ok {
			return
		}
	}
	return
}

func (sl SearchLibrary) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		Name        string
		Description string
		Query       string
		UUID        string
	}{
		Name:        sl.Name,
		Description: sl.Description,
		Query:       sl.Query,
		UUID:        sl.GUID.String(),
	})
	return json.RawMessage(b), err
}
