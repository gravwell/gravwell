/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package resources defines the resources nav, which holds data related to persistent data.
*/
package resources

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewResourcesNav() *cobra.Command {
	const (
		use   string = "resources"
		short string = "manage persistent search data"
		long  string = "Resources store persistent data for use in searches." +
			" Resources can be manually uploaded by a user or automatically created by search modules." +
			" Resources are used by a number of modules for things such as storing lookup tables," +
			" scripts, and more. A resource is simply a stream of bytes."
	)
	return treeutils.GenerateNav(use, short, long, nil,
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			download(),
		})
}

func list() action.Pair {
	const (
		short string = "list resources on the system"
		long  string = "view resources available to your user."
	)
	return scaffoldlist.NewListAction(short, long,
		types.Resource{}, func(fs *pflag.FlagSet) ([]types.Resource, error) {
			if all, err := fs.GetBool("all"); err != nil {
				uniques.ErrGetFlag("resources list", err)
			} else if all {
				resp, err := connection.Client.ListAllResources(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			}

			resp, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		scaffoldlist.Options{
			DefaultColumns: []string{
				"ID",
				"Name",
				"Description",
				"Size"},
			ColumnAliases: map[string]string{
				"Size": "SizeBytes",
			},
			AddtlFlags: flags,
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "resources")
	return addtlFlags
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download a resource", "Download a resource for use locally.\n"+
		"Prints to STDOUT until -o is specified.\n"+
		"Because resources can be shared, and resources are not required to have globally-unique names,"+
		"the following precedence is used when selecting a resource by user-friendly name:\n"+
		"1. Resources owned by the user always have highest priority\n"+
		"2. Resources shared with a group to which the user belongs are next\n"+
		"3. Global resources are the lowest priority.",
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			// arg length checked by the options
			id := fs.Arg(0)
			var out io.Writer = cmd.OutOrStdout()
			if outPath, err := fs.GetString(ft.Output.Name()); err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			} else if outPath != "" {
				out, err = os.Create(outPath)
				if err != nil {
					return err.Error(), nil
				}
			}

			data, err := connection.Client.GetResource(id)
			if err != nil {
				return err.Error(), nil
			}
			// spit out to stdout or file
			fmt.Fprintf(out, "%s", data)
			return "", nil
		},
		scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				ft.Output.Register(&fs)
				return fs
			},
			CmdMods: func(cmd *cobra.Command) {
				cmd.SetUsageFunc(func(c *cobra.Command) error {
					fmt.Fprintf(c.OutOrStdout(), "%s %s %s", c.Use, ft.Optional("FLAGS"), ft.Mandatory("resource ID"))
					return nil
				})
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return "you must specify exactly 1 argument (resource ID)", nil
				}
				return "", nil
			},
		},
	)
}

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name": {
			Required:      true,
			Title:         "name",
			Usage:         "name of the new resource",
			Type:          scaffoldcreate.Text,
			FlagName:      "name",
			FlagShorthand: 'n',
			Order:         100,
		},
		"desc": {
			Required:      false,
			Title:         "description",
			Usage:         ft.Description.Usage("resource"),
			Type:          scaffoldcreate.Text,
			FlagName:      ft.Description.Name(),
			FlagShorthand: 'd',
			Order:         90,
		},
	}

	return scaffoldcreate.NewCreateAction("resource", fields,
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// transmute to resource struct
			data := types.Resource{
				CommonFields: types.CommonFields{
					Name:        fieldValues["name"],
					Description: fieldValues["desc"],
				},
			}

			resp, err := connection.Client.CreateResource(data)
			return resp.ID, "", err
		}, nil)
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("resource", "resources",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetResourceMetadata(id)
				return err
			}
			return connection.Client.DeleteResource(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			resources, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(resources.Results,
				func(a, b types.Resource) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(resources.Results))
			for i, r := range resources.Results {
				items[i] = scaffolddelete.NewItem(r.Name, r.Description, r.ID)
			}
			return items, nil
		})
}
