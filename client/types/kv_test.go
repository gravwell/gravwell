/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
)

func TestValidateKVColumns(t *testing.T) {
	tests := []struct {
		name string
		cols []KVColumn
		want error
	}{
		{"empty", nil, nil},
		{"ok", []KVColumn{{Name: "owner", Type: KVColumnString}, {Name: "seen_2", Type: KVColumnTimestamp}}, nil},
		{"leading underscore", []KVColumn{{Name: "_updated", Type: KVColumnString}}, ErrKVColumnName},
		{"leading digit", []KVColumn{{Name: "2x", Type: KVColumnString}}, ErrKVColumnName},
		{"dash", []KVColumn{{Name: "a-b", Type: KVColumnString}}, ErrKVColumnName},
		{"empty name", []KVColumn{{Name: "", Type: KVColumnString}}, ErrKVColumnName},
		{"too long", []KVColumn{{Name: "a" + strings.Repeat("b", KVMaxColumnNameLength), Type: KVColumnString}}, ErrKVColumnName},
		{"max length", []KVColumn{{Name: strings.Repeat("a", KVMaxColumnNameLength), Type: KVColumnString}}, nil},
		{"reserved key", []KVColumn{{Name: "Key", Type: KVColumnString}}, ErrKVColumnReserved},
		{"bad type", []KVColumn{{Name: "a", Type: "uuid"}}, ErrKVColumnType},
		{"duplicate ignoring case", []KVColumn{{Name: "Owner", Type: KVColumnString}, {Name: "owner", Type: KVColumnInt}}, ErrKVColumnDuplicate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKVColumns(tt.cols)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestKVValuesKeepIntPrecision(t *testing.T) {
	const big = `9007199254740993` // 2^53 + 1, which a float64 can't hold
	var row KVTableRow
	if err := json.Unmarshal([]byte(`{"Key":"k","Values":{"n":`+big+`}}`), &row); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := json.Unmarshal(row.Values["n"], &n); err != nil {
		t.Fatal(err)
	}
	if n != 9007199254740993 {
		t.Fatalf("got %d, want %s", n, big)
	}
}

func TestKVTableRowWriteExpectedVersion(t *testing.T) {
	w := KVTableRowWrite{Values: KVValues{"a": jsontext.Value(`"x"`)}}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "ExpectedVersion") {
		t.Fatalf("unset ExpectedVersion was marshaled: %s", b)
	}

	// 0 is meaningful (the row must not exist), so it has to survive marshaling.
	w.ExpectedVersion = NewOptional(int64(0))
	if b, err = json.Marshal(w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ExpectedVersion":0`) {
		t.Fatalf("ExpectedVersion 0 was dropped: %s", b)
	}
}

func TestKVTablePatchMarshal(t *testing.T) {
	b, err := json.Marshal(KVTablePatch{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{}` {
		t.Fatalf("empty patch marshaled to %s, want {}", b)
	}

	tbl := KVTable{Columns: []KVColumn{{Name: "owner", Type: KVColumnString, Indexed: true}}}
	if b, err = json.Marshal(tbl.ToPatch()); err != nil {
		t.Fatal(err)
	}
	var got struct{ Columns []KVColumn }
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 1 || got.Columns[0] != tbl.Columns[0] {
		t.Fatalf("Columns didn't round trip: %s", b)
	}
}
