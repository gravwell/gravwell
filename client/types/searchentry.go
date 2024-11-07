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
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

// Used for scripting and ingesting entries via the webserver.
type StringTagEntry struct {
	TS         time.Time
	Tag        string
	SRC        net.IP
	Data       []byte
	Enumerated []EnumeratedPair
}

type PrintableSearchEntry SearchEntry

// Search entry is the entry that makes it out of the search pipeline.
type SearchEntry struct {
	TS         entry.Timestamp
	SRC        net.IP
	Tag        entry.EntryTag
	Data       []byte
	Enumerated []EnumeratedPair
}

// EnumeratedPair is the string representation of enumerated values.
type EnumeratedPair struct {
	Name     string
	Value    string             `json:"ValueStr"`
	RawValue RawEnumeratedValue `json:"Value"`
}

type RawEnumeratedValue struct {
	Type uint16
	Data []byte
}

func (p PrintableSearchEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TS         entry.Timestamp
		SRC        net.IP
		Tag        entry.EntryTag
		Data       string
		Enumerated []EnumeratedPair
	}{
		TS:         p.TS,
		SRC:        p.SRC,
		Tag:        p.Tag,
		Data:       string(p.Data),
		Enumerated: p.Enumerated,
	})
}

// Return the string representation of an enumerated value in a SearchEntry.
func (se SearchEntry) GetEnumerated(name string) (val string, ok bool) {
	for _, v := range se.Enumerated {
		if v.Name == name {
			val = v.Value
			ok = true
			break
		}
	}
	return
}

// String implements the  Stringer interface
func (se SearchEntry) String() string {
	return string(se.Data) //basically a YOLO cast, maybe it prints, maybe it doesn't
}

// Return the string representation of an enumerated value in a StringTagEntry.
func (se StringTagEntry) GetEnumerated(name string) (val string, ok bool) {
	for _, v := range se.Enumerated {
		if v.Name == name {
			val = v.Value
			ok = true
			break
		}
	}
	return
}

// Return true if both SearchEntry objects are equal.
func (se SearchEntry) Equal(v SearchEntry) bool {
	if !se.TS.Equal(v.TS) || se.Tag != v.Tag || !se.SRC.Equal(v.SRC) {
		return false
	} else if !bytes.Equal(se.Data, v.Data) || !enumeratedEqual(se.Enumerated, v.Enumerated) {
		return false
	}
	return true
}

// String implements the fmt.Stringer
func (se StringTagEntry) String() string {
	return string(se.Data)
}

// Return true if both StringTagEntry objects are equal.
func (se StringTagEntry) Equal(v StringTagEntry) bool {
	if !se.TS.Equal(v.TS) || se.Tag != v.Tag || !se.SRC.Equal(v.SRC) {
		return false
	} else if !bytes.Equal(se.Data, v.Data) || !enumeratedEqual(se.Enumerated, v.Enumerated) {
		return false
	}
	return true
}

func enumeratedEqual(a, b []EnumeratedPair) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		} else if a[i].Value != b[i].Value {
			return false
		} else if a[i].RawValue.Type != b[i].RawValue.Type {
			return false
		} else if !bytes.Equal(a[i].RawValue.Data, b[i].RawValue.Data) {
			return false
		}
	}
	return true
}

func (ep EnumeratedPair) String() string {
	return fmt.Sprintf("%s:%s", ep.Name, ep.Value)
}

func (rev RawEnumeratedValue) String() string {
	return string(rev.Data)
}
