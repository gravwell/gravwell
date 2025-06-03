/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package macros provides actions for interacting with macros. Makes sense, no?
package macros

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/spf13/cobra"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	grav "github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

// NewMacrosNav returns a nav with children relating to macro handling.
func NewMacrosNav() *cobra.Command {
	const (
		use   = "macros"
		short = "manage search macros"
		long  = "Macros are search keywords that expand to set phrases on use within a query."
	)
	var aliases = []string{"macro", "m"}
	return treeutils.GenerateNav(use, short, long, aliases, []*cobra.Command{},
		[]action.Pair{newMacroListAction(),
			newMacroCreateAction(),
			newMacroDeleteAction(),
			newMacroEditAction()})
}

//#region list

func newMacroListAction() action.Pair {
	const (
		listShort = "list your macros"
		listLong  = "lists all macros associated to your user, a group," +
			"or the system itself"
	)
	var listDefaultColumns = []string{"ID", "Name", "Description", "Expansion"}
	return scaffoldlist.NewListAction("", listShort, listLong, listDefaultColumns,
		types.SearchMacro{}, listMacros, flags)
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	addtlFlags.Bool(ft.Name.ListAll, false, ft.Usage.ListAll("macros")+"\n"+
		"Ignored if you are not an admin.\n"+
		"Supersedes --group.")
	addtlFlags.Int32("group", 0, "Fetches all macros shared with the given group id.")
	return addtlFlags
}

// lister subroutine for macros
func listMacros(c *grav.Client, fs *pflag.FlagSet) ([]types.SearchMacro, error) {
	if all, err := fs.GetBool(ft.Name.ListAll); err != nil {
		clilog.LogFlagFailedGet(ft.Name.ListAll, err)
	} else if all {
		return c.GetAllMacros()
	}
	if gid, err := fs.GetInt32("group"); err != nil {
		clilog.LogFlagFailedGet("group", err)
	} else if gid != 0 {
		return c.GetGroupMacros(gid)
	}

	return c.GetUserMacros(connection.MyInfo.UID)
}

//#region create

func newMacroCreateAction() action.Pair {
	n := scaffoldcreate.NewField(true, "name", 100)
	n.FlagShorthand = 'n'
	d := scaffoldcreate.NewField(true, "description", 90)
	d.FlagShorthand = 'd'

	fields := scaffoldcreate.Config{
		"name": n,
		"desc": d,
		"exp": scaffoldcreate.Field{
			Required:      true,
			Title:         "expansion",
			Usage:         ft.Usage.Expansion,
			Type:          scaffoldcreate.Text,
			FlagName:      ft.Name.Expansion,
			FlagShorthand: 'e',
			DefaultValue:  "",
			Order:         80,
		},
	}

	return scaffoldcreate.NewCreateAction("macro", fields, create, nil)
}

func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values, _ *pflag.FlagSet) (any, string, error) {
	sm := types.SearchMacro{}
	// all three fields are required, no need to nil-check them
	sm.Name = strings.ToUpper(vals["name"])
	sm.Description = vals["desc"]
	sm.Expansion = vals["exp"]

	id, err := connection.Client.AddMacro(sm)
	return id, "", err

}

//#endregion create

//#region edit

func newMacroEditAction() action.Pair {
	const singular string = "macro"

	cfg := scaffoldedit.Config{
		"name": &scaffoldedit.Field{
			Required: true,
			Title:    "Name",
			Usage:    ft.Usage.Name(singular),
			FlagName: ft.Name.Name,
			Order:    100,
		},
		"description": &scaffoldedit.Field{
			Required: false,
			Title:    "Description",
			Usage:    ft.Usage.Desc(singular),
			FlagName: ft.Name.Desc,
			Order:    80,
		},
		"expansion": &scaffoldedit.Field{
			Required: true,
			Title:    "Expansion",
			Usage:    ft.Usage.Expansion,
			FlagName: ft.Name.Expansion,
			Order:    60,
		},
	}

	funcs := scaffoldedit.SubroutineSet[uint64, types.SearchMacro]{
		SelectSub: func(id uint64) (item types.SearchMacro, err error) {
			return connection.Client.GetMacro(id)
		},
		FetchSub: func() ([]types.SearchMacro, error) {
			return connection.Client.GetUserMacros(connection.MyInfo.UID)
		},
		GetFieldSub: func(item types.SearchMacro, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "expansion":
				return item.Expansion, nil
			}

			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.SearchMacro, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				if strings.Contains(val, " ") {
					return "name may not contain spaces", nil
				}
				val = strings.ToUpper(val)
				item.Name = val
			case "description":
				item.Description = val
			case "expansion":
				item.Expansion = val
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub: func(item types.SearchMacro) string {
			return fmt.Sprintf("%s -> %v", item.Name, item.Expansion)
		},
		GetDescriptionSub: func(item types.SearchMacro) string { return item.Description },
		UpdateSub: func(data *types.SearchMacro) (identifier string, err error) {
			if err := connection.Client.UpdateMacro(*data); err != nil {
				return "", err
			}
			return data.Name, nil
		},
	}

	return scaffoldedit.NewEditAction(singular, "macros", cfg, funcs)
}

//#endregion edit

//#region delete

func newMacroDeleteAction() action.Pair {
	return scaffolddelete.NewDeleteAction("macro", "macros", del,
		func() ([]scaffolddelete.Item[uint64], error) {
			ms, err := connection.Client.GetUserGroupsMacros()
			if err != nil {
				return nil, err
			}
			slices.SortFunc(ms, func(m1, m2 types.SearchMacro) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[uint64], len(ms))
			for i, m := range ms {
				items[i] = scaffolddelete.NewItem(
					m.Name,
					fmt.Sprintf("Expansion: '%v'\n%v",
						stylesheet.Header2Style.Render(m.Expansion), m.Description),
					m.ID)
			}
			return items, nil
		})
}

func del(dryrun bool, id uint64) error {
	if dryrun {
		_, err := connection.Client.GetMacro(id)
		return err
	}
	return connection.Client.DeleteMacro(id)
}

//#endregion delete
