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
	"testing"
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
	}{
		{
			name: "chart",
			json: `{"renderer":"chart","channels":{"category":"Category","nominal":"Value","temporal":"Timestamp"}}`,
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
			name: "point2point",
			json: `{"renderer":"point2point","channels":{"from":"Src","to":"Dst","magnitude":"Mag"}}`,
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
			name: "numbercard",
			json: `{"renderer":"numbercard","channels":{"value":"Magnitude","label":"Name"}}`,
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
			name: "gauge",
			json: `{"renderer":"gauge","channels":{"value":"Magnitude","min":"Min","max":"Max","label":"Name"}}`,
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
			name: "heatmap",
			json: `{"renderer":"heatmap","channels":{"location":"Location","magnitude":"Magnitude"}}`,
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
			name: "pointmap",
			json: `{"renderer":"pointmap","channels":{"location":"Location","tooltip":["Bytes"]}}`,
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
			name: "stackgraph",
			json: `{"renderer":"stackgraph","channels":{"category":"proto","nominal":"count","color":"service"}}`,
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
			name: "wordcloud",
			json: `{"renderer":"wordcloud","channels":{"name":"Name","magnitude":"Magnitude"}}`,
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
			name: "table",
			json: `{"renderer":"table","channels":{"columns":["Appname","MsgID"]}}`,
			want: RendererSettings{
				Tabular: &RSTabular{
					Renderer: "table",
					Channels: RSTabularChannels{
						Columns: []string{"Appname", "MsgID"},
					},
				},
			},
		},
		{
			name: "fdg",
			json: `{"renderer":"fdg","channels":{"from":"Src","to":"Dst","magnitude":"Mag"}}`,
			want: RendererSettings{
				Fdg: &RSFdg{
					Renderer: "fdg",
					Channels: RSFdgChannels{
						From:      "Src",
						To:        "Dst",
						Magnitude: "Mag",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Unmarshal
			var got RendererSettings
			if err := json.Unmarshal([]byte(tt.json), &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			// Verify fields match
			b1, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal got failed: %v", err)
			}
			b2, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("Marshal want failed: %v", err)
			}
			if string(b1) != string(b2) {
				t.Errorf("Mismatch:\nGot:  %s\nWant: %s", string(b1), string(b2))
			}

			// Test Marshal from original 'want'
			b3, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("Marshal want failed: %v", err)
			}
			var got2 RendererSettings
			if err := json.Unmarshal(b3, &got2); err != nil {
				t.Fatalf("Unmarshal from Marshal failed: %v", err)
			}
			b4, err := json.Marshal(got2)
			if err != nil {
				t.Fatalf("Marshal got2 failed: %v", err)
			}
			if string(b4) != string(b2) {
				t.Errorf("Roundtrip mismatch:\nGot:  %s\nWant: %s", string(b4), string(b2))
			}
		})
	}
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
	_, err = json.Marshal(rsEmpty)
	if err == nil {
		t.Fatal("expected error when no render settings are specified")
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
