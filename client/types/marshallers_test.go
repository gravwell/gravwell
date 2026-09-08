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
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
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

func TestBaseResponseEncode(t *testing.T) {
	br := BaseResponse{
		Messages: []Message{
			{
				ID: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x BaseResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestChartResponseEncode(t *testing.T) {
	br := ChartResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: ChartableValueSet{
			Names: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x ChartResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries.Names) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestFDGResponseEncode(t *testing.T) {
	br := FdgResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: FdgSet{
			Groups: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x FdgResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries.Groups) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestPointmapResponseEncode(t *testing.T) {
	br := PointmapResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []PointmapValue{
			{
				Loc: Location{
					Lat:  1,
					Long: 1,
				},
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x PointmapResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestHeatmapResponseEncode(t *testing.T) {
	br := HeatmapResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []HeatmapValue{
			{
				Magnitude: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x HeatmapResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestP2PResponseEncode(t *testing.T) {
	br := P2PResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []P2PValue{
			{
				Magnitude: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x P2PResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestStackgraphResponseEncode(t *testing.T) {
	br := StackGraphResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []StackGraphSet{
			{
				Key: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x StackGraphResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestTableResponseEncode(t *testing.T) {
	br := TableResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: TableValueSet{
			Columns: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x TableResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries.Columns) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestGaugeResponseEncode(t *testing.T) {
	br := GaugeResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []GaugeValue{
			{
				Name: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x GaugeResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestWordcloudResponseEncode(t *testing.T) {
	br := WordcloudResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []WordcloudValue{
			{
				Name: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x WordcloudResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestTextResponseEncode(t *testing.T) {
	br := TextResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []SearchEntry{
			{
				Data: []byte("foo"),
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x TextResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestRawResponseEncode(t *testing.T) {
	br := RawResponse{
		BaseResponse: BaseResponse{
			Messages: []Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []SearchEntry{
			{
				Data: []byte("foo"),
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.NewEncoder(bb).Encode(br); err != nil {
		t.Fatal(err)
	}

	var x RawResponse

	if err := json.NewDecoder(bb).Decode(&x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}
