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
	"net"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

// Used for scripting and ingesting entries via the webserver.
type StringTagEntry struct {
	TS         time.Time
	Tag        string
	SRC        net.IP
	Data       []byte
	Enumerated []EnumeratedPair
}

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
		}
	}
	return true
}
