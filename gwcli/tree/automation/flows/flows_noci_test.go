package flows_test

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/tree"
	"github.com/stretchr/testify/require"
)

func TestParseRejectsBadArguments(t *testing.T) {
	tests := []struct {
		name         string
		args         []string // the local arguments to give to `flows parse`
		expectZeroEC bool     // are we expecting a zero exit code?
	}{
		{"no arguments", []string{""}, false},
		{"space arguments", []string{" ", "		"}, false},
		{"all three methods", []string{"--stdin", "--path", "path/to/file", "abcdef"}, false},
		{"stdin and bare arguments", []string{"--stdin", "abcdef"}, false},
		{"path and bare arguments", []string{"--path", "path/to/file", "abcdef"}, false},
		{"stdin and path", []string{"--path", "path/to/file", "--stdin"}, false},
	}
	for _, tt := range tests {
		var sbOut, sbErr strings.Builder
		baseArgs := testsupport.MetaArgs(t, false, testsupport.WithDefaults())
		baseArgs = append(baseArgs, "flows", "parse")
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				sbOut.Reset()
				sbErr.Reset()
			})
			if tt.expectZeroEC {
				require.Zero(t, tree.Execute(append(baseArgs, tt.args...), tree.ExecuteOptions{
					Stdout: &sbOut,
					Stderr: &sbErr,
				}), "stdout: %v\n\nstderr%v", sbOut.String(), sbErr.String())
			} else {
				require.NotZero(t, tree.Execute(append(baseArgs, tt.args...), tree.ExecuteOptions{
					Stdout: &sbOut,
					Stderr: &sbErr,
				}), "stdout: %v\n\nstderr%v", sbOut.String(), sbErr.String())
			}
		})
	}
}
