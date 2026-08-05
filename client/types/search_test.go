/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRowSelectionMarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   RowSelection
		wantErr bool
	}{
		{"valid range", RowSelection{Kind: "range", Start: 0, End: 10}, false},
		{"valid single", RowSelection{Kind: "single", Index: 5}, false},
		{"range with index set", RowSelection{Kind: "range", Start: 0, End: 10, Index: 3}, true},
		{"single with start set", RowSelection{Kind: "single", Index: 5, Start: 1}, true},
		{"single with end set", RowSelection{Kind: "single", Index: 5, End: 1}, true},
		{"single with start and end set", RowSelection{Kind: "single", Index: 5, Start: 1, End: 7}, true},
		{"unknown kind", RowSelection{Kind: "bogus"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var out RowSelection
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			if out != tt.input {
				t.Fatalf("round-trip mismatch: got %+v, want %+v", out, tt.input)
			}
		})
	}
}

func TestRowSelectionUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid range", `{"kind":"range","start":0,"end":10}`, false},
		{"valid single", `{"kind":"single","index":5}`, false},
		{"range with index set", `{"kind":"range","start":0,"end":10,"index":3}`, true},
		{"single with start set", `{"kind":"single","index":5,"start":1}`, true},
		{"single with end set", `{"kind":"single","index":5,"end":1}`, true},
		{"single with start and end set", `{"kind":"single","index":5,"start":1,"end":7}`, true},
		{"unknown kind", `{"kind":"bogus"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out RowSelection
			err := json.Unmarshal([]byte(tt.input), &out)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRenderSettings(t *testing.T) {
	tests := []struct {
		name string
		json string
		want RendererSettings
		// wantJSON is the exact expected wire format. Asserted against a
		// literal on purpose: comparing marshal(got) to marshal(want) cannot
		// observe anything MarshalJSON normalizes.
		wantJSON string
	}{
		{
			name:     "chart",
			wantJSON: `{"Renderer":"chart","Channels":{"Category":"Category","Nominal":"Value","Temporal":"Timestamp"}}`,
			json:     `{"Renderer":"chart","Channels":{"Category":"Category","Nominal":"Value","Temporal":"Timestamp"}}`,
			want: RendererSettings{
				Chart: &RSChart{
					Renderer: "chart",
					Channels: RSChartChannels{
						Category: "Category",
						Nominal:  "Value",
						Temporal: "Timestamp",
					},
				},
			},
		},
		{
			name:     "point2point",
			wantJSON: `{"Renderer":"point2point","Channels":{"From":"Src","To":"Dst","Magnitude":"Mag","Tooltip":[]}}`,
			json:     `{"Renderer":"point2point","Channels":{"From":"Src","To":"Dst","Magnitude":"Mag"}}`,
			want: RendererSettings{
				P2P: &RSP2P{
					Renderer: "point2point",
					Channels: RSP2PChannels{
						From:      "Src",
						To:        "Dst",
						Magnitude: "Mag",
					},
				},
			},
		},
		{
			name:     "numbercard",
			wantJSON: `{"Renderer":"numbercard","Channels":{"Label":"Name","Value":"Magnitude"}}`,
			json:     `{"Renderer":"numbercard","Channels":{"Value":"Magnitude","Label":"Name"}}`,
			want: RendererSettings{
				Number: &RSNumber{
					Renderer: "numbercard",
					Channels: RSNumberChannels{
						Value: "Magnitude",
						Label: "Name",
					},
				},
			},
		},
		{
			name:     "gauge",
			wantJSON: `{"Renderer":"gauge","Channels":{"Label":"Name","Value":"Magnitude","Min":"Min","Max":"Max"}}`,
			json:     `{"Renderer":"gauge","Channels":{"Value":"Magnitude","Min":"Min","Max":"Max","Label":"Name"}}`,
			want: RendererSettings{
				Number: &RSNumber{
					Renderer: "gauge",
					Channels: RSNumberChannels{
						Value: "Magnitude",
						Label: "Name",
						Min:   "Min",
						Max:   "Max",
					},
				},
			},
		},
		{
			name:     "heatmap",
			wantJSON: `{"Renderer":"heatmap","Channels":{"Location":"Location","Magnitude":"Magnitude"}}`,
			json:     `{"Renderer":"heatmap","Channels":{"Location":"Location","Magnitude":"Magnitude"}}`,
			want: RendererSettings{
				Heatmap: &RSHeatmap{
					Renderer: "heatmap",
					Channels: RSHeatmapChannels{
						Location:  "Location",
						Magnitude: "Magnitude",
					},
				},
			},
		},
		{
			name:     "pointmap",
			wantJSON: `{"Renderer":"pointmap","Channels":{"Location":"Location","Tooltip":["Bytes"]}}`,
			json:     `{"Renderer":"pointmap","Channels":{"Location":"Location","Tooltip":["Bytes"]}}`,
			want: RendererSettings{
				Pointmap: &RSPointmap{
					Renderer: "pointmap",
					Channels: RSPointmapChannels{
						Location: "Location",
						Tooltip:  []string{"Bytes"},
					},
				},
			},
		},
		{
			name:     "stackgraph",
			wantJSON: `{"Renderer":"stackgraph","Channels":{"Category":"proto","Nominal":"count","Color":"service"}}`,
			json:     `{"Renderer":"stackgraph","Channels":{"Category":"proto","Nominal":"count","Color":"service"}}`,
			want: RendererSettings{
				StackGraph: &RSStackGraph{
					Renderer: "stackgraph",
					Channels: RSStackGraphChannels{
						Category: "proto",
						Nominal:  "count",
						Color:    "service",
					},
				},
			},
		},
		{
			name:     "wordcloud",
			wantJSON: `{"Renderer":"wordcloud","Channels":{"Name":"Name","Magnitude":"Magnitude"}}`,
			json:     `{"Renderer":"wordcloud","Channels":{"Name":"Name","Magnitude":"Magnitude"}}`,
			want: RendererSettings{
				WordCloud: &RSWordCloud{
					Renderer: "wordcloud",
					Channels: RSWordCloudChannels{
						Name:      "Name",
						Magnitude: "Magnitude",
					},
				},
			},
		},
		{
			name:     "table",
			wantJSON: `{"Renderer":"table","Channels":{"Columns":["Appname","MsgID"]}}`,
			json:     `{"Renderer":"table","Channels":{"Columns":["Appname","MsgID"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "table",
					Channels: RSTabularChannels{
						Columns: []string{"Appname", "MsgID"},
					},
				},
			},
		},
		// hex/pcap/raw/text all decode to RSTabular; only the discriminator
		// distinguishes them, so each string needs its own case.
		{
			name:     "hex",
			wantJSON: `{"Renderer":"hex","Channels":{"Columns":["DATA"]}}`,
			json:     `{"Renderer":"hex","Channels":{"Columns":["DATA"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "hex",
					Channels: RSTabularChannels{Columns: []string{"DATA"}},
				},
			},
		},
		{
			name:     "pcap",
			wantJSON: `{"Renderer":"pcap","Channels":{"Columns":["DATA"]}}`,
			json:     `{"Renderer":"pcap","Channels":{"Columns":["DATA"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "pcap",
					Channels: RSTabularChannels{Columns: []string{"DATA"}},
				},
			},
		},
		{
			name:     "raw",
			wantJSON: `{"Renderer":"raw","Channels":{"Columns":["DATA"]}}`,
			json:     `{"Renderer":"raw","Channels":{"Columns":["DATA"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "raw",
					Channels: RSTabularChannels{Columns: []string{"DATA"}},
				},
			},
		},
		{
			name:     "text",
			wantJSON: `{"Renderer":"text","Channels":{"Columns":["DATA"]}}`,
			json:     `{"Renderer":"text","Channels":{"Columns":["DATA"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "text",
					Channels: RSTabularChannels{Columns: []string{"DATA"}},
				},
			},
		},
		{
			name:     "fdg",
			wantJSON: `{"Renderer":"fdg","Channels":{"Weight":"weight"}}`,
			json:     `{"Renderer":"fdg","Channels":{"Weight":"weight"}}`,
			want: RendererSettings{
				Fdg: &RSFdg{
					Renderer: "fdg",
					Channels: RSFdgChannels{
						Weight: "weight",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got RendererSettings
			if err := json.Unmarshal([]byte(tt.json), &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			// Assert the decoded Go value directly. Comparing marshalled bytes
			// alone cannot see a variant flip: a value whose Renderer string
			// disagrees with its variant decodes into a *different* variant and
			// still re-marshals byte-identically.
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("decoded value mismatch:\nGot:  %#v\nWant: %#v", got, tt.want)
			}

			// Assert the wire format against a literal. Comparing marshal(got)
			// to marshal(want) is blind to anything MarshalJSON normalizes,
			// because both sides pass through the same normalization — that is
			// how the nil-slice -> [] fix went untested.
			b1, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal got failed: %v", err)
			}
			if string(b1) != tt.wantJSON {
				t.Errorf("wire format mismatch:\nGot:  %s\nWant: %s", string(b1), tt.wantJSON)
			}

			// Round trip is asserted on the wire format, not the Go value:
			// normalization is intentionally not idempotent in memory (a nil
			// slice marshals to [] and decodes back as an empty non-nil slice).
			// What must hold is that re-encoding is stable.
			b2, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("Marshal want failed: %v", err)
			}
			var got2 RendererSettings
			if err := json.Unmarshal(b2, &got2); err != nil {
				t.Fatalf("Unmarshal from Marshal failed: %v", err)
			}
			b3, err := json.Marshal(got2)
			if err != nil {
				t.Fatalf("Marshal roundtripped value failed: %v", err)
			}
			if string(b3) != tt.wantJSON {
				t.Errorf("roundtrip wire mismatch:\nGot:  %s\nWant: %s", string(b3), tt.wantJSON)
			}
		})
	}
}

// TestRendererSettingsEmptyMarshalsNull pins the contract that an empty
// RendererSettings encodes as null rather than failing. A MarshalJSON error
// here propagates to the top of the enclosing document: the whole SearchInfo
// (or a slice of them) yields zero bytes, which in a handler that has already
// written a 200 means an empty body with no error signal.
// TestSearchInfoWithEmptyRendererSettings guards the blast radius: omitempty
// tests the pointer, not the pointee, so a non-nil pointer to an empty
// RendererSettings is not skipped. If marshalling it failed, the error would
// escape to the top of the enclosing document and blank every SearchInfo in it.
func TestSearchInfoWithEmptyRendererSettings(t *testing.T) {
	si := []SearchInfo{
		{ID: "a"},
		{ID: "b", RendererSettings: &RendererSettings{}},
		{ID: "c"},
	}
	sb, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("SearchInfo slice with an empty RendererSettings must marshal: %v", err)
	}
	for _, id := range []string{`"a"`, `"b"`, `"c"`} {
		if !strings.Contains(string(sb), id) {
			t.Errorf("SearchInfo %s missing from output: %s", id, sb)
		}
	}
}

// TestRendererSettingsRequiredArraysNeverNull pins the wire contract for the
// three channel arrays the schema marks required: they must encode as [] even
// when the Go value is nil. Nil is reachable in practice because gob does not
// preserve the empty-vs-nil distinction, so a search that round-trips through
// the archive or remote search info comes back with a nil slice.
//
// Each case must fail if its type's MarshalJSON is removed — the table-driven
// test above only covers this for point2point, whose fixture has no tooltip.
func TestRendererSettingsRequiredArraysNeverNull(t *testing.T) {
	tests := []struct {
		name string
		in   RendererSettings
		want string
	}{
		{
			name: "p2p nil tooltip",
			in:   RendererSettings{P2P: &RSP2P{Renderer: "point2point", Channels: RSP2PChannels{From: "Src", To: "Dst", Magnitude: "Magnitude"}}},
			want: `{"Renderer":"point2point","Channels":{"From":"Src","To":"Dst","Magnitude":"Magnitude","Tooltip":[]}}`,
		},
		{
			name: "pointmap nil tooltip",
			in:   RendererSettings{Pointmap: &RSPointmap{Renderer: "pointmap", Channels: RSPointmapChannels{Location: "Location"}}},
			want: `{"Renderer":"pointmap","Channels":{"Location":"Location","Tooltip":[]}}`,
		},
		{
			name: "tabular nil columns",
			in:   RendererSettings{Tabular: &RSTabular{Renderer: "table", Channels: RSTabularChannels{}}},
			want: `{"Renderer":"table","Channels":{"Columns":[]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("required array encoded as null:\nGot:  %s\nWant: %s", b, tt.want)
			}
		})
	}
}

// TestRendererSettingsDecodeGuards covers the two Unmarshaler conventions the
// type has to honour: null is a no-op, and the receiver is reset so that
// decoding twice into the same value cannot leave two live variants (which
// MarshalJSON then rejects forever).
func TestRendererSettingsDecodeGuards(t *testing.T) {
	t.Run("null is a no-op", func(t *testing.T) {
		var rs RendererSettings
		if err := json.Unmarshal([]byte(`null`), &rs); err != nil {
			t.Fatalf("decoding null must not error: %v", err)
		}
		if !reflect.DeepEqual(rs, RendererSettings{}) {
			t.Errorf("decoding null must leave the value untouched, got %#v", rs)
		}
	})

	t.Run("receiver is reset between decodes", func(t *testing.T) {
		var rs RendererSettings
		if err := json.Unmarshal([]byte(`{"Renderer":"chart","Channels":{"Category":"c","Nominal":"n"}}`), &rs); err != nil {
			t.Fatalf("first decode: %v", err)
		}
		if err := json.Unmarshal([]byte(`{"Renderer":"fdg","Channels":{"Weight":"weight"}}`), &rs); err != nil {
			t.Fatalf("second decode: %v", err)
		}
		if rs.Chart != nil {
			t.Error("Chart survived a decode into the same value; receiver was not reset")
		}
		if rs.Fdg == nil {
			t.Fatal("Fdg not set by second decode")
		}
		// A value carrying two variants is permanently unencodable.
		if _, err := json.Marshal(rs); err != nil {
			t.Errorf("value is unencodable after two decodes: %v", err)
		}
	})

	t.Run("streaming decoder reuses the value", func(t *testing.T) {
		stream := `{"Renderer":"chart","Channels":{"Category":"c","Nominal":"n"}}` +
			`{"Renderer":"fdg","Channels":{"Weight":"weight"}}`
		dec := json.NewDecoder(strings.NewReader(stream))
		var rs RendererSettings
		for i := 0; i < 2; i++ {
			if err := dec.Decode(&rs); err != nil {
				t.Fatalf("decode %d: %v", i, err)
			}
			if _, err := json.Marshal(rs); err != nil {
				t.Fatalf("frame %d is unencodable: %v", i, err)
			}
		}
	})
}

func TestRenderSettingsErrors(t *testing.T) {
	// 1. Multiple fields set
	rsMultiple := RendererSettings{
		Chart: &RSChart{Renderer: "chart"},
		Fdg:   &RSFdg{Renderer: "fdg"},
	}
	_, err := json.Marshal(rsMultiple)
	if err == nil {
		t.Fatal("expected error when multiple render settings are specified")
	}

	// 2. Zero fields set (empty / nil)
	rsEmpty := RendererSettings{}
	k, err := json.Marshal(rsEmpty)
	if err != nil {
		t.Fatal(err)
	}

	if string(k) != `null` {
		t.Fatalf("expect k to be `null`, got %s", string(k))
	}
}

// TestSearchInfoDurationRoundTrip covers Duration surviving a decode. It is
// carried on the wire as a string and parsed back in UnmarshalJSON, so it is
// the one field that can be silently dropped by the order of assignments there.
func TestSearchInfoDurationRoundTrip(t *testing.T) {
	in := SearchInfo{ID: "dur", ItemCount: 42, Duration: 90 * time.Second}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"Duration":"1m30s"`) {
		t.Fatalf("Duration missing from the wire format: %s", b)
	}
	var out SearchInfo
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Duration != in.Duration {
		t.Errorf("Duration lost on decode: got %v, want %v", out.Duration, in.Duration)
	}
	if out.ItemCount != in.ItemCount {
		t.Errorf("ItemCount = %d, want %d", out.ItemCount, in.ItemCount)
	}

	// An unparseable duration must still surface as an error.
	if err := json.Unmarshal([]byte(`{"Duration":"notaduration"}`), &out); err == nil {
		t.Error("expected an error for an invalid duration")
	}
}

// TestDownloadSearchOmitsZeroTimeframe pins that a zero TimeRange omits the
// field entirely rather than sending a year-1 window. A zero TimeRange is the
// in-repo idiom for "download everything".
func TestDownloadSearchOmitsZeroTimeframe(t *testing.T) {
	var zero Timeframe
	if !zero.IsEmpty() {
		t.Error("zero Timeframe must report empty")
	}
	var nilTF *Timeframe
	if !nilTF.IsEmpty() {
		t.Error("nil Timeframe must report empty")
	}

	omitted, err := json.Marshal(SearchDownloadRequest{Format: "json"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(omitted), "Timeframe") {
		t.Errorf("a nil Timeframe must be omitted, got %s", omitted)
	}

	set, err := json.Marshal(SearchDownloadRequest{
		Format:    "json",
		Timeframe: &Timeframe{Start: time.Unix(1, 0).UTC(), End: time.Unix(2, 0).UTC()},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(set), "Timeframe") {
		t.Errorf("a set Timeframe must be present, got %s", set)
	}
}

func TestSearchInfoMarshal(t *testing.T) {
	// Test without RenderSettings
	siEmpty := SearchInfo{
		ID: "test-id",
	}
	b, err := json.Marshal(siEmpty)
	if err != nil {
		t.Fatalf("failed to marshal SearchInfo without RenderSettings: %v", err)
	}
	var outEmpty SearchInfo
	if err := json.Unmarshal(b, &outEmpty); err != nil {
		t.Fatalf("failed to unmarshal SearchInfo without RenderSettings: %v", err)
	}
	if outEmpty.ID != "test-id" {
		t.Fatalf("expected ID test-id, got %s", outEmpty.ID)
	}

	// Test with RenderSettings
	siWithRenderSettings := SearchInfo{
		ID: "test-id-with-rs",
		RendererSettings: &RendererSettings{
			Chart: &RSChart{
				Renderer: "chart",
			},
		},
	}
	b2, err := json.Marshal(siWithRenderSettings)
	if err != nil {
		t.Fatalf("failed to marshal SearchInfo with RenderSettings: %v", err)
	}
	var outWithRS SearchInfo
	if err := json.Unmarshal(b2, &outWithRS); err != nil {
		t.Fatalf("failed to unmarshal SearchInfo with RenderSettings: %v", err)
	}
	if outWithRS.RendererSettings == nil || outWithRS.RendererSettings.Chart == nil || outWithRS.RendererSettings.Chart.Renderer != "chart" {
		t.Fatalf("expected chart renderer, got %+v", outWithRS.RendererSettings)
	}
}
