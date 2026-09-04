/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types_test

// This file tests that marshalers generate the JSON we expect.
// It mostly centers around omitting fields and defaulting empty/nil to [] or {} instead of null.

import (
	"bytes"
	"encoding/json/v2"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
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

// thats that every BaseResponse-embedding response type marshals and unmarshals cleanly.
func TestResponseRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		src        any
		entriesLen func(decoded any) int
	}{
		{"BaseResponse", types.BaseResponse{Messages: []types.Message{{ID: 1}}}, nil},
		{"ChartResponse", types.ChartResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  types.ChartableValueSet{Names: []string{"test"}},
		}, func(d any) int { return len(d.(*types.ChartResponse).Entries.Names) }},
		{"FdgResponse", types.FdgResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  types.FdgSet{Groups: []string{"test"}},
		}, func(d any) int { return len(d.(*types.FdgResponse).Entries.Groups) }},
		{"PointmapResponse", types.PointmapResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.PointmapValue{{Loc: types.Location{Lat: 1, Long: 1}}},
		}, func(d any) int { return len(d.(*types.PointmapResponse).Entries) }},
		{"HeatmapResponse", types.HeatmapResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.HeatmapValue{{Magnitude: 1}},
		}, func(d any) int { return len(d.(*types.HeatmapResponse).Entries) }},
		{"P2PResponse", types.P2PResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.P2PValue{{Magnitude: 1}},
		}, func(d any) int { return len(d.(*types.P2PResponse).Entries) }},
		{"StackgraphResponse", types.StackGraphResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.StackGraphSet{{Key: "test"}},
		}, func(d any) int { return len(d.(*types.StackGraphResponse).Entries) }},
		{"TableResponse", types.TableResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  types.TableValueSet{Columns: []string{"test"}},
		}, func(d any) int { return len(d.(*types.TableResponse).Entries.Columns) }},
		{"GaugeResponse", types.GaugeResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.GaugeValue{{Name: "test"}},
		}, func(d any) int { return len(d.(*types.GaugeResponse).Entries) }},
		{"WordcloudResponse", types.WordcloudResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.WordcloudValue{{Name: "test"}},
		}, func(d any) int { return len(d.(*types.WordcloudResponse).Entries) }},
		{"RawResponse", types.RawResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.SearchEntry{{Data: []byte("foo")}},
		}, func(d any) int { return len(d.(*types.RawResponse).Entries) }},
		{"TextResponse", types.TextResponse{
			Messages: []types.Message{{ID: 1}},
			Entries:  []types.SearchEntry{{Data: []byte("foo")}},
		}, func(d any) int { return len(d.(*types.TextResponse).Entries) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := bytes.NewBuffer(nil)
			require.NoError(t, json.MarshalWrite(bb, tt.src))

			decoded := reflect.New(reflect.TypeOf(tt.src)).Interface()
			require.NoError(t, json.UnmarshalRead(bb, decoded))

			// all parent types have a messages field;
			// we have to access it via reflection as there is no interface we can assert.
			messages := reflect.ValueOf(decoded).Elem().FieldByName("Messages")
			require.Equal(t, 1, messages.Len(), "invalid decode")

			if tt.entriesLen != nil {
				require.Equal(t, 1, tt.entriesLen(decoded), "invalid decode")
			}
		})
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

// tests that types that had their custom marshalers killed in favour of leveraging JSONv2 still send values as expected.
// Specifically, guarantees that empty/nil maps/slice marshal to {}/[].
// Deterministic(true) is passed so output can be checked consistently.
func TestDeadCustomMarshalers(t *testing.T) {
	u := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	farFuture := time.Date(10001, time.January, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		data     any
		expected string
	}{
		// RawObject should default to {}.
		{"RawObject nil marshals to empty object", types.RawObject(nil), `{}`},
		{"RawObject explicit empty slice marshals to empty object", types.RawObject([]byte{}), `{}`},
		{"RawObject populated object passes through", types.RawObject(`{"a":1}`), `{"a":1}`},
		{"RawObject populated array passes through", types.RawObject(`[1,2,3]`), `[1,2,3]`},

		{"RawResponse", types.RawResponse{}, `{"ID":0,"Finished":false,"EntryCount":0,"EntryCountValid":false,"AdditionalEntries":false,"OverLimit":false,"LimitDroppedRange":{"StartTS":"0001-01-01T00:00:00Z","EndTS":"0001-01-01T00:00:00Z"},"SessionID":"00000000-0000-0000-0000-000000000000","Interval":0,"Messages":[],"ContainsBinaryEntries":false,"Entries":[]}`},

		{"LoggingLevels populated passes through", &types.LoggingLevels{
			Levels: []string{"INFO", "WARN"}, Current: "INFO",
		}, `{"Levels":["INFO","WARN"],"Current":"INFO"}`},
		{"LoggingLevels empty displays expected defaults", &types.LoggingLevels{},
			`{"Levels":[],"Current":""}`},

		{"WellInfo zero value defaults Tags and Shards to empty lists", types.WellInfo{},
			`{"ID":"","Name":"","Tags":[],"Shards":[],"Fragmentation":0}`},
		{"WellInfo populated fields all round trip, omitempty strings included when set", types.WellInfo{
			ID: "id1", Name: "well1", Tags: []string{"tag1", "tag2"},
			Shards:      []types.ShardInfo{{Name: "shard1"}},
			Accelerator: "acc", Engine: "eng", Path: "/hot", ColdPath: "/cold", Fragmentation: 5,
		}, `{"ID":"id1","Name":"well1","Tags":["tag1","tag2"],"Shards":[{"Name":"shard1","Start":"0001-01-01T00:00:00Z","End":"0001-01-01T00:00:00Z","Entries":0,"Size":0,"Stored":0,"Cold":false,"Fragmentation":0}],"Accelerator":"acc","Engine":"eng","Path":"/hot","ColdPath":"/cold","Fragmentation":5}`},
		{"IndexerWellData zero value defaults Wells to empty list and Replicated to empty object", types.IndexerWellData{},
			`{"UUID":"00000000-0000-0000-0000-000000000000","Wells":[],"Replicated":{}}`},
		{"IndexerWellData populated Wells and Replicated round trip", types.IndexerWellData{
			UUID: u, Wells: []types.WellInfo{{Name: "w1"}},
			Replicated: map[uuid.UUID][]types.WellInfo{u: {{Name: "w2"}}},
		}, `{"UUID":"11111111-1111-1111-1111-111111111111","Wells":[{"ID":"","Name":"w1","Tags":[],"Shards":[],"Fragmentation":0}],"Replicated":{"11111111-1111-1111-1111-111111111111":[{"ID":"","Name":"w2","Tags":[],"Shards":[],"Fragmentation":0}]}}`},
		{"ShardInfo zero value RemoteState is omitted entirely, not marshaled as {}", types.ShardInfo{},
			`{"Name":"","Start":"0001-01-01T00:00:00Z","End":"0001-01-01T00:00:00Z","Entries":0,"Size":0,"Stored":0,"Cold":false,"Fragmentation":0}`},
		{"ShardInfo fully populated RemoteState is included", types.ShardInfo{
			Name: "s1", RemoteState: types.ReplicationState{UUID: u, Entries: 10, Size: 100},
		}, `{"Name":"s1","Start":"0001-01-01T00:00:00Z","End":"0001-01-01T00:00:00Z","Entries":0,"Size":0,"Stored":0,"RemoteState":{"UUID":"11111111-1111-1111-1111-111111111111","Entries":10,"Size":100},"Cold":false,"Fragmentation":0}`},
		{"ShardInfo RemoteState with only a UUID set still counts as non-empty", types.ShardInfo{
			Name: "s2", RemoteState: types.ReplicationState{UUID: u},
		}, `{"Name":"s2","Start":"0001-01-01T00:00:00Z","End":"0001-01-01T00:00:00Z","Entries":0,"Size":0,"Stored":0,"RemoteState":{"UUID":"11111111-1111-1111-1111-111111111111","Entries":0,"Size":0},"Cold":false,"Fragmentation":0}`},
		{"ShardInfo timestamps beyond year 9999 are clamped to maxJsonTimestamp", types.ShardInfo{
			Name: "s3", Start: farFuture, End: farFuture.Add(24 * time.Hour),
		}, `{"Name":"s3","Start":"9999-12-12T23:59:59.000000099Z","End":"9999-12-12T23:59:59.000000099Z","Entries":0,"Size":0,"Stored":0,"Cold":false,"Fragmentation":0}`},

		{"ChartableValueSet zero value", types.ChartableValueSet{}, `{"Names":[],"Values":[]}`},
		{"ChartableValueSet populated", types.ChartableValueSet{
			Names: []string{"n1"}, KeyComps: []types.KeyComponents{{}}, Categories: []string{"cat1"},
		}, `{"Names":["n1"],"KeyComps":[{"Keys":[]}],"Categories":["cat1"],"Values":[]}`},

		// CapabilityState and TagAccess are both single-field structs wrapping Grants
		{"CapabilityState zero value", types.CapabilityState{}, `{"Grants":[]}`},
		{"CapabilityState populated", types.CapabilityState{Grants: []string{"read", "write"}}, `{"Grants":["read","write"]}`},
		{"TagAccess zero value", types.TagAccess{}, `{"Grants":[]}`},
		{"TagAccess populated", types.TagAccess{Grants: []string{"tag1", "tag2"}}, `{"Grants":["tag1","tag2"]}`},

		{"TableValueSet zero value", types.TableValueSet{}, `{"Columns":[],"Rows":[]}`},
		{"TableValueSet populated", types.TableValueSet{
			Columns: []string{"c1", "c2"}, Rows: types.TableRowSet{{Row: []string{"r1"}}},
		}, `{"Columns":["c1","c2"],"Rows":[{"TS":"0001-01-01T00:00:00Z","Row":["r1"]}]}`},
		{"TableRow zero value", types.TableRow{}, `{"TS":"0001-01-01T00:00:00Z","Row":[]}`},
		{"TableRow populated", types.TableRow{Row: []string{"a", "b"}}, `{"TS":"0001-01-01T00:00:00Z","Row":["a","b"]}`},

		{"UserDetails zero value", types.UserDetails{},
			`{"UID":0,"User":"","Name":"","Email":"","Admin":false,"Locked":false,"TS":"0001-01-01T00:00:00Z","DefaultGID":0,"Groups":[],"MFA":{"TOTP":{"Enabled":false},"RecoveryCodes":{"Enabled":false,"Remaining":0,"Generated":"0001-01-01T00:00:00Z"}},"Synced":false,"SSOUser":false}`},
		{"UserDetails populated Groups round trip", types.UserDetails{
			UID: 1, User: "bob", Groups: []types.GroupDetails{{GID: 2, Name: "g1"}},
		}, `{"UID":1,"User":"bob","Name":"","Email":"","Admin":false,"Locked":false,"TS":"0001-01-01T00:00:00Z","DefaultGID":0,"Groups":[{"GID":2,"Name":"g1","Desc":"","Synced":false}],"MFA":{"TOTP":{"Enabled":false},"RecoveryCodes":{"Enabled":false,"Remaining":0,"Generated":"0001-01-01T00:00:00Z"}},"Synced":false,"SSOUser":false}`},
		{"UserAddGroups zero value pointer defaults GIDs to an empty list", &types.UserAddGroups{}, `{"GIDs":[]}`},
		{"UserAddGroups populated pointer", &types.UserAddGroups{GIDs: []int32{1, 2}}, `{"GIDs":[1,2]}`},

		{"RenderModuleInfo zero value pointer", &types.RenderModuleInfo{}, `{"Name":"","Description":"","Examples":[]}`},
		{"RenderModuleInfo populated pointer", &types.RenderModuleInfo{
			Name: "mod1", Examples: []string{"ex1"},
		}, `{"Name":"mod1","Description":"","Examples":["ex1"]}`},

		{"RSP2PChannels zero value", types.RSP2PChannels{}, `{"From":"","To":"","Magnitude":"","Tooltip":[]}`},
		{"RSP2PChannels populated", types.RSP2PChannels{
			From: "f", To: "t", Magnitude: "m", Tooltip: []string{"tip1"},
		}, `{"From":"f","To":"t","Magnitude":"m","Tooltip":["tip1"]}`},

		{"IngestStats zero value", types.IngestStats{},
			`{"QuotaUsed":0,"QuotaMax":0,"EntriesPerSecond":0,"BytesPerSecond":0,"TotalCount":0,"TotalSize":0,"LastDayCount":0,"LastDaySize":0,"EntriesHourTail":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"EntriesMinuteTail":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"BytesHourTail":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"BytesMinuteTail":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"Ingesters":[],"Missing":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.data, json.Deterministic(true)) // we want deterministic so we can properly test expected values
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(b))
		})
	}

	t.Run("RawResponse with printable data (zero)", func(t *testing.T) {
		rr := types.RawResponse{}
		rr.SetPrintableData(true)

		b, err := json.Marshal(rr, json.Deterministic(true)) // we want deterministic so we can properly test expected values
		require.NoError(t, err)
		require.Equal(t, `{"ID":0,"Finished":false,"EntryCount":0,"EntryCountValid":false,"AdditionalEntries":false,"OverLimit":false,"LimitDroppedRange":{"StartTS":"0001-01-01T00:00:00Z","EndTS":"0001-01-01T00:00:00Z"},"SessionID":"00000000-0000-0000-0000-000000000000","Interval":0,"Messages":[],"ContainsBinaryEntries":false,"Entries":[]}`, string(b))
	})
	t.Run("RawResponse with printable data", func(t *testing.T) {
		rr := types.RawResponse{Entries: []types.SearchEntry{{TS: entry.Timestamp{Sec: 55555555}, Data: []byte("Hello World")}}}
		rr.SetPrintableData(true)

		b, err := json.Marshal(rr, json.Deterministic(true)) // we want deterministic so we can properly test expected values
		require.NoError(t, err)
		require.Equal(t, `{"ID":0,"Finished":false,"EntryCount":0,"EntryCountValid":false,"AdditionalEntries":false,"OverLimit":false,"LimitDroppedRange":{"StartTS":"0001-01-01T00:00:00Z","EndTS":"0001-01-01T00:00:00Z"},"SessionID":"00000000-0000-0000-0000-000000000000","Interval":0,"Messages":[],"ContainsBinaryEntries":false,"Entries":[{"TS":"0002-10-06T00:05:55Z","SRC":"","Tag":0,"Data":"Hello World","Enumerated":null}]}`, string(b))
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
