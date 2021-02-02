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
)

const (
	emptyContentType = `empty`
)

// Things are stored in the datastore, a common class of blobs.
type Thing struct {
	UUID     uuid.UUID
	UID      int32
	GIDs     []int32
	Global   bool
	Contents []byte

	Updated time.Time
	Synced  bool
}

type ThingHeader struct {
	ThingUUID uuid.UUID `json:",omitempty"`
	UID       int32
	GIDs      []int32 `json:",omitempty"`
	Global    bool
}

func (t *Thing) Header() ThingHeader {
	return ThingHeader{
		ThingUUID: t.UUID,
		UID:       t.UID,
		GIDs:      t.GIDs,
		Global:    t.Global,
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

// UserTemplate is what is stored in the "thing" object, it is encoded into Contents
type UserTemplate struct {
	GUID        uuid.UUID
	Name        string
	Description string
	Contents    RawObject
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
	Contents    RawObject
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
	t.Updated = time.Now()
	//do not set the synced value
	err = t.EncodeContents(w.UserTemplate())
	return
}

// type used for templates in packages
type PackedUserTemplate struct {
	UUID        string
	Name        string
	Description string
	Data        RawObject
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

func (pivot Pivot) WirePivot(thing Thing) WirePivot {
	return WirePivot{
		GUID:        pivot.GUID,
		ThingHeader: thing.Header(),
		Updated:     thing.Updated,
		Name:        pivot.Name,
		Description: pivot.Description,
		Contents:    pivot.Contents,
		Labels:      pivot.Labels,
		Disabled:    pivot.Disabled,
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

	t.Updated = time.Now()
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

// type used for pivots in packages
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
	Contents []byte
	Labels   []string
}

type WireUserFile struct {
	ThingHeader
	UserFile
}

func (w WireUserFile) Thing() (t Thing, err error) {
	t.UUID = w.ThingUUID
	t.UID = w.UID
	t.GIDs = w.GIDs
	t.Global = w.Global
	t.Updated = time.Now()
	//do not set the synced value
	err = t.EncodeContents(w.UserFile)
	return
}

// UserFileDetails is a structure that is used to relay additional ownership information about a UserFile object
// This structure is populated via the things metadata, and does not contain any of the contents
type UserFileDetails struct {
	GUID      uuid.UUID
	ThingUUID uuid.UUID
	UID       int32
	GIDs      []int32
	Global    bool
	Size      int64  //size of the file
	Type      string //content type as determined by the http content type detector
	Name      string
	Desc      string
	Updated   time.Time
	Labels    []string
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
}

func (wsl WireSearchLibrary) Thing() (t Thing, err error) {
	t.UUID = wsl.ThingUUID
	t.UID = wsl.UID
	t.GIDs = wsl.GIDs
	t.Global = wsl.Global
	t.Updated = time.Now()

	err = t.EncodeContents(wsl.SearchLibrary)
	return
}

// SearchLibrary is a structure to store a search string and optional set of info
// The GUI uses this to build up a search library with info about a search
type SearchLibrary struct {
	Name        string
	Description string
	Query       string
	GUID        uuid.UUID
	Labels      []string  `json:",omitempty"`
	Metadata    RawObject `json:",omitempty"`
}

func (psl SearchLibrary) Equal(other SearchLibrary) (ok bool) {
	ok = psl.Name == other.Name && psl.Description == other.Description && psl.Query == other.Query && psl.GUID == other.GUID
	if !ok {
		return
	}
	if ok = bytes.Equal(psl.Metadata, other.Metadata); !ok {
		return
	}
	if ok = len(psl.Labels) == len(other.Labels); !ok {
		return
	}
	for i := range psl.Labels {
		if ok = (psl.Labels[i] == other.Labels[i]); !ok {
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
