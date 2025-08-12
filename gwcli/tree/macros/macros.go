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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/spf13/cobra"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/spf13/pflag"
)

const (
	FlagExpansion      string = "expansion"
	FlagExpansionUsage string = "value for the macro to expand to"
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
	return scaffoldlist.NewListAction(listShort, listLong,
		types.Macro{}, listMacros,
		scaffoldlist.Options{AddtlFlags: flags, DefaultColumns: []string{"CommonFields.ID", "CommonFields.Name", "CommonFields.Description", "Expansion"}})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "macros", "Supersedes --group")
	addtlFlags.Int32("group", 0, "fetches all macros shared with the given group id")
	return addtlFlags
}

// lister subroutine for macros
func listMacros(fs *pflag.FlagSet) ([]types.Macro, error) {
	if all, err := fs.GetBool("all"); err != nil {
		uniques.ErrGetFlag("macros list", err)
	} else if all {
		return connection.Client.ListAllMacros()
	}
	if gid, err := fs.GetInt32("group"); err != nil {
		uniques.ErrGetFlag("macros list", err)
	} else if gid != 0 {
		macros, err := connection.Client.ListAllMacros()
		if err != nil {
			return nil, err
		}
		var ret []types.Macro
		for _, m := range macros {
			if m.GroupCanRead(gid) {
				ret = append(ret, m)
			}
		}
		return ret, nil
	}

	return connection.Client.ListMacros()
}

//#region create

func newMacroCreateAction() action.Pair {
	fields := scaffoldcreate.Config{
		"name": scaffoldcreate.Field{
			Required:     true,
			Title:        "name",
			Usage:        ft.Name.Usage("macro"),
			Type:         scaffoldcreate.Text,
			FlagName:     ft.Name.Name(),
			DefaultValue: "",
			Order:        100,
		},
		"desc": scaffoldcreate.Field{
			Required:     true,
			Title:        "description",
			Usage:        ft.Description.Usage("macro"),
			Type:         scaffoldcreate.Text,
			FlagName:     ft.Description.Name(),
			DefaultValue: "",
			Order:        90,
		},
		"exp": scaffoldcreate.Field{
			Required:     true,
			Title:        "expansion",
			Usage:        FlagExpansionUsage,
			Type:         scaffoldcreate.Text,
			FlagName:     FlagExpansion,
			DefaultValue: "",
			Order:        80,
		},
	}

	return scaffoldcreate.NewCreateAction("macro", fields, create, nil)
}

func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values, _ *pflag.FlagSet) (any, string, error) {
	sm := types.Macro{}
	// all three fields are required, no need to nil-check them
	sm.Name = strings.ToUpper(vals["name"])
	sm.Description = vals["desc"]
	sm.Expansion = vals["exp"]

	id, err := connection.Client.CreateMacro(sm)
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
			Usage:    ft.Name.Usage(singular),
			FlagName: ft.Name.Name(),
			Order:    100,
		},
		"description": &scaffoldedit.Field{
			Required: false,
			Title:    "Description",
			Usage:    ft.Description.Usage(singular),
			FlagName: ft.Description.Name(),
			Order:    80,
		},
		"expansion": &scaffoldedit.Field{
			Required: true,
			Title:    "Expansion",
			Usage:    FlagExpansionUsage,
			FlagName: FlagExpansion,
			Order:    60,
		},
	}

	funcs := scaffoldedit.SubroutineSet[string, types.Macro]{
		SelectSub: func(id string) (item types.Macro, err error) {
			return connection.Client.GetMacro(id)
		},
		FetchSub: func() ([]types.Macro, error) {
			return connection.Client.ListMacros()
		},
		GetFieldSub: func(item types.Macro, fieldKey string) (string, error) {
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
		SetFieldSub: func(item *types.Macro, fieldKey, val string) (string, error) {
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
		GetTitleSub: func(item types.Macro) string {
			return fmt.Sprintf("%s -> %v", item.Name, item.Expansion)
		},
		GetDescriptionSub: func(item types.Macro) string { return item.Description },
		UpdateSub: func(data *types.Macro) (identifier string, err error) {
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
		func() ([]scaffolddelete.Item[string], error) {
			ms, err := connection.Client.ListMacros()
			if err != nil {
				return nil, err
			}
			slices.SortFunc(ms, func(m1, m2 types.Macro) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[string], len(ms))
			for i, m := range ms {
				items[i] = scaffolddelete.NewItem(
					m.Name,
					fmt.Sprintf("Expansion: '%v'\n%v",
						stylesheet.Cur.SecondaryText.Render(m.Expansion), m.Description),
					m.ID)
			}
			return items, nil
		})
}

func del(dryrun bool, id string) error {
	if dryrun {
		_, err := connection.Client.GetMacro(id)
		return err
	}
	return connection.Client.DeleteMacro(id)
}

//#endregion delete
