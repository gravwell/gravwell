//go:build ci

package scaffold_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestQOBuilder(t *testing.T) {
	// the flagset will always attempt to set the following values into the flagset, ignoring errors
	setFlags := func(fs *pflag.FlagSet) {
		fs.Parse(nil)
		fs.Set("all", "true")
		fs.Set("include-deleted", "true")
		fs.Set("limit", "5")
	}

	tests := []struct {
		name       string
		builder    scaffold.QOBuilder
		expectedQO types.QueryOptions
	}{
		{"omit everything", scaffold.QOOmit{Everything: true}, types.QueryOptions{}},
		{"omit all", scaffold.QOOmit{AllData: true}, types.QueryOptions{IncludeDeleted: true, Limit: 5}},
		{"omit limit", scaffold.QOOmit{Limit: true}, types.QueryOptions{AdminMode: true, IncludeDeleted: true}},

		{"include everything",
			scaffold.QOInclude{Everything: true},
			types.QueryOptions{AdminMode: true, IncludeDeleted: true, Limit: 5}},
		{"include include-deleted & limit",
			scaffold.QOInclude{IncludeDeleted: true, Limit: true},
			types.QueryOptions{IncludeDeleted: true, Limit: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.builder == nil {
				t.Skip("cannot test nil builder")
			}
			var fs = pflag.NewFlagSet("test", pflag.ContinueOnError) // always tries to set
			tt.builder.Install(fs)
			setFlags(fs)
			qo := tt.builder.QueryOptions(fs)
			require.EqualExportedValues(t, tt.expectedQO, *qo)
		})
	}
}
