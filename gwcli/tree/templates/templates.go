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
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
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
			//download(),
			//edit(),
		})
}

func list() action.Pair {
	const (
		short string = "list templates on the system"
		long  string = "view templates available to your user."
	)
	return scaffoldlist.NewListAction(short, long,
		types.Template{}, func(fs *pflag.FlagSet) ([]types.Template, error) {
			if all, err := fs.GetBool("all"); err != nil {
				uniques.ErrGetFlag("templates list", err)
			} else if all {
				resp, err := connection.Client.ListAllTemplates(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			}

			resp, err := connection.Client.ListTemplates(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{AddtlFlags: func() *pflag.FlagSet {
				addtlFlags := &pflag.FlagSet{}
				ft.GetAll.Register(addtlFlags, true, "templates")
				return addtlFlags
			}},

			DefaultColumns: []string{
				"ID",
				"Name",
				"Description",
				"Query",
				"Variables",
				"Labels",
			},
		})
}

// TODO we will need a submodule to handle variables. Create currently cannot handle this.
/*func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name":  scaffoldcreate.FieldName("template"),
		"desc":  scaffoldcreate.FieldDescription("template"),
		"query": scaffoldcreate.FieldPath("template"),
		"variables": scaffoldcreate.Field{
			Required: false,
			CustomTIFuncInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				// TODO
				return ti
			},
		},
		"labels": scaffoldcreate.FieldLabels(),
	}

	return scaffoldcreate.NewCreateAction("resource", fields,
		func(cfg map[string]scaffoldcreate.Field, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// check that path is valid and the file exists
			if fi, err := os.Stat(fieldValues["path"]); err != nil {
				switch {
				case errors.Is(err, filesystem.ErrNotExist):
					return "", fmt.Sprintf("file '%v' not found", fieldValues["path"]), nil
				}
				return "", fmt.Sprintf("failed to access path: %v", err), nil
			} else if fi.IsDir() {
				return "", "path must point to a file", nil
			}
			// transmute to resource struct
			var labels []string
			if lbls, found := fieldValues["labels"]; !found {
				return "", "", errors.New("failed to find \"labels\" field")
			} else if lbls = strings.TrimSpace(lbls); lbls != "" {
				labels = strings.Split(lbls, ",")
			}

			data := types.Resource{
				CommonFields: types.CommonFields{
					Name:        fieldValues["name"],
					Description: fieldValues["desc"],
					Labels:      labels,
				},
			}

			resp, err := connection.Client.CreateResource(data)
			// upload the file
			f, err := os.Open(fieldValues["path"])
			if err != nil {
				errStr := fmt.Sprintf("created resource, but failed to populate it: %v", err)
				clilog.Writer.Warn(errStr, rfc5424.SDParam{Name: "stage", Value: "open file"})
				return resp.ID, "", errors.New(errStr)
			}
			defer f.Close()
			b, err := io.ReadAll(f)
			if err != nil {
				errStr := fmt.Sprintf("created resource, but failed to populate it: %v", err)
				clilog.Writer.Warn(errStr, rfc5424.SDParam{Name: "stage", Value: "slurp file"})
				return resp.ID, "", errors.New(errStr)
			}
			if err := connection.Client.PopulateResource(resp.ID, b); err != nil {
				errStr := fmt.Sprintf("created resource, but failed to populate it: %v", err)
				clilog.Writer.Warn(errStr, rfc5424.SDParam{Name: "stage", Value: "populate"})
				return resp.ID, "", errors.New(errStr)
			}

			return resp.ID, "", err
		}, nil)
}*/

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("template", "templates",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetTemplate(id)
				return err
			}
			return connection.Client.DeleteTemplate(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			templates, err := connection.Client.ListAllTemplates(nil)
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(templates.Results,
				func(a, b types.Template) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(templates.Results))
			for i, r := range templates.Results {
				items[i] = scaffolddelete.NewItem(r.Name, r.Description, r.ID)
			}
			return items, nil
		})
}
