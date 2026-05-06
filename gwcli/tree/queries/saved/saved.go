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
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

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
		})
}

//#region list

func list() action.Pair {
	return scaffoldlist.NewListAction("list your saved queries", "lists all saved queries associated to your user",
		types.SavedQuery{}, func(fs *pflag.FlagSet) ([]types.SavedQuery, error) {
			if all, err := fs.GetBool("all"); err != nil {
				return nil, uniques.ErrGetFlag("saved list", err)
			} else if all {
				r, err := connection.Client.ListAllSavedQueries(nil)
				if err != nil {
					return nil, err
				}
				return r.Results, nil
			}
			r, err := connection.Client.ListSavedQueries(nil)
			if err != nil {
				return nil, err
			}
			return r.Results, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{AddtlFlags: flags},
			DefaultColumns: []string{"CommonFields.ID", "CommonFields.Name", "CommonFields.Description", "Query"},
		})
}

func flags() *pflag.FlagSet {
	addtlFlags := &pflag.FlagSet{}
	ft.GetAll.Register(addtlFlags, true, "saved queries", "")
	return addtlFlags
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
		createQueryKey: scaffoldcreate.Field{
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
	return scaffolddelete.NewDeleteAction("saved query", "saved queries", func(dryrun bool, id string) error {
		if dryrun {
			_, err := connection.Client.GetSavedQuery(id)
			return err
		}
		return connection.Client.DeleteSavedQuery(id)
	},
		func() ([]scaffolddelete.Item[string], error) {
			qs, err := connection.Client.ListSavedQueries(nil)
			if err != nil {
				return nil, err
			}
			slices.SortFunc(qs.Results, func(q1, q2 types.SavedQuery) int {
				return strings.Compare(q1.Name, q2.Name)
			})
			var items = make([]scaffolddelete.Item[string], len(qs.Results))
			for i, q := range qs.Results {
				items[i] = scaffolddelete.NewItem(
					q.Name,
					fmt.Sprintf("Query: '%v'\n%v",
						stylesheet.Cur.SecondaryText.Render(q.Query), q.Description),
					q.ID)
			}
			return items, nil
		})
}

//#endregion delete
