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
	"encoding/json"
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
	const (
		use   string = "templates"
		short string = "manage templated queries"
		long  string = `Templates are special objects which define a Gravwell query containing variables.
Multiple templates using the same variable(s) can be included in a dashboard to create a powerful tool called an Investigative Dashboard.
For instance, templates which expect an IP address as their variable can be used to create an IP address investigation dashboard.`
	)
	return treeutils.GenerateNav(use, short, long,
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			delete(),
			edit(),
			show(),
			create(),
			jsonAction(),
		},
		treeutils.NodeOptions{CommandAliases: []string{"template"}},
	)
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
			},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.TemplateRead},
					XPermissions: []types.Capability{types.TemplateRead},
				},
			},

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
	return scaffolddelete.NewDeleteAction("template",
		func(dryrun bool, id string, _ *pflag.FlagSet) error {
			if dryrun {
				_, err := connection.Client.GetTemplate(id)
				return err
			}
			return connection.Client.DeleteTemplate(id)
		},
		func(params scaffolddelete.DataParameters) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListTemplates(params.QueryOpts)
			if err != nil {
				return nil, err
			}

			return listitem.WrapAssets(lr.Results), nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.TemplateRead, types.TemplateWrite},
					XPermissions: []types.Capability{types.TemplateWrite},
				},
			},
			QueryOptionsFlags: scaffold.QOInclude{Everything: true},
		})
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
			_, err := connection.Client.UpdateTemplate(data.ID, data.ToPatch())
			return data.Name, err
		},
	}
	return scaffoldedit.NewEditAction("template", "templates", cfg, funcs,
		scaffoldedit.Options{
			CommonOptions: scaffold.CommonOptions{
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.TemplateRead, types.TemplateWrite},
					XPermissions: []types.Capability{types.TemplateRead, types.TemplateWrite},
				},
			},
		})
}

// for to/from JSON
type content struct {
	Query     string
	Variables []types.TemplateVariable
}

func show() action.Pair {
	return scaffoldselect.NewSelectAction("display template contents", "Display the contents of a template", "template",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListTemplates(nil) // TODO need to pass in params
			if err != nil {
				return nil, err
			}
			return listitem.WrapAssets(lr.Results), nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			asJSON, err := addtlFlags.GetBool(ft.JSON.Name())
			clilog.GetFlag(err)

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				template, err := connection.Client.GetTemplate(ID)
				if phrases.IsNotFoundErr(err) {
					results[i] = scaffold.Result{
						Output: phrases.ErrUnknownIdentifier(ID, "template ID").Error(),
					}
					continue
				} else if err != nil {
					clilog.Writer.Warn("failed to get template", log.KV("ID", ID), log.KVErr(err))
					results[i] = scaffold.Result{
						Output: err.Error(),
					}
					continue
				}
				// compose output
				if asJSON {
					content := content{Query: template.Query, Variables: template.Variables}
					b, err := json.Marshal(content)
					if err != nil {
						clilog.Writer.Error("failed to marshal content", log.KV("content", content), log.KVErr(err))
						results[i] = scaffold.Result{Output: "failed to marshal content: " + err.Error()}
						continue
					}
					results[i] = scaffold.Result{Success: true, Output: string(b)}
					continue
				}
				var sb strings.Builder
				sb.WriteString("ID ")
				sb.WriteString(ID)
				sb.WriteString(": ")
				sb.WriteString(template.Query)
				sb.WriteString("\n")
				for _, variable := range template.Variables {
					requiredString := ""
					if variable.Required {
						requiredString = " (required)"
					}
					fmt.Fprintf(&sb,
						"\t%s=%s%s\n"+
							"\t\t%s\n",
						variable.Name, variable.DefaultValue, requiredString,
						variable.Description)
				}

				results[i] = scaffold.Result{
					Success: true,
					Output:  sb.String()[:sb.Len()-1],
				}

			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "show",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.JSON.Register(fs)
					return fs
				},
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.TemplateRead},
					XPermissions: []types.Capability{types.TemplateRead},
				},
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("template",
		map[string]scaffoldcreate.Field{
			"name":   scaffoldcreate.FieldName("template"),
			"desc":   scaffoldcreate.FieldDescription("template"),
			"path":   scaffoldcreate.FieldPath("template specification", false),
			"labels": scaffoldcreate.FieldLabels(),
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var (
				content  content
				emptyStr string
			)
			if pth := strings.TrimSpace(fields["path"].Provider.Get()); pth != "" {
				b, err := os.ReadFile(pth)
				if err != nil {
					return 0, "", err
				}
				if err := json.Unmarshal(b, &content); err != nil {
					return 0, "", err
				}
			} else {
				emptyStr = " empty " // inserting into the success line to confirm that no actual data were given
			}

			newTemplate, err := connection.Client.CreateTemplate(types.Template{
				CommonFields: types.CommonFields{
					Name:        fields["name"].Provider.Get(),
					Description: fields["desc"].Provider.Get(),
					Labels:      scaffoldcreate.GetLabelsFromField(fields["labels"]),
				},
				Query:     content.Query,
				Variables: content.Variables,
			})
			if err != nil {
				return 0, "", err
			}
			return phrases.SuccessfullyCreatedItem(emptyStr+"template", newTemplate.ID), "", nil

		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Long: "Create a new template. It will be empty unless you specify a --path to a JSON file.\n" +
					"Call " + stylesheet.Path(true, "~", "templates", "json") + " to see the format of the JSON file.",
				Requirements: annotations.Requirements{
					IPermissions: []types.Capability{types.TemplateWrite},
					XPermissions: []types.Capability{types.TemplateWrite},
				},
			},

			IDIsSuccessMessage: true,
		})
}

func jsonAction() action.Pair {
	return scaffold.NewBasicAction("json", "display template JSON schema",
		"Print the JSON schema expected for creating templates via the cli.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			return `{
  "Query": "my example query",
  "Variables": [
    {
      "Name": "NAME1",
      "Label": "lbl",
      "Description": "my variable description",
      "Required": true,
      "DefaultValue": "default",
      "PreviewValue": "preview"
    },
    {
      "Name": "NAME2",
      "Label": "lbl",
      "Description": "my variable description",
      "Required": true,
      "DefaultValue": "default",
      "PreviewValue": "preview"
    }
  ]
}`, nil
		},
		scaffold.BasicOptions{},
	)

}
