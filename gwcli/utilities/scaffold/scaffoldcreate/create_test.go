/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate_test

import (
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
)

func TestCleanPathSuggestions(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		availSgts []string
		input     string
		want      []string
	}{
		{"input is directory",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "dir1/",
			[]string{"file1", "file2", "abc"}},
		{"no input",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "",
			[]string{"file1", "file2", "abc"}},
		{"input has no matches",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "unmatching",
			[]string{}},
		{"input is partial file match",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "dir1/",
			[]string{"file1", "file2", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaffoldcreate.TrimSuggestsToFile(tt.availSgts, tt.input)
			if !slices.Equal(tt.want, got) {
				t.Error(testsupport.ExpectedActual(tt.want, got))
			}
		})
	}
}
