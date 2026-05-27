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

type Actions struct {
	Delete bool
	Modify bool
	Share  bool
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
// TODO #761 move this into kits/types.go
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
