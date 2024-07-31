package scaffoldlist

import (
	"testing"
)

func Test_format_String(t *testing.T) {
	tests := []struct {
		name string
		f    outputFormat
		want string
	}{
		{"JSON", json, "JSON"},
		{"CSV", csv, "CSV"},
		{"table", table, "table"},
		{"unknown", 5, "unknown format (5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.String(); got != tt.want {
				t.Errorf("format.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
