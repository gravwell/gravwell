// Package files provides utilities for working with (user)files.
package files

import (
	"errors"
	"fmt"
	"os"
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
	return treeutils.GenerateNav(use, short, long, []string{"file"}, nil,
		[]action.Pair{
			list(),
			download(),
			create(),
			edit(),
		})
}

func list() action.Pair {
	const (
		short string = "list files on the system"
		long  string = "Lists information about the files you have access to."
	)
	return scaffoldlist.NewListAction(short, long,
		types.File{}, func(fs *pflag.FlagSet) ([]types.File, error) {
			// check for all
			all, err := fs.GetBool(ft.GetAll.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.GetAll.Name(), err)
			}

			var flr types.FileListResponse
			if all {
				flr, err = connection.Client.ListAllFiles(nil)
				if err != nil {
					return nil, err
				}
			} else {
				flr, err = connection.Client.ListFiles(nil)
				if err != nil {
					return nil, err
				}
			}
			return flr.Results, nil
		},
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					var fs = &pflag.FlagSet{}
					ft.GetAll.Register(fs, true, "files")
					return fs
				},
			},
			// TODO update column names once files get the registry treatment
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

			outPath, err := fs.GetString(ft.Output.Name())
			if err != nil {
				clilog.LogFlagFailedGet(ft.Output.Name(), err)
			}
			clilog.Writer.Info("downloading file", rfc5424.SDParam{Name: "file_id", Value: id})
			b, err := connection.Client.GetFile(id)
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
				n, err := out.Write(b)
				if err != nil {
					return err.Error(), nil
				}
				return phrases.SuccessfullyWroteToFile(n, outPath), nil
			}
			return string(b), nil
		}, scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					var fs = &pflag.FlagSet{}
					ft.Output.Register(fs)
					return fs
				},
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
		func(cfg scaffoldcreate.Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var (
				name, desc, filePath string
				labels               []string
			)
			name = cfg["name"].Provider.Get()
			desc = cfg["desc"].Provider.Get()
			filePath = cfg["path"].Provider.Get()
			if lbls := cfg["labels"].Provider.Get(); lbls != "" {
				labels = strings.Split(lbls, ",")
			}

			// TODO make file content/path non-mandatory

			// get a reader on the file
			f, err := os.Open(filePath)
			if err != nil {
				return 0, "", err
			}
			defer f.Close()

			var inMeta = types.File{
				CommonFields: types.CommonFields{
					Name:        name,
					Description: desc,
					Labels:      labels,
				},
			}

			outMeta, err := connection.Client.CreateFile(inMeta)
			if err != nil {
				return 0, "", fmt.Errorf("failed to create empty file: %w", err)
			}
			// populate the file
			if _, err := connection.Client.PopulateFileFromReader(outMeta.ID, f); err != nil {
				return 0, "", fmt.Errorf("failed to populate file: %w", err)
			}

			return outMeta.ID, "", nil
		}, scaffoldcreate.Options{})
}

func edit() action.Pair {
	return scaffoldedit.NewEditAction("file", "files",
		scaffoldedit.Config{
			"name":   scaffoldedit.FieldName("file"),
			"desc":   scaffoldedit.FieldDescription("file"),
			"labels": scaffoldedit.FieldLabels(),
		},
		scaffoldedit.SubroutineSet[string, types.File]{
			SelectSub: func(id string) (item types.File, err error) {
				return connection.Client.GetFileMetadata(id)
			},
			FetchSub: func() (items []types.File, err error) {
				flr, err := connection.Client.ListFiles(nil)
				if err != nil {
					return nil, err
				}
				return flr.Results, nil
			},
			GetFieldSub: func(item types.File, fieldKey string) (value string, err error) {
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
			SetFieldSub: func(item *types.File, fieldKey, val string) (invalid string, err error) {
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
			GetTitleSub: func(item types.File) string {
				return item.Name
			},
			GetDescriptionSub: func(item types.File) string {
				return item.Description
			},
			UpdateSub: func(data *types.File) (identifier string, err error) {
				if data == nil {
					return "", errors.New("cannot update nil item")
				}
				_, err = connection.Client.UpdateFileMetadata(data.ID, *data)
				return data.ID, err
			},
		},
	)
}
