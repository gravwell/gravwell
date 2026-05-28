/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package pivots provides actions for managing Gravwell pivots (actionable items).
package actionables

import (
	"encoding/json"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("actionables", "manage actionables",
		"Actionables are items that appear when hovering over data in the Gravwell web interface.\n"+ // TODO
			"They allow users to quickly pivot from a data value to a related search or action.\n"+
			"Actionable contents are stored as a JSON blob describing the actionable behaviour.",
		[]string{"pivot", "pivots", "actionable"}, nil,
		[]action.Pair{
			listAction(),
			create(),
			delete(),
		})
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list actionables",
		"List actionables available to your user.",
		types.Actionable{},
		func(fs *pflag.FlagSet) ([]types.Actionable, error) {
			lr, err := connection.Client.ListActionables(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return lr.Results, nil
		},
		nil,
		scaffoldlist.Options{
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Disabled",
			},
		})
}

// get is another list command, but provides better handling of the actual actionable triggers/commands
func get() // TODO

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("actionable",
		map[string]scaffoldcreate.Field{},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			spec := types.Actionable{}

			// extract common field values
			spec.CommonFields = types.CommonFields{
				Name:        fields["name"].Provider.Get(),
				Description: fields["description"].Provider.Get(),
			}
			// attempt to read from the given JSON
			jsonPath := fields["path"].Provider.Get()
			b, err := os.ReadFile(jsonPath)
			if err != nil {
				return 0, err.Error(), nil
			}
			if err := json.Unmarshal(b, spec.Contents); err != nil {
				return 0, err.Error(), nil
			}
			new, err := connection.Client.CreateActionable(spec)
			return new.ID, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{},
			Long: "Create a new actionable empty or from JSON." +
				"Call " + stylesheet.Cur.Action.Render("json") + " to view the required schema or" +
				" call " + stylesheet.Cur.Action.Render("get --json <ID>") + " to view an existing actionable as JSON.",
		})
}

// displays the json template used to populate actionables
func jsonAction() action.Pair {
	return scaffold.NewBasicAction("json", "display actionable JSON schema", "", // TODO
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			withComments, err := fs.GetBool("comments")
			clilog.GetFlag(err)
			if withComments {
				// TODO print comment version
				return
			}
			// TODO print normal JSON
			return
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("comments", false, "include comments in the json")
					return fs
				},
			},
		},
	)

}

// editAction updates a pivot's name and/or description.
/*func editAction() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("pivot"),
		"description": scaffoldedit.FieldDescription("pivot"),
	}
	funcs := scaffoldedit.SubroutineSet[string, types.WirePivot]{
		SelectSub: func(id string) (types.WirePivot, error) {
			uid, err := uuid.Parse(id)
			if err != nil {
				return types.WirePivot{}, err
			}
			return connection.Client.GetPivot(uid)
		},
		FetchSub: func() ([]types.WirePivot, error) {
			return connection.Client.ListPivots()
		},
		GetFieldSub: func(item types.WirePivot, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.WirePivot, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				item.Name = val
			case "description":
				item.Description = val
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub:       func(item types.WirePivot) string { return item.Name },
		GetDescriptionSub: func(item types.WirePivot) string { return item.Description },
		UpdateSub: func(data *types.WirePivot) (string, error) {
			uid := data.ThingUUID
			_, err := connection.Client.SetPivot(uid, *data)
			return data.Name, err
		},
	}
	return scaffoldedit.NewEditAction("pivot", "pivots", cfg, funcs)
}*/

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("pivot", "pivots",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetActionable(id)
				return err
			}
			return connection.Client.DeleteActionable(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListActionables(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(lr.Results))
			for i, p := range lr.Results {
				items[i] = &listitem.Generic{
					Selected_:    false,
					ID_:          p.ID,
					Name:         p.Name,
					SecondLine:   p.Description,
					ShowDisabled: true,
					Enabled:      !p.Disabled,
				}
			}

			return items, nil
		}, scaffolddelete.Options{})
}
