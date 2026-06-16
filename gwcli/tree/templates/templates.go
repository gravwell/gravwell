/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package templates defines the templates nav, which holds data related to... er, templates.
*/
package templates

import (
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "templates"
		short string = "manage templated queries"
		long  string = `Templates are special objects which define a Gravwell query containing variables.
Multiple templates using the same variable(s) can be included in a dashboard to create a powerful tool called an Investigative Dashboard.
For instance, templates which expect an IP address as their variable can be used to create an IP address investigation dashboard.`
	)
	return treeutils.GenerateNav(use, short, long, []string{"template"},
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			//create(),
			delete(),
			edit(),
			//download(),
		})
}

// wrap templates so we can better display variables
type wrappedTemplate struct {
	types.CommonFields

	Query     string
	Variables string
}

func wrap(ts []types.Template) []wrappedTemplate {
	wrapped := make([]wrappedTemplate, len(ts))
	for i, t := range ts {
		w := wrappedTemplate{
			CommonFields: t.CommonFields,

			Query: t.Query,
		}
		var vars = make([]string, len(t.Variables))
		for j, v := range t.Variables {
			var sb strings.Builder
			if v.Required {
				sb.WriteString("(required) ")
			}
			fmt.Fprintf(&sb, "%s=%s", v.Name, v.Label)
			if v.Description != "" {
				sb.WriteString(" \"")
				sb.WriteString(v.Description)
				sb.WriteString("\"")
			}
			if v.DefaultValue != "" {
				sb.WriteString(" Default: \"")
				sb.WriteString(v.DefaultValue)
				sb.WriteString("\"")
			}
			vars[j] = sb.String()
		}
		w.Variables = strings.Join(vars, ";")
		wrapped[i] = w
	}

	return wrapped

}

func list() action.Pair {
	const (
		short string = "list templates on the system"
		long  string = "view templates available to your user."
	)
	return scaffoldlist.NewListAction(short, long,
		wrappedTemplate{}, func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]wrappedTemplate, error) {
			resp, err := connection.Client.ListTemplates(params.QueryOpts)
			if err != nil {
				return nil, err
			}
			return wrap(resp.Results), nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{AddtlFlags: func() *pflag.FlagSet {
				addtlFlags := &pflag.FlagSet{}
				return addtlFlags
			}},

			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Query",
				"Variables",
			},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("template", "templates",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetTemplate(id)
				return err
			}
			return connection.Client.DeleteTemplate(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListTemplates(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(lr.Results))
			for i, t := range lr.Results {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        t.ID,
					Name:       t.Name,
					SecondLine: t.Description,
				}
			}

			return items, nil
		}, scaffolddelete.Options{})
}

func edit() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("template"),
		"description": scaffoldedit.FieldDescription("template"),
		"query": &scaffoldedit.Field{
			Required: true,
			Title:    "Query",
			Usage:    "the query string for this template",
			FlagName: "query",
			Order:    60,
		},
	}
	funcs := scaffoldedit.SubroutineSet[string, types.Template]{
		SelectSub: func(id string) (types.Template, error) {
			return connection.Client.GetTemplate(id)
		},
		FetchSub: func() ([]types.Template, error) {
			resp, err := connection.Client.ListTemplates(nil)
			return resp.Results, err
		},
		GetFieldSub: func(item types.Template, fieldKey string) (string, error) {
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
		SetFieldSub: func(item *types.Template, fieldKey, val string) (string, error) {
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
		GetTitleSub:       func(item types.Template) string { return item.Name },
		GetDescriptionSub: func(item types.Template) string { return item.Description },
		UpdateSub: func(data *types.Template) (string, error) {
			_, err := connection.Client.UpdateTemplate(*data)
			return data.Name, err
		},
	}
	return scaffoldedit.NewEditAction("template", "templates", cfg, funcs)
}
