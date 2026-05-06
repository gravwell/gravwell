/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package resources defines the resources nav, which holds data related to persistent data.
*/
package resources

import (
	"errors"
	"fmt"
	"io"
	filesystem "io/fs"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
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

func NewResourcesNav() *cobra.Command {
	const (
		use   string = "resources"
		short string = "manage persistent search data"
		long  string = "Resources store persistent data for use in searches." +
			" Resources can be manually uploaded by a user or automatically created by search modules." +
			" Resources are used by a number of modules for things such as storing lookup tables," +
			" scripts, and more. A resource is simply a stream of bytes."
	)
	return treeutils.GenerateNav(use, short, long, []string{"resource"},
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			download(),
			edit(),
		})
}

func list() action.Pair {
	const (
		short string = "list resources on the system"
		long  string = "view resources available to your user."
	)
	return scaffoldlist.NewListAction(short, long,
		types.Resource{}, func(fs *pflag.FlagSet) ([]types.Resource, error) {
			if all, err := fs.GetBool("all"); err != nil {
				uniques.ErrGetFlag("resources list", err)
			} else if all {
				resp, err := connection.Client.ListAllResources(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			}

			resp, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		map[string]string{"Size": "SizeBytes"},
		scaffoldlist.Options{
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"Size",
				"ContentType",
			},
			CommonOptions: scaffold.CommonOptions{AddtlFlags: flags},
		})
}

func flags() *pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "resources")
	return &addtlFlags
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download a resource", "Download a resource for use locally.\n"+
		"Prints to STDOUT unless -o is specified.\n"+
		"You may specify resource by name or ID.\n\n"+
		"Because resources can be shared, and resources are not required to have globally-unique names,"+
		"the following precedence is used when selecting a resource by user-friendly name:\n"+
		"1. Resources owned by the user always have highest priority\n"+
		"2. Resources shared with a group to which the user belongs are next\n"+
		"3. Global resources are the lowest priority.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			// arg length checked by the options
			id := fs.Arg(0)
			outPath, err := fs.GetString(ft.Output.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			}
			clilog.Writer.Info("downloading resource", rfc5424.SDParam{Name: "resource_ID", Value: id})
			data, err := connection.Client.GetResource(id)
			if err != nil {
				return err.Error(), nil
			}
			// write to file or stdout
			if outPath != "" {
				out, err := os.Create(outPath)
				if err != nil {
					return err.Error(), nil
				}
				defer out.Close()
				n, err := out.WriteString(string(data))
				if err != nil {
					return err.Error(), nil
				}
				return phrases.SuccessfullyWroteToFile(n, outPath), nil
			}
			return string(data), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: fmt.Sprintf("%s %s %s", "download", ft.Optional("FLAGS"), ft.Mandatory("resource ID")),
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Output.Register(fs)
					return fs
				},
			},

			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("resource ID"), nil
				}
				return "", nil
			},
		},
	)
}

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name":   scaffoldcreate.FieldName("resource"),
		"desc":   scaffoldcreate.FieldDescription("resource"),
		"path":   scaffoldcreate.FieldPath("resource"),
		"labels": scaffoldcreate.FieldLabels(),
	}

	return scaffoldcreate.NewCreateAction("resource", fields,
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			filePath := cfg["path"].Provider.Get()
			// check that path is valid and the file exists
			if fi, err := os.Stat(filePath); err != nil {
				switch {
				case errors.Is(err, filesystem.ErrNotExist):
					return "", fmt.Sprintf("file '%v' not found", filePath), nil
				}
				return "", fmt.Sprintf("failed to access path: %v", err), nil
			} else if fi.IsDir() {
				return "", "path must point to a file", nil
			}
			// transmute to resource struct
			var labels []string
			if lbls := cfg["labels"].Provider.Get(); strings.TrimSpace(lbls) != "" {
				labels = strings.Split(strings.TrimSpace(lbls), ",")
			}

			data := types.Resource{
				CommonFields: types.CommonFields{
					Name:        cfg["name"].Provider.Get(),
					Description: cfg["desc"].Provider.Get(),
					Labels:      labels,
				},
			}

			resp, err := connection.Client.CreateResource(data)
			// upload the file
			f, err := os.Open(filePath)
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
		}, scaffoldcreate.Options{})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("resource", "resources",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetResourceMetadata(id)
				return err
			}
			return connection.Client.DeleteResource(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			resources, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(resources.Results,
				func(a, b types.Resource) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(resources.Results))
			for i, r := range resources.Results {
				items[i] = scaffolddelete.NewItem(r.Name, r.Description, r.ID)
			}
			return items, nil
		})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("resource", "resources", scaffoldedit.Config{
		"name":   scaffoldedit.FieldName("resource"),
		"desc":   scaffoldedit.FieldDescription("resource"),
		"labels": scaffoldedit.FieldLabels(),
	}, scaffoldedit.SubroutineSet[string, types.Resource]{
		SelectSub: func(id string) (item types.Resource, err error) { // get a specific resource
			return connection.Client.GetResourceMetadata(id)
		},
		FetchSub: func() (items []types.Resource, err error) { // get all available resources
			resp, err := connection.Client.ListResources(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		GetFieldSub: func(item types.Resource, fieldKey string) (value string, err error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "desc":
				return item.Description, nil
			case "labels":
				return strings.Join(item.Labels, ","), nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.Resource, fieldKey, val string) (invalid string, err error) {
			if item == nil {
				return "", errors.New("cannot set nil item")
			}
			switch fieldKey {
			case "name":
				if strings.Contains(val, " ") {
					return "name may not contain spaces", nil
				}
				val = strings.ToUpper(val)
				item.Name = val
			case "desc":
				item.Description = val
			case "labels":
				item.Labels = strings.Split(val, ",")
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return
		},
		GetTitleSub: func(item types.Resource) string {
			return item.Name
		},
		GetDescriptionSub: func(item types.Resource) string {
			return item.Description
		},
		UpdateSub: func(data *types.Resource) (identifier string, err error) {
			return data.Name, connection.Client.UpdateResourceMetadata(data.ID, *data)
		},
	})
}
