/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

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
