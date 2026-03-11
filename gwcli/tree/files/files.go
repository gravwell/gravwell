// Package files provides utilities for working with userfiles.
package files

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
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
			download(),
			create(),
		})
}

func list() action.Pair {
	const (
		short string = "list userfiles on the system"
		long  string = "Lists information about the userfiles you have access to."
	)
	return scaffoldlist.NewListAction(short, long,
		types.UserFileDetails{}, func(fs *pflag.FlagSet) ([]types.UserFileDetails, error) {
			// check for all
			all, err := fs.GetBool(ft.GetAll.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.GetAll.Name(), err)
			}

			if all {
				return connection.Client.AllUserFiles()
			}
			return connection.Client.UserFiles()
		},
		scaffoldlist.Options{
			AddtlFlags: func() pflag.FlagSet {
				var fs = pflag.FlagSet{}
				ft.GetAll.Register(&fs, true, "files")
				return fs
			},
			// TODO update column names once userfiles get the registry treatment
			DefaultColumns: []string{"Name", "Type", "Labels", "Size"},
			ColumnAliases:  map[string]string{"Size": "SizeBytes"},
		})
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download a file", "Download a file for use locally.",
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			// arg length checked by the options
			id := fs.Arg(0)

			// TODO remove me after registry updates
			u, err := uuid.Parse(id)
			if err != nil {
				return err.Error(), nil
			}

			// check output
			var out io.Writer = cmd.OutOrStdout()
			if outPath, err := fs.GetString(ft.Output.Name()); err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			} else if outPath != "" {
				f, err := os.Create(outPath)
				if err != nil {
					return err.Error(), nil
				}
				defer f.Close()
				out = f
			}

			b, err := connection.Client.GetUserFile(u)
			if err != nil {
				return err.Error(), nil
			}
			fmt.Fprintf(out, "%s", b)
			return "", nil // TODO what about a success message?
		}, scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				var fs pflag.FlagSet
				ft.Output.Register(&fs)
				return fs
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("file",
		map[string]scaffoldcreate.Field{
			"name": {
				Required:      true,
				Title:         "Name",
				Usage:         ft.Name.Usage("file"),
				Type:          scaffoldcreate.Text,
				FlagName:      ft.Name.Name(),
				FlagShorthand: rune(ft.Name.Shorthand()[0]),
				Order:         100},
			"desc": {
				Required:      false,
				Title:         "Description",
				Usage:         ft.Description.Usage("file"),
				Type:          scaffoldcreate.Text,
				FlagName:      ft.Description.Name(),
				FlagShorthand: rune(ft.Description.Shorthand()[0]),
				Order:         90},
			"path": {
				Required:      true,
				Title:         "Path",
				Usage:         ft.Path.Usage("file"),
				Type:          scaffoldcreate.Text,
				FlagName:      ft.Path.Name(),
				FlagShorthand: rune(ft.Path.Shorthand()[0]),
				Order:         80},
		},
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			id, err = connection.Client.AddUserFile(fieldValues["name"], fieldValues["desc"], fieldValues["path"])
			return
		}, nil)
}
