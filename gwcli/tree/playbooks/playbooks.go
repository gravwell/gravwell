/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package playbooks provides actions for managing Gravwell playbooks.
package playbooks

import (
	"fmt"
	"os"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	return treeutils.GenerateNav("playbooks", "manage playbooks",
		"Playbooks are markdown documents that can be used to guide investigations.",
		[]string{"playbook"}, nil,
		[]action.Pair{
			listAction(),
			download(),
			create(),
			delete(),
			edit(),
		})
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction("list playbooks", "List playbooks available to your user.",
		types.Playbook{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.Playbook, error) {
			resp, err := connection.Client.ListPlaybooks(params.QueryOpts)
			return resp.Results, err
		},
		nil,
		scaffoldlist.Options{
			DefaultColumns: []string{
				"CommonFields.ID",
				"CommonFields.Name",
				"CommonFields.Description",
				"AuthorName",
			},
		})
}

func download() action.Pair {
	return scaffoldselect.NewSelectAction("download the content of a playbook",
		"Download the markdown body of a playbook for use locally.",
		"playbook", func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListPlaybooks(nil)
			if err != nil {
				return nil, err
			}
			data := make([]multiselectlist.SelectableItem[string], len(lr.Results))
			for i, pb := range lr.Results {
				data[i] = &listitem.Generic{
					ID_:        pb.ID,
					Name:       pb.Name,
					SecondLine: pb.Description,
				}
			}
			return data, nil
		},
		func(ID string, addtlFlags *pflag.FlagSet) (success string, _ error) {
			// check for output
			out, err := addtlFlags.GetString(ft.Output.Name())
			clilog.GetFlag(err)
			var f *os.File
			if out != "" {
				f, err = os.Create(out)
				if err != nil {
					return "", err
				}
				defer f.Close()
			}

			pb, err := connection.Client.GetPlaybook(ID)
			if err != nil {
				return "", err
			}
			if f != nil {
				n, err := f.WriteString(pb.Body)
				if err != nil {
					return "", err
				}
				return phrases.SuccessfullyWroteToFile(n, f.Name()), nil
			}
			return pb.Body + "\n", nil

		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "download",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Output.Register(fs)
					return fs
				},
			},
			Exactly1: true,
		})
}

// create allows creation of a playbook, optionally with content.
func create() action.Pair {
	path := scaffoldcreate.FieldPath("", true)
	path.Flag.Usage = "path to the markdown file to use as the playbook's contents"
	return scaffoldcreate.NewCreateAction("playbook",
		map[string]scaffoldcreate.Field{
			"name": scaffoldcreate.FieldName("playbook"),
			"desc": scaffoldcreate.FieldDescription("playbook"),
			"content": {
				Title:    "Content",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{Usage: "Markdown content of the new playbook." +
					"Use " + ft.Path.Name() + " to read content from a file instead."}, // TODO prepopulate this TA with the file's contents
				Order:    80,
				Provider: &scaffoldcreate.TextAreaProvider{},
			},
			"labels": scaffoldcreate.FieldLabels(),
		},
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (any, string, error) {
			// TODO because we cannot guarantee that content's custom SetArgs has run, we just have to always prefer the path variable if it was set.
			var body = cfg["content"].Provider.Get()
			if path, err := fs.GetString(ft.Path.Name()); err != nil {
				clilog.GetFlag(err)
			} else if path = strings.TrimSpace(path); path != "" {
				b, err := os.ReadFile(path)
				if err != nil {
					return "", err.Error(), nil
				}
				body = string(b)
			}

			pb := types.Playbook{
				CommonFields: types.CommonFields{
					Name:        cfg["name"].Provider.Get(),
					Description: cfg["desc"].Provider.Get(),
					Labels:      strings.Split(strings.TrimSpace(cfg["labels"].Provider.Get()), ","),
				},
				Body: body,
			}
			result, err := connection.Client.CreatePlaybook(pb)
			return result.ID, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					ft.Path.Register(fs, "", "markdown file")
					return fs
				},
			},
		})
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("playbook", "playbooks",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetPlaybook(id)
				return err
			}
			return connection.Client.DeletePlaybook(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			lr, err := connection.Client.ListPlaybooks(nil)
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

func edit() action.Pair {
	cfg := scaffoldedit.Config{
		"name":        scaffoldedit.FieldName("playbook"),
		"description": scaffoldedit.FieldDescription("playbook"),
		"body": &scaffoldedit.Field{
			Required: true,
			Title:    "body",
			Usage:    "markdown body content",
			FlagName: "body",
			Order:    40,
		},
	}
	funcs := scaffoldedit.SubroutineSet[string, types.Playbook]{
		SelectSub: func(id string) (types.Playbook, error) {
			return connection.Client.GetPlaybook(id)
		},
		FetchSub: func() ([]types.Playbook, error) {
			resp, err := connection.Client.ListPlaybooks(nil)
			return resp.Results, err
		},
		GetFieldSub: func(item types.Playbook, fieldKey string) (string, error) {
			switch fieldKey {
			case "name":
				return item.Name, nil
			case "description":
				return item.Description, nil
			case "body":
				return item.Body, nil
			}
			return "", fmt.Errorf("unknown field key: %v", fieldKey)
		},
		SetFieldSub: func(item *types.Playbook, fieldKey, val string) (string, error) {
			switch fieldKey {
			case "name":
				item.Name = val
			case "description":
				item.Description = val
			case "body":
				item.Body = val
			default:
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			}
			return "", nil
		},
		GetTitleSub:       func(item types.Playbook) string { return item.Name },
		GetDescriptionSub: func(item types.Playbook) string { return item.Description },
		UpdateSub: func(data *types.Playbook) (string, error) {
			_, err := connection.Client.UpdatePlaybook(*data)
			return data.Name, err
		},
	}
	return scaffoldedit.NewEditAction("playbook", "playbooks", cfg, funcs)
}
