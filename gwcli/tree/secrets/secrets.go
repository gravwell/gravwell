package secrets

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

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "secrets"
		short string = "manage secret data that can be fed into other actions"
		long  string = "Gravwell can store secret strings for use in other actions (typically flows)." +
			" Once created, the user cannot read the contents of the secret again, although they may change the value later." +
			" The user may then refer to the secret in certain node types when building a flow."
	)
	return treeutils.GenerateNav(use, short, long, []string{"secret"},
		[]*cobra.Command{},
		[]action.Pair{
			list(),
			create(),
			delete(),
			edit(),
		})
}

func list() action.Pair {
	const (
		short string = "list secrets on the system"
		long  string = "View secrets available to your user."
	)
	return scaffoldlist.NewListAction(short, long,
		types.Secret{}, func(fs *pflag.FlagSet) ([]types.Secret, error) {
			if all, err := fs.GetBool("all"); err != nil {
				uniques.ErrGetFlag("secrets list", err)
			} else if all {
				resp, err := connection.Client.ListAllSecrets(nil)
				if err != nil {
					return nil, err
				}
				return resp.Results, nil
			}

			resp, err := connection.Client.ListSecrets(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		scaffoldlist.Options{
			DefaultColumns: []string{
				"ID",
				"Name",
				"Description",
				"Labels",
			},
			AddtlFlags: flags,
		})
}

func flags() pflag.FlagSet {
	addtlFlags := pflag.FlagSet{}
	ft.GetAll.Register(&addtlFlags, true, "secrets")
	return addtlFlags
}

func create() action.Pair {
	fields := map[string]scaffoldcreate.Field{
		"name": scaffoldcreate.FieldName("secret"),
		"desc": scaffoldcreate.FieldDescription("secret"),
		"value": scaffoldcreate.Field{
			Required:      true,
			Title:         "Value",
			Usage:         "the secret itself",
			Type:          scaffoldcreate.Text,
			FlagShorthand: 'v',
			Order:         80,
		},
		"labels": scaffoldcreate.FieldLabels(),
	}

	return scaffoldcreate.NewCreateAction("secret", fields,
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			// transmute to resource struct
			var labels []string
			if lbls, found := fieldValues["labels"]; !found {
				return "", "", errors.New("failed to find \"labels\" field")
			} else if lbls = strings.TrimSpace(lbls); lbls != "" {
				labels = strings.Split(lbls, ",")
			}

			data := types.SecretCreate{
				CommonFields: types.CommonFields{
					Name:        fieldValues["name"],
					Description: fieldValues["desc"],
					Labels:      labels,
				},
				Value: fieldValues["value"],
			}

			resp, err := connection.Client.CreateSecret(data)
			if err != nil {
				return "", "", err
			}

			return resp.ID, "", err
		}, nil)
}

func delete() action.Pair {
	return scaffolddelete.NewDeleteAction("secret", "secret",
		func(dryrun bool, id string) error {
			if dryrun {
				_, err := connection.Client.GetSecret(id)
				return err
			}
			return connection.Client.DeleteSecret(id)
		},
		func() ([]scaffolddelete.Item[string], error) {
			secrets, err := connection.Client.ListSecrets(nil)
			if err != nil {
				return nil, err
			}
			slices.SortStableFunc(secrets.Results,
				func(a, b types.Secret) int {
					return strings.Compare(a.Name, b.Name)
				})
			var items = make([]scaffolddelete.Item[string], len(secrets.Results))
			for i, r := range secrets.Results {
				items[i] = scaffolddelete.NewItem(r.Name, r.Description, r.ID)
			}
			return items, nil
		})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("secret", "secret", scaffoldedit.Config{
		"name":   scaffoldedit.FieldName("secret"),
		"desc":   scaffoldedit.FieldDescription("secret"),
		"labels": scaffoldedit.FieldLabels(),
	}, scaffoldedit.SubroutineSet[string, types.Secret]{
		SelectSub: func(id string) (item types.Secret, err error) { // get a specific resource
			return connection.Client.GetSecret(id)
		},
		FetchSub: func() (items []types.Secret, err error) { // get all available resources
			resp, err := connection.Client.ListSecrets(nil)
			if err != nil {
				return nil, err
			}
			return resp.Results, nil
		},
		GetFieldSub: func(item types.Secret, fieldKey string) (value string, err error) {
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
		SetFieldSub: func(item *types.Secret, fieldKey, val string) (invalid string, err error) {
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
		GetTitleSub: func(item types.Secret) string {
			return item.Name
		},
		GetDescriptionSub: func(item types.Secret) string {
			return item.Description
		},
		UpdateSub: func(data *types.Secret) (identifier string, err error) {
			s, err := connection.Client.UpdateSecret(data.ID, types.SecretCreate{
				CommonFields: types.CommonFields{
					Name:        data.Name,
					Description: data.Description,
					Labels:      data.Labels,
				},
			})
			return s.ID, err
		},
	})
}
