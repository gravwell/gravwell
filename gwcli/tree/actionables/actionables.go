/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package actionables provides actions for managing actionables (formerly pivots).
package actionables

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("actionables", "manage actionables",
		"Actionables are items that appear when hovering over data in the Gravwell web interface.\n"+
			"They allow users to quickly pivot from a data value to a related search or action.\n"+
			"Actionable contents are stored as a JSON blob describing the actionable behavior.",
		nil,
		[]action.Pair{
			listAction(),
			get(),
			create(),
			delete(),
			jsonAction(),
			replace(),
			edit(),
		},
		treeutils.NodeOptions{CommandAliases: []string{"pivot", "pivots", "actionable"}},
	)
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list actionables",
		"List actionables available to your user.",
		types.Actionable{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.Actionable, error) {
			lr, err := connection.Client.ListActionables(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return lr.Results, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ActionableRead},
				},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Disabled",
			},
		})
}

// get is another list command, but provides better handling of the actual actionable triggers/commands
func get() action.Pair {
	return scaffold.NewBasicAction("get", "view actionables as JSON",
		"Display the JSON description of one or many actionables."+
			"These descriptions can be used to export/import actionables via "+stylesheet.Cur.Action.Render("create")+".",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			var sb strings.Builder
			for _, ID := range fs.Args() {
				a, err := connection.Client.GetActionable(ID)
				if err != nil {
					if phrases.IsNotFoundErr(err) {
						return ID + " is not a known actionable ID", nil
					}
					return err.Error(), nil
				}
				b, err := json.Marshal(a.Contents)
				if err != nil {
					return err.Error(), nil
				}
				sb.WriteString(string(b))
				sb.WriteString("\n")
			}
			return sb.String(), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: "get " + ft.VariadicArgs("actionable ID", true),
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ActionableRead},
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() < 1 {
					return phrases.AtLeast1ArgRequired("actionable IDs"), nil
				}
				return "", nil
			},
		})
}

func create() action.Pair {
	path := scaffoldcreate.FieldPath("", false)
	path.Flag.Usage = "path to the JSON file containing the actionable's contents. " +
		"If not provided, the actionable will be created empty."
	return scaffoldcreate.NewCreateAction("actionable",
		map[string]scaffoldcreate.Field{
			"name":        scaffoldcreate.FieldName("actionable"),
			"description": scaffoldcreate.FieldDescription("actionable"),
			"labels":      scaffoldcreate.FieldLabels(),
			"path":        path,
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// stuff common fields into the actionable
			spec := types.Actionable{
				CommonFields: types.CommonFields{
					Name:        fields["name"].Provider.Get(),
					Description: fields["description"].Provider.Get(),
					Labels:      scaffoldcreate.GetLabelsFromField(fields["labels"]),
				},
			}

			// attempt to read from the given JSON, if provided
			if jsonPath := fields["path"].Provider.Get(); jsonPath != "" {
				b, err := os.ReadFile(jsonPath)
				if err != nil {
					return 0, err.Error(), nil
				}
				if err := json.Unmarshal(b, &spec.Contents); err != nil {
					return 0, err.Error(), nil
				}
			}

			new, err := connection.Client.CreateActionable(spec)
			return new.ID, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Long: "Create a new actionable empty or from JSON. " +
					"Call " + stylesheet.Path(true, "~", "actionables", "json") + " to view the required schema or " +
					"call " + stylesheet.Path(true, "~", "actionables", "get", "<ID>") + " to view an existing actionable as JSON.",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ActionableWrite},
				},
			},
		})
}

// displays the json template used to populate actionables
func jsonAction() action.Pair {
	return scaffold.NewBasicAction("json", "display actionable JSON schema",
		"Print the JSON schema expected for creating Actionables via the cli.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			return `{
  "MenuLabel": "My Actionable",
  "Actions": [
    {
      "Name": "action1",
      "Description": "",
      "TriggerPlaceholder": "",
      "NoValueUrlEncode": true,
      "Start": {
        "Type": "timestamp or string",
        "Format": "Unix if (timestamp) || YYYY-MM-DDThh:mm if (type==string && empty)",
        "Placeholder": "_START_ if empty"
      },
      "End": {
        "Type": "timestamp or string",
        "Format": "Unix if (timestamp) || YYYY-MM-DDThh:mm if (type==string && empty)",
        "Placeholder": "_END_ if empty"
      },
      "Type": "query, template, dashboard, saved_query, url",
      "Reference": "",
      "Options": {
        "Variable": "Template and dashboard commands use Variable.",
        "ModalWidth": "URL commands use Modal, ModalWidth, and NoValueUrlEncode.",
        "NoValueUrlEncode": true
      }
    }
  ],
  "Triggers": [
    {
      "Pattern": "/javascript regex: see developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Regular_expressions/g",
      "ActivatesOn": "always or selection",
      "Disabled": false
    }
  ]
}`, nil
		},
		scaffold.BasicOptions{},
	)

}

func replace() action.Pair {
	return scaffoldselect.NewSelectAction("update the content of an actionable",
		"Replace the JSON content (viewable via "+stylesheet.Cur.Action.Render("get")+") of an actionable, changing its operation/definition",
		"actionable ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListActionables(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, fs *pflag.FlagSet) (_ []scaffold.Result, _ error) {
			// This is an exactly1 function
			if len(IDs) != 1 {
				clilog.Writer.Warn("exactly1 function with incorrect number of IDs made it to operate()",
					log.KV("count", len(IDs)),
					scaffold.IdentifyCaller())
				if len(IDs) == 0 {
					return nil, errors.New(phrases.Exactly1ArgRequired("actionable ID"))
				}
			}
			// fetch the actionables existing metadata; we only want to update the contents
			a, err := connection.Client.GetActionable(IDs[0])
			if err != nil {
				if phrases.IsNotFoundErr(err) {
					return nil, phrases.ErrUnknownIdentifier(IDs[0], "actionable ID")
				}
				return nil, err
			}
			// unmarshal the given json as contents
			pth, _ := fs.GetString(ft.Path.Name())
			f, err := os.Open(pth)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			dcdr := json.NewDecoder(f)
			if err := dcdr.Decode(&a.Contents); err != nil {
				return nil, err
			}
			a, err = connection.Client.UpdateActionable(a.ID, a.ToPatch())
			if err != nil {
				return nil, err
			}
			return []scaffold.Result{
				{Output: fmt.Sprintf("replaced actionable definition of %s (ID: %s)", a.Name, a.ID), Success: true},
			}, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "replace",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "local file to replace the remote file")
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ActionableRead, types.ActionableWrite},
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				pth, err := fs.GetString(ft.Path.Name())
				clilog.GetFlag(err)
				if pth == "" {
					return "--path is required", nil
				}
				return "", nil
			},
			Exactly1: true,
		})
}

// edit allows a user to update the basic metadata of an actionable.
func edit() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("actionable"),
		"description": scaffoldedit.FieldDescription("actionable"),
		"labels":      scaffoldedit.FieldLabels(),
	}
	funcs := scaffoldedit.SubroutineSet[string, types.Actionable]{
		SelectSub: func(ID string) (types.Actionable, error) {
			return connection.Client.GetActionable(ID)
		},
		FetchSub: func() ([]types.Actionable, error) {
			lr, err := connection.Client.ListActionables(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			return lr.Results, nil
		},
		GetFieldSub: func(item types.Actionable, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "labels":
				return strings.Join(item.Labels, ","), nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.Actionable, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				item.Name = val
			case "description":
				item.Description = val
			case "labels":
				item.Labels = []string{}
				for lbl := range strings.SplitSeq(val, ",") {
					lbl = strings.TrimSpace(lbl)
					if lbl == "" {
						continue
					}
					item.Labels = append(item.Labels, lbl)
				}
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub:       func(item types.Actionable) string { return item.Name },
		GetDescriptionSub: func(item types.Actionable) string { return item.Description },
		UpdateSub: func(data *types.Actionable) (string, error) {
			result, err := connection.Client.UpdateActionable(data.ID, data.ToPatch())
			return result.Name, err
		},
	}
	return scaffoldedit.NewEditAction("actionable", "actionables", cfg, funcs,
		scaffoldedit.Options{CommonOptions: scaffold.CommonOptions{
			Requirements: annotations.Requirements{IPermissions: []types.Capability{types.ActionableRead}}}})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("actionable",
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetActionable(id)
				return err
			}
			return connection.Client.DeleteActionable(id)
		},
		func(param scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListActionables(param.QueryOpts)
			if err != nil {
				return nil, err
			}

			return listitem.WrapAssets(lr.Results), nil
		},
		scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.ActionableRead, types.ActionableWrite},
				},
			},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
		})
}
