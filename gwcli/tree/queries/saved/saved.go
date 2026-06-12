/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package saved provides actions for interacting with saved queries.
package saved

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewSavedNav returns a nav with children relating to saved query handling.
func NewSavedNav() *cobra.Command {
	var aliases = []string{"library", "searchlibrary"}
	return treeutils.GenerateNav("saved", "manage saved queries", "Saved queries are stored queries that can be retrieved and reused.", aliases, []*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			edit(),
			show(),
		})
}

//#region list

func list() action.Pair {
	return scaffoldlist.NewListAction("list your saved queries", "lists all saved queries associated to your user",
		types.SavedQuery{}, func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.SavedQuery, error) {
			r, err := connection.Client.ListSavedQueries(param.QueryOpts)
			return r.Results, err
		},
		nil,
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{},
			DefaultColumns: []string{"CommonFields.ID", "CommonFields.Name", "CommonFields.Description", "Query"},
		})
}

//#endregion list

//#region create

const (
	createNameKey  = "name"
	createDescKey  = "desc"
	createQueryKey = "query"
)

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		createNameKey: scaffoldcreate.FieldName("saved query"),
		createDescKey: scaffoldcreate.FieldDescription("saved query"),
		createQueryKey: {
			Required: true,
			Title:    "query",
			Flag:     scaffoldcreate.FlagConfig{Name: "query", Usage: "the query to save"},
			Provider: &scaffoldcreate.TextProvider{},
			Order:    80,
		},
	}

	return scaffoldcreate.NewCreateAction("saved query", fields,
		func(cfg map[string]scaffoldcreate.Field, _ *pflag.FlagSet) (any, string, error) {
			sq := types.SavedQuery{}
			sq.Name = cfg[createNameKey].Provider.Get()
			sq.Description = cfg[createDescKey].Provider.Get()
			sq.Query = cfg[createQueryKey].Provider.Get()

			result, err := connection.Client.CreateSavedQuery(sq)
			return result.ID, "", err
		}, scaffoldcreate.Options{})
}

//#endregion create

//#region edit

const singular string = "saved query"

func edit() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName(singular),
		"description": scaffoldedit.FieldDescription(singular),
		"query": &scaffoldedit.Field{
			Required: true,
			Title:    "query",
			Usage:    "the query to save",
			FlagName: "query",
			Order:    80,
		},
	}

	funcs := scaffoldedit.SubroutineSet[string, types.SavedQuery]{
		SelectSub: func(id string) (item types.SavedQuery, err error) {
			return connection.Client.GetSavedQuery(id)
		},
		FetchSub: func() ([]types.SavedQuery, error) {
			r, err := connection.Client.ListSavedQueries(nil)
			return r.Results, err
		},
		GetFieldSub: func(item types.SavedQuery, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "query":
				return item.Query, nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.SavedQuery, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				item.Name = val
			case "description":
				item.Description = val
			case "query":
				item.Query = val
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub: func(item types.SavedQuery) string {
			return item.Name
		},
		GetDescriptionSub: func(item types.SavedQuery) string {
			return fmt.Sprintf("%s\nQuery: %s", item.Description, item.Query)
		},
		UpdateSub: func(data *types.SavedQuery) (identifier string, err error) {
			result, err := connection.Client.UpdateSavedQuery(*data)
			return result.Name, err
		},
	}

	return scaffoldedit.NewEditAction(singular, "saved queries", cfg, funcs)
}

//#endregion edit

//#region delete

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("saved query", "saved queries",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetSavedQuery(id)
				return err
			}
			return connection.Client.DeleteSavedQuery(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListSavedQueries(nil)
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(lr.Results))
			for i, p := range lr.Results {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        p.ID,
					Name:       p.Name,
					SecondLine: p.Description,
				}
			}

			return items, nil
		}, scaffolddelete.Options{})
}

//#endregion delete

//#region show

func show() action.Pair {
	return scaffold.NewBasicAction("show", "display a saved query",
		"Display the full details of a saved query by its ID.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			id := fs.Arg(0)
			sq, err := connection.Client.GetSavedQuery(id)
			if err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("Name:        %s\nDescription: %s\nQuery:       %s",
				sq.Name, sq.Description, sq.Query), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"print", "get"},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("saved query ID"), nil
				}
				return "", nil
			},
		})
}

//#endregion show
