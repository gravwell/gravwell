// Package files provides utilities for working with userfiles.
package files

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "files"
		short string = "manage extra files you have uploaded"
		long  string = "Files can be used to store small files for use in playbooks, cover images for kits, etc.\n" +
			"See https://docs.gravwell.io/gui/files/files.html for more information."
	)
	return treeutils.GenerateNav(use, short, long, []string{"uf", "userfiles", "userfile"}, nil,
		[]action.Pair{
			list(),
		})
}

func list() action.Pair {
	const (
		short string = "list userfiles on the system"
		long  string = "Lists information about the userfiles you have access to."
	)
	return scaffoldlist.NewListAction(short, long,
		types.UserFileDetails{}, func(fs *pflag.FlagSet) ([]types.UserFileDetails, error) {
			return connection.Client.UserFiles()
		},
		scaffoldlist.Options{
			// TODO update column names once userfiles get the registry treatment
			DefaultColumns: []string{"Name", "Type", "Labels", "Size"},
			ColumnAliases:  map[string]string{"Size": "SizeBytes"},
		})
}
