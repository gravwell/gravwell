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
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
)

func TestTimeRangeEncodeDecode(t *testing.T) {
	ts := entry.Now()
	tr := TimeRange{
		StartTS: ts,
		EndTS:   ts.Add(time.Hour),
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(tr); err != nil {
		t.Fatal(err)
	}
	var ttr TimeRange
	if err := json.NewDecoder(bb).Decode(&ttr); err != nil {
		t.Fatal(err)
	}

	if !tr.StartTS.Equal(ttr.StartTS) {
		t.Fatal("StartTS not equal")
	}
	if !tr.EndTS.Equal(ttr.EndTS) {
		t.Fatal("EndTS not equal")
	}
}

func TestEmptyTimeRangeEncodeDecode(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	var ttr TimeRange
	if err := ttr.DecodeJSON(bb); err != nil {
		t.Fatal(err)
	}
	if !ttr.IsEmpty() {
		t.Fatal("Not empty on empty decode")
	}
}

func TestSearchEntryEncodeDecode(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	//test without any enumerated values
	s := SearchEntry{
		TS:   entry.FromStandard(time.Now()),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("this is my data, there are many like it, but this is mine"),
	}
	var d SearchEntry
	if err := json.NewEncoder(bb).Encode(s); err != nil {
		t.Fatal(err)
	} else if err = json.NewDecoder(bb).Decode(&d); err != nil {
		t.Fatal(err)
	} else if !s.Equal(d) {
		t.Fatalf("EncodeDecode failed:\n%+v\n%+v", s, d)
	}
}

func TestSearchEntryEncodeDecodeEnum(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	//test without any enumerated values
	s := SearchEntry{
		TS:   entry.FromStandard(time.Now()),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("this is my data, there are many like it, but this is mine"),
		Enumerated: []EnumeratedPair{
			EnumeratedPair{Name: `foo`, Value: `bar`, RawValue: RawEnumeratedValue{Type: 1, Data: []byte("stuff")}},
			EnumeratedPair{Name: `bar`, Value: `baz`},
		},
	}
	var d SearchEntry
	if err := json.NewEncoder(bb).Encode(s); err != nil {
		t.Fatal(err)
	} else if err = json.NewDecoder(bb).Decode(&d); err != nil {
		t.Fatal(err)
	} else if !s.Equal(d) {
		t.Fatalf("EncodeDecode failed:\n%+v\n%+v", s, d)
	}
}

func TestSearchEntryEncodeDecodeRaw(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	ts, err := time.Parse(time.RFC3339Nano, `2020-12-23T16:04:17.417437Z`)
	if err != nil {
		t.Fatal(err)
	}
	//test without any enumerated values
	s := SearchEntry{
		TS:   entry.FromStandard(ts),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("testdata"),
	}
	raw := `{"TS": "2020-12-23T16:04:17.417437Z", "Tag": 4919, "SRC": "DEAD::BEEF", "Data": "dGVzdGRhdGE="}`
	bb.WriteString(raw)
	var d SearchEntry
	if err = json.NewDecoder(bb).Decode(&d); err != nil {
		t.Fatal(err)
	} else if !s.Equal(d) {
		t.Fatalf("EncodeDecode failed:\n%+v\n%+v", s, d)
	}
}

func TestElementNull(t *testing.T) {
	// Test with an empty []byte
	s := Element{
		Module:  "syslog",
		Name:    "MsgID",
		Path:    "MsgID",
		Filters: []string{"=="},
	}
	var b []byte
	var err error
	if b, err = json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("Found null in %v", string(b))
	}

	// Test with an empty string to make sure we set Value in that case
	s = Element{
		Module:  "syslog",
		Name:    "MsgID",
		Path:    "MsgID",
		Value:   "",
		Filters: []string{"=="},
	}
	if b, err = json.Marshal(s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Value") {
		t.Fatalf("No Value in %v", string(b))
	}

}