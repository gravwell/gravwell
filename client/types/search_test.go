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
