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

func TestValidateDataTableColumns(t *testing.T) {
	str := func(name string) DataTableColumn { return DataTableColumn{Name: name, Type: DataTableColumnString} }
	tests := []struct {
		name string
		cols []DataTableColumn
		key  string
		want error
	}{
		{"ok", []DataTableColumn{str("host"), {Name: "seen_2", Type: DataTableColumnTimestamp}}, "host", nil},
		{"no columns", nil, "host", ErrDataTableKey},
		{"leading underscore", []DataTableColumn{str("host"), str("_updated")}, "host", ErrDataTableColumnName},
		{"leading digit", []DataTableColumn{str("2x")}, "2x", ErrDataTableColumnName},
		{"dash", []DataTableColumn{str("a-b")}, "a-b", ErrDataTableColumnName},
		{"empty name", []DataTableColumn{str("")}, "", ErrDataTableColumnName},
		{"too long", []DataTableColumn{str("a" + strings.Repeat("b", DataTableMaxColumnNameLength))}, "a", ErrDataTableColumnName},
		{"max length", []DataTableColumn{str(strings.Repeat("a", DataTableMaxColumnNameLength))}, strings.Repeat("a", DataTableMaxColumnNameLength), nil},
		{"key is an ordinary name", []DataTableColumn{str("Key")}, "Key", nil},
		{"bad type", []DataTableColumn{str("host"), {Name: "a", Type: "uuid"}}, "host", ErrDataTableColumnType},
		{"no binary types in v1", []DataTableColumn{str("host"), {Name: "a", Type: "bytes"}}, "host", ErrDataTableColumnType},
		{"duration and mac", []DataTableColumn{{Name: "d", Type: DataTableColumnDuration}, {Name: "m", Type: DataTableColumnMAC}}, "m", nil},
		{"duplicate ignoring case", []DataTableColumn{str("Owner"), {Name: "owner", Type: DataTableColumnInt}}, "Owner", ErrDataTableColumnDuplicate},
		{"key ignores case", []DataTableColumn{str("Host")}, "HOST", nil},
		{"empty key", []DataTableColumn{str("host")}, "", ErrDataTableKey},
		{"key not a column", []DataTableColumn{str("host")}, "ip", ErrDataTableKey},
		{"float key", []DataTableColumn{{Name: "score", Type: DataTableColumnFloat}}, "score", ErrDataTableKey},
		{"ip key", []DataTableColumn{{Name: "ip", Type: DataTableColumnIP}}, "ip", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDataTableColumns(tt.cols, tt.key)
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

func TestValidateDataTableName(t *testing.T) {
	for _, name := range []string{"hosts", "_scratch", "web-hosts_2", strings.Repeat("a", DataTableMaxNameLength)} {
		if err := ValidateDataTableName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "2hosts", "-hosts", "web hosts", "a.b", strings.Repeat("a", DataTableMaxNameLength+1)} {
		if err := ValidateDataTableName(name); !errors.Is(err, ErrDataTableName) {
			t.Errorf("%q: got %v, want %v", name, err, ErrDataTableName)
		}
	}
}

func TestDataTableValidateChange(t *testing.T) {
	prev := DataTable{Key: "host", Columns: []DataTableColumn{
		{Name: "host", Type: DataTableColumnString},
		{Name: "count", Type: DataTableColumnInt},
	}}
	tests := []struct {
		name string
		next DataTable
		want error
	}{
		{"other columns change", DataTable{Key: "host", Columns: []DataTableColumn{{Name: "count", Type: DataTableColumnString}, {Name: "host", Type: DataTableColumnString}}}, nil},
		{"key case", DataTable{Key: "HOST", Columns: []DataTableColumn{{Name: "Host", Type: DataTableColumnString}}}, nil},
		{"new key", DataTable{Key: "count", Columns: prev.Columns}, ErrDataTableKeyChanged},
		{"key type", DataTable{Key: "host", Columns: []DataTableColumn{{Name: "host", Type: DataTableColumnIP}}}, ErrDataTableKeyChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.next.ValidateChange(prev)
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

func TestDataTableRowValuesKeepIntPrecision(t *testing.T) {
	const big = `9007199254740993` // 2^53 + 1, which a float64 can't hold
	var row DataTableRow
	if err := json.Unmarshal([]byte(`{"Values":{"host":"k","n":`+big+`}}`), &row); err != nil {
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

func TestDataTableRowWriteExpectedVersion(t *testing.T) {
	w := DataTableRowWrite{Values: DataTableRowValues{"a": jsontext.Value(`"x"`)}}
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

func TestDataTableRowPatchMarshal(t *testing.T) {
	b, err := json.Marshal(DataTableRowPatch{Clear: []string{"ip"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"Clear":["ip"]}` {
		t.Fatalf("clear-only patch marshaled to %s", b)
	}
}

func TestDataTablePatchMarshal(t *testing.T) {
	b, err := json.Marshal(DataTablePatch{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{}` {
		t.Fatalf("empty patch marshaled to %s, want {}", b)
	}

	tbl := DataTable{Key: "owner", Columns: []DataTableColumn{{Name: "owner", Type: DataTableColumnString, Indexed: true}}}
	if b, err = json.Marshal(tbl.ToPatch()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Columns []DataTableColumn
		Key     *string
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 1 || got.Columns[0] != tbl.Columns[0] {
		t.Fatalf("Columns didn't round trip: %s", b)
	}
	if got.Key != nil {
		t.Fatalf("a patch can't change the key, but it sent one: %s", b)
	}
}
