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
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/spf13/cobra"

	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
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
	var aliases = []string{"macro", "m"}
	return treeutils.GenerateNav("macros", "manage search macros", "Macros are search keywords that expand to set phrases on use within a query.", aliases, []*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			edit(),
		})
}

//#region list

func list() action.Pair {
	return scaffoldlist.NewListAction("list your macros", "lists all macros associated to your user, a group, or the system itself",
		types.Macro{}, func(fs *pflag.FlagSet) ([]types.Macro, error) {
			if all, err := fs.GetBool("all"); err != nil {
				return nil, uniques.ErrGetFlag("macros list", err)
			} else if all { // fetch all macros instead of just user macros
				r, err := connection.Client.ListAllMacros(nil)
				if err != nil {
					return nil, err
				}
				return r.Results, nil
			}
			if gid, err := fs.GetInt32("group"); err != nil {
				return nil, uniques.ErrGetFlag("macros list", err)
			} else if gid != 0 { // fetch all macros our group ID can read
				macros, err := connection.Client.ListAllMacros(nil)
				if err != nil {
					return nil, err
				}
				var macroResults []types.Macro
				for _, m := range macros.Results {
					if m.GroupCanRead(gid) {
						macroResults = append(macroResults, m)
					}
				}
				return macroResults, nil
			}
			r, err := connection.Client.ListMacros(nil)
			if err != nil {
				return nil, err
			}
			return r.Results, nil
		},
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{AddtlFlags: flags},
			DefaultColumns: []string{"Name", "Description", "Expansion"},
		})
}

func flags() *pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "macros", "Supersedes --group")
	addtlFlags.Int32("group", 0, "fetches all macros shared with the given group id")
	return &addtlFlags
}

var macroNameRgx = regexp.MustCompile("^[a-zA-Z0-9_-]*$")

// creates macros using 3 fields: name, description, and expansion.
func create() action.Pair {

	nameField := scaffoldcreate.FieldName("macro")
	nameField.Provider = &scaffoldcreate.TextProvider{
		CustomInit: func() textinput.Model {
			ti := stylesheet.NewTI("", false)
			ti.Prompt = "$"
			ti.Validate = func(s string) error {
				s = strings.ToUpper(s)
				if !macroNameRgx.MatchString(s) {
					return errors.New("Macro names may contain capital letters, numbers, dashes and underscores")
				}

				if len(s) > 0 {
					char := []rune(s)[0]
					if !(unicode.IsDigit(char) || unicode.IsLetter(char)) {
						return errors.New("macro names must start with a letter or number")
					}

				}
				return nil
			}
			return ti
		},
	}

	fields := map[string]scaffoldcreate.Field{
		"name": nameField,
		"desc": scaffoldcreate.FieldDescription("macro"),
		"exp": scaffoldcreate.Field{
			Required:     true,
			Title:        "expansion",
			Flag:         scaffoldcreate.FlagConfig{Name: FlagExpansion, Usage: FlagExpansionUsage},
			Provider:     &scaffoldcreate.TextProvider{},
			DefaultValue: "",
			Order:        80,
		},
	}

	return scaffoldcreate.NewCreateAction("macro", fields,
		func(cfg map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (any, string, error) {
			sm := types.Macro{}
			// all three fields are required, no need to nil-check them
			sm.Name = strings.ToUpper(cfg["name"].Provider.Get())
			sm.Description = cfg["desc"].Provider.Get()
			sm.Expansion = cfg["exp"].Provider.Get()

			macro, err := connection.Client.CreateMacro(sm)
			return macro.ID, "", err

		}, scaffoldcreate.Options{})
}

func edit() action.Pair {
	const singular string = "macro"

	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("macro"),
		"description": scaffoldedit.FieldDescription("macro"),
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
			r, err := connection.Client.ListMacros(nil)
			return r.Results, err
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

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("macro", "macros", func(dryrun bool, id string) error {
		if dryrun {
			_, err := connection.Client.GetMacro(id)
			return err
		}
		return connection.Client.DeleteMacro(id)
	},
		func() ([]scaffolddelete.Item[string], error) {
			ms, err := connection.Client.ListMacros(nil)
			if err != nil {
				return nil, err
			}
			slices.SortFunc(ms.Results, func(m1, m2 types.Macro) int {
				return strings.Compare(m1.Name, m2.Name)
			})
			var items = make([]scaffolddelete.Item[string], len(ms.Results))
			for i, m := range ms.Results {
				items[i] = scaffolddelete.NewItem(
					m.Name,
					fmt.Sprintf("Expansion: '%v'\n%v",
						stylesheet.Cur.SecondaryText.Render(m.Expansion), m.Description),
					m.ID)
			}
			return items, nil
		})
}
