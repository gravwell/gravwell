/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

import (
	"bytes"
	"encoding/json/v2"
	"net"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/stretchr/testify/require"
)

func TestTimeRangeEncodeDecode(t *testing.T) {
	ts := entry.Now()
	tr := types.TimeRange{
		StartTS: ts,
		EndTS:   ts.Add(time.Hour),
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, tr); err != nil {
		t.Fatal(err)
	}
	var ttr types.TimeRange
	if err := json.UnmarshalRead(bb, &ttr); err != nil {
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
	var ttr types.TimeRange
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
	s := types.SearchEntry{
		TS:   entry.FromStandard(time.Now()),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("this is my data, there are many like it, but this is mine"),
	}
	var d types.SearchEntry
	if err := json.MarshalWrite(bb, s); err != nil {
		t.Fatal(err)
	} else if err = json.UnmarshalRead(bb, &d); err != nil {
		t.Fatal(err)
	} else if !s.Equal(d) {
		t.Fatalf("EncodeDecode failed:\n%+v\n%+v", s, d)
	}
}

func TestSearchEntryEncodeDecodeEnum(t *testing.T) {
	bb := bytes.NewBuffer(nil)
	//test without any enumerated values
	s := types.SearchEntry{
		TS:   entry.FromStandard(time.Now()),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("this is my data, there are many like it, but this is mine"),
		Enumerated: []types.EnumeratedPair{
			{Name: `foo`, Value: `bar`, RawValue: types.RawEnumeratedValue{Type: 1, Data: []byte("stuff")}},
			{Name: `bar`, Value: `baz`},
		},
	}
	var d types.SearchEntry
	if err := json.MarshalWrite(bb, s); err != nil {
		t.Fatal(err)
	} else if err = json.UnmarshalRead(bb, &d); err != nil {
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
	s := types.SearchEntry{
		TS:   entry.FromStandard(ts),
		Tag:  0x1337,
		SRC:  net.ParseIP(`DEAD::BEEF`),
		Data: []byte("testdata"),
	}
	raw := `{"TS": "2020-12-23T16:04:17.417437Z", "Tag": 4919, "SRC": "DEAD::BEEF", "Data": "dGVzdGRhdGE="}`
	bb.WriteString(raw)
	var d types.SearchEntry
	if err = json.UnmarshalRead(bb, &d); err != nil {
		t.Fatal(err)
	} else if !s.Equal(d) {
		t.Fatalf("EncodeDecode failed:\n%+v\n%+v", s, d)
	}
}

func TestBaseResponseEncode(t *testing.T) {
	br := types.BaseResponse{
		Messages: []types.Message{
			{
				ID: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.BaseResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestChartResponseEncode(t *testing.T) {
	br := types.ChartResponse{
		Messages: []types.Message{
			{
				ID: 1,
			},
		},
		Entries: types.ChartableValueSet{
			Names: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.ChartResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.FdgResponse{
		Messages: []types.Message{
			{
				ID: 1,
			},
		},
		Entries: types.FdgSet{
			Groups: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.FdgResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.PointmapResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.PointmapValue{
			{
				Loc: types.Location{
					Lat:  1,
					Long: 1,
				},
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.PointmapResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.HeatmapResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.HeatmapValue{
			{
				Magnitude: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.HeatmapResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.P2PResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.P2PValue{
			{
				Magnitude: 1,
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.P2PResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.StackGraphResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.StackGraphSet{
			{
				Key: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.StackGraphResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.TableResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: types.TableValueSet{
			Columns: []string{"test"},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.TableResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.GaugeResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.GaugeValue{
			{
				Name: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.GaugeResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.WordcloudResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.WordcloudValue{
			{
				Name: "test",
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.WordcloudResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.TextResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.SearchEntry{
			{
				Data: []byte("foo"),
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.TextResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
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
	br := types.RawResponse{
		BaseResponse: types.BaseResponse{
			Messages: []types.Message{
				{
					ID: 1,
				},
			},
		},
		Entries: []types.SearchEntry{
			{
				Data: []byte("foo"),
			},
		},
	}
	bb := bytes.NewBuffer(nil)
	if err := json.MarshalWrite(bb, br); err != nil {
		t.Fatal(err)
	}

	var x types.RawResponse

	if err := json.UnmarshalRead(bb, &x); err != nil {
		t.Fatal(err)
	}

	if len(x.Messages) != 1 {
		t.Fatal("invalid decode")
	}
	if len(x.Entries) != 1 {
		t.Fatal("invalid decode")
	}
}

func TestOptionalNoTags(t *testing.T) {
	// NOTE(rlandau): this first set of tests do NOT assume any tags on Optional[T] fields,
	// hence the includeOmitZeroOption bool.
	tests := []struct {
		name                  string
		data                  any
		includeOmitZeroOption bool // include the json.OmitZeroStructFields option when marshaling
		expected              string
	}{
		{"Optional field is included if set",
			struct{ S types.Optional[string] }{S: types.NewOptional("foo")},
			false,
			`{"S":"foo"}`,
		},
		{"Optional field with zero value is included",
			struct{ S types.Optional[int32] }{S: types.NewOptional[int32](5)},
			true,
			`{"S":5}`,
		},
		{"Optional field is omitted if omitzero tag is present",
			struct {
				S types.Optional[string] `json:",omitzero"`
			}{},
			false,
			`{}`,
		},
		{
			"Optional nil slice marshals to []",
			types.Optional[[]string]{},
			false,
			`[]`,
		},
		{
			"Optional nil map marshals to {}",
			types.Optional[map[string]string]{},
			false,
			`{}`,
		},
		{"Optional field is omitted if OmitZeroStructFields option is present",
			struct{ S types.Optional[int32] }{S: types.NewOptional[int32](5)},
			true,
			`{"S":5}`,
		},
		{"Zero Patch results in emptyJSON",
			types.CommonFieldsPatch{},
			true,
			`{}`,
		},
		{"Partially populated Patch type results in only populated fields",
			types.CommonFieldsPatch{
				Description: types.NewOptional(""),
				Writers:     types.NewOptional(types.ACL{GIDs: []int32{1, 2, 12}}),
			},
			true,
			`{"Description":"","Writers":{"GIDs":[1,2,12],"Global":false}}`,
		},
		{"Fully populated Patch type results in full JSON",
			types.CommonFieldsPatch{
				Description: types.NewOptional("desc"),
				Labels:      types.NewOptional([]string{"shí"}),
				Name:        types.NewOptional("fully popped"),
				OwnerID:     types.NewOptional[int32](1),
				Readers:     types.NewOptional(types.ACL{GIDs: []int32{0, 5, 8, 0}}),
				Writers:     types.NewOptional(types.ACL{}),
			},
			true,
			`{"Description":"desc","Labels":["shí"],"Name":"fully popped","OwnerID":1,"Readers":{"GIDs":[0,5,8,0],"Global":false},"Writers":{"GIDs":[],"Global":false}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []json.Options
			if tt.includeOmitZeroOption {
				opts = append(opts, json.OmitZeroStructFields(true))
			}
			b, err := json.Marshal(&tt.data, opts...)
			require.Nil(t, err, "failed to marshal %v", &tt.data)
			t.Logf("Marshaled %+v to %s", tt.data, b)
			require.Equal(t, tt.expected, string(b))
		})
	}
	t.Run("Complex Optional scenarios", func(t *testing.T) {
		t.Run("Nested T", func(t *testing.T) {
			// very messy, but a good way to ensure we capture all fields
			v := types.NewOptional([]struct {
				A int32
				B struct{ Yī, èr string }
			}{struct {
				A int32
				B struct {
					Yī string
					èr string
				}
			}{A: 5, B: struct {
				Yī string
				èr string
			}{"one", "two"}}})
			b, err := json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `[{"A":5,"B":{"Yī":"one"}}]`, string(b))

			v.Unset()
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			// we unset the whole thing, so the zero value ([]struct{...}(nil)) is marshaled;
			// JSON/v2 defaults nil slices to '[]' rather than null.
			require.Equal(t, `[]`, string(b))
		})
		t.Run("Set -> Set -> Unset -> Set", func(t *testing.T) {
			v := struct {
				A types.Optional[int32]
				B struct{ One, two types.Optional[string] }
				C types.Optional[struct {
					three bool
					Four  float32
				}]
			}{
				A: types.NewOptional[int32](0),
				B: struct {
					One types.Optional[string]
					two types.Optional[string]
				}{types.NewOptional("Yī"), types.NewOptional("èr")},
				C: types.NewOptional(struct {
					three bool
					Four  float32
				}{true, 4.4}),
			}
			// everything should be included
			b, err := json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `{"A":0,"B":{"One":"Yī"},"C":{"Four":4.4}}`, string(b))

			// set a couple new values
			v.B.One.Set("ein")
			v.B.two.Set("swei")
			// everything should be included
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			require.Equal(t, `{"A":0,"B":{"One":"ein"},"C":{"Four":4.4}}`, string(b))

			// unset everything
			v.A.Unset()
			v.B.One.Unset()
			v.C.Unset()
			b, err = json.Marshal(&v, json.OmitZeroStructFields(true))
			require.Nil(t, err, "failed to marshal %v", &v)
			t.Logf("Marshaled %+v to %s", v, b)
			// B is a concrete struct, so it will always be included.
			// This is intentional.
			require.Equal(t, `{"B":{}}`, string(b))
		})
	})
}

func TestMarshalUnmarshal(t *testing.T) {
	t.Run("empty patch", func(t *testing.T) {
		mp := types.MacroPatch{}
		b, err := json.Marshal(&mp)
		require.Nil(t, err, "failed to marshal %v", &mp)

		out := types.MacroPatch{}
		require.Nil(t, json.Unmarshal(b, &out))
	})
	t.Run("partial patch", func(t *testing.T) {
		mp := types.MacroPatch{Expansion: types.NewOptional("exp")}
		b, err := json.Marshal(&mp)
		require.Nil(t, err, "failed to marshal %v", &mp)

		out := types.MacroPatch{}
		require.Nil(t, json.Unmarshal(b, &out))
		require.Equal(t, mp.Expansion.Value(), out.Expansion.Value())
	})
	t.Run("partial patch 2", func(t *testing.T) {
		mp := types.MacroPatch{
			Labels:    types.NewOptional([]string{"kuài"}),
			OwnerID:   types.NewOptional[int32](0),
			Expansion: types.NewOptional("exp")}
		b, err := json.Marshal(&mp)
		require.Nil(t, err, "failed to marshal %v", &mp)

		out := types.MacroPatch{}
		require.Nil(t, json.Unmarshal(b, &out))
		require.Equal(t, mp.Expansion.Value(), out.Expansion.Value())
	})
}
