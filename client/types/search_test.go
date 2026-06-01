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
