// Package files provides utilities for working with userfiles.
package files

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldedit"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "files"
		short string = "manage extra files you have uploaded"
		long  string = "Files can be used to store small files for use in playbooks, cover images for kits, etc.\n" +
			"See https://docs.gravwell.io/gui/files/files.html for more information."
	)
	return treeutils.GenerateNav(use, short, long, []string{"uf", "userfiles", "userfile"}, nil,
		[]action.Pair{
			list(),
			download(),
			create(),
			edit(),
		})
}

func list() action.Pair {
	const (
		short string = "list userfiles on the system"
		long  string = "Lists information about the userfiles you have access to."
	)
	return scaffoldlist.NewListAction(short, long,
		types.UserFileDetails{}, func(fs *pflag.FlagSet) ([]types.UserFileDetails, error) {
			// check for all
			all, err := fs.GetBool(ft.GetAll.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.GetAll.Name(), err)
			}

			if all {
				return connection.Client.AllUserFiles()
			}
			return connection.Client.UserFiles()
		},
		scaffoldlist.Options{
			AddtlFlags: func() pflag.FlagSet {
				var fs = pflag.FlagSet{}
				ft.GetAll.Register(&fs, true, "files")
				return fs
			},
			// TODO update column names once userfiles get the registry treatment
			DefaultColumns: []string{"Name", "Type", "Labels", "Size"},
			ColumnAliases:  map[string]string{"Size": "SizeBytes"},
		})
}

func download() action.Pair {
	return scaffold.NewBasicAction("download", "download a file", "Download a file for use locally.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			// arg length checked by the options
			id := fs.Arg(0)

			// TODO remove me after registry updates
			u, err := uuid.Parse(id)
			if err != nil {
				return err.Error(), nil
			}

			outPath, err := fs.GetString(ft.Output.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			}
			clilog.Writer.Info("downloading file", rfc5424.SDParam{Name: "file_UUID", Value: u.String()})
			data, err := connection.Client.GetUserFile(u)
			if err != nil {
				return err.Error(), nil
			}
			if outPath != "" { // spit to standard out
				out, err := os.Create(outPath)
				if err != nil {
					return err.Error(), nil
				}
				defer out.Close()
				n, err := out.WriteString(string(data))
				if err != nil {
					return err.Error(), nil
				}
				return stylesheet.StringWriteToFileSuccess(n, outPath), nil
			}
			// spit to terminal
			return fmt.Sprintf("%s", data), nil
		}, scaffold.BasicOptions{
			AddtlFlagFunc: func() pflag.FlagSet {
				var fs pflag.FlagSet
				ft.Output.Register(&fs)
				return fs
			},
		})
}

func create() action.Pair {
	return scaffoldcreate.NewCreateAction("file",
		map[string]scaffoldcreate.Field{
			"name":   scaffoldcreate.FieldName("file"),
			"desc":   scaffoldcreate.FieldDescription("file"),
			"path":   scaffoldcreate.FieldPath("file"),
			"labels": scaffoldcreate.FieldLabels(),
		},
		func(cfg scaffoldcreate.Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var (
				name, desc, path string
				labels           []string
			)
			// fetch and sanity check values
			var found bool
			if name, found = fieldValues["name"]; !found {
				return uuid.UUID{}, "", errors.New("failed to find \"name\" field")
			}
			if desc, found = fieldValues["desc"]; !found {
				return uuid.UUID{}, "", errors.New("failed to find \"desc\" field")
			}
			if path, found = fieldValues["path"]; !found {
				return uuid.UUID{}, "", errors.New("failed to find \"path\" field")
			}
			if lbls, found := fieldValues["labels"]; !found {
				return uuid.UUID{}, "", errors.New("failed to find \"labels\" field")
			} else {
				labels = strings.Split(lbls, ",")
			}

			var m = types.UserFileDetails{
				Name:   name,
				Desc:   desc,
				Labels: labels,
			}

			id, err = connection.Client.AddUserFileDetails(m, path)
			return
		}, nil)
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("file", "files",
		scaffoldedit.Config{
			"name":   scaffoldedit.FieldName("file"),
			"desc":   scaffoldedit.FieldDescription("file"),
			"labels": scaffoldedit.FieldLabels(),
		},
		scaffoldedit.SubroutineSet[uuid.UUID, types.UserFileDetails]{
			SelectSub: func(id uuid.UUID) (item types.UserFileDetails, err error) {
				return connection.Client.GetUserFileDetails(id)
			},
			FetchSub: func() (items []types.UserFileDetails, err error) {
				return connection.Client.UserFiles()
			},
			GetFieldSub: func(item types.UserFileDetails, fieldKey string) (value string, err error) {
				switch fieldKey {
				case "name":
					return item.Name, nil
				case "desc":
					return item.Desc, nil
				case "labels":
					return strings.Join(item.Labels, ","), nil
				}
				return "", fmt.Errorf("unknown field key: %v", fieldKey)
			},
			SetFieldSub: func(item *types.UserFileDetails, fieldKey, val string) (invalid string, err error) {
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
					item.Desc = val
				case "labels":
					item.Labels = strings.Split(val, ",")
				default:
					return "", fmt.Errorf("unknown field key: %v", fieldKey)
				}
				return
			},
			GetTitleSub: func(item types.UserFileDetails) string {
				return item.Name
			},
			GetDescriptionSub: func(item types.UserFileDetails) string {
				return item.Desc
			},
			UpdateSub: func(data *types.UserFileDetails) (identifier string, err error) {
				err = connection.Client.UpdateUserFileMetadata(data.ThingUUID, types.UserFileDetails{
					Name:   data.Name,
					Desc:   data.Desc,
					Labels: data.Labels,
				})
				return data.ThingUUID.String(), err
			},
		},
	)
}
