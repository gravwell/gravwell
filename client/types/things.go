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
	otypes "github.com/gravwell/gravwell/v3/client/types"
	"github.com/gravwell/gravwell/v4/utils"
)

const (
	emptyContentType = `empty`
)

type Access struct {
	Global bool
	GIDs   []int32
}

func (a Access) GetOld() otypes.Access {
	return otypes.Access{
		Global: a.Global,
		GIDs:   a.GIDs,
	}
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

// PackedUserTemplate type used for templates in packages
type PackedUserTemplate struct {
	ID          string
	Name        string
	Description string
	Query       string
	Variables   []TemplateVariable
	Labels      []string
}

func (t Template) Pack() (put PackedUserTemplate) {
	put.ID = t.ID
	put.Name = t.Name
	put.Description = t.Description
	put.Query = t.Query
	put.Variables = t.Variables
	put.Labels = t.Labels
	return
}

func (put *PackedUserTemplate) JSONMetadata() (json.RawMessage, error) {
	b, err := json.Marshal(&struct {
		ID          string
		Name        string
		Description string
	}{
		ID:          put.ID,
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

type Actions struct {
	Delete bool
	Modify bool
	Share  bool
}
