/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package kits provides actions for interacting with kits. *jazz hands*
package kits

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"

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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest/log"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	var aliases = []string{"kit"}
	return treeutils.GenerateNav("kits", "view kits associated to this instance",
		"Kits bundle up of related items (dashboards, queries, scheduled searches, autoextractors) for easy installation.",
		aliases,
		[]*cobra.Command{},
		[]action.Pair{
			listAction(),
			uninstall(),
			install(),
			upload(),
			pull(),
			remote(),
			download(),
		})
}

func listAction() action.Pair {
	return scaffoldlist.NewListAction(
		"list installed and staged kits", "Lists system kits visible to you or, if you are an admin, available on this system.",
		types.KitState{}, func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.KitState, error) {
			kits, err := connection.Client.ListKits(param.QueryOpts)
			if err != nil {
				return nil, err
			}
			return kits.Results, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"get"},
			},
			DefaultColumns: []string{
				"CommonFields.ID",
				"KitID",
				"CommonFields.Name",
				"CommonFields.Description",
				"KitVersion",
				"Installed",
			},
			Omit: scaffold.OmitFlags{IncludeDeleted: true},
		})
}

func uninstall() action.Pair {
	return scaffolddelete.NewDeleteAction("kit",
		func(dryrun bool, ID string) error {
			if dryrun {
				_, err := connection.Client.GetKit(ID)
				return err
			}
			return connection.Client.DeleteKit(ID)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			pkgs, err := connection.Client.ListKits(nil)
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(pkgs.Results))
			for i, kit := range pkgs.Results {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        kit.ID,
					Name:       kit.Name,
					SecondLine: fmt.Sprintf("(%s) %s", kit.KitID, kit.Description),
				}
			}
			return items, nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "uninstall",
				Aliases: []string{"delete", "remove"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("force", false, "Delete the kit even if it has modified items.")
					return fs
				},
			},
		})
}

func install() action.Pair {
	return scaffoldselect.NewSelectAction("install a staged kit",
		"Install a kit that has been uploaded/staged, queuing it for full installation.",
		"kit",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			ks, err := connection.Client.ListKits(&types.QueryOptions{AdminMode: connection.AdminMode()})
			if err != nil {
				return nil, err
			}
			items := []multiselectlist.SelectableItem[string]{}
			for _, kit := range ks.Results {
				if kit.Installed {
					continue
				}

				items = append(items, &listitem.Generic{
					ID_:        kit.ID,
					Name:       kit.Name,
					SecondLine: fmt.Sprintf("(Version: %v) %s", kit.Version, kit.Description),
				})
			}
			return items, nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			overwriteExisting, err := addtlFlags.GetBool("overwrite-existing")
			clilog.GetFlag(err)
			allowUnsigned, err := addtlFlags.GetBool("allow-unsigned")
			clilog.GetFlag(err)
			itemLabels, err := addtlFlags.GetStringArray("item-label")
			clilog.GetFlag(err)
			kitLabels, err := addtlFlags.GetStringArray("kit-label")
			clilog.GetFlag(err)

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				_, err := connection.Client.InstallKit(ID,
					types.KitConfig{
						OverwriteExisting: overwriteExisting,
						AllowUnsigned:     allowUnsigned,
						Labels:            itemLabels,
						KitLabels:         kitLabels,
					},
				)
				if err != nil {
					results[i] = scaffold.Result{Output: "failed to install kit " + ID + ": " + err.Error()}
					continue
				}
				results[i] = scaffold.Result{
					Success: true,
					Output:  "installed kit " + ID,
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("overwrite-existing", false, "Overwrite existing assets")
					fs.Bool("allow-unsigned", false, "Allow installation of unsigned kits")
					fs.StringArray("item-label", nil, "Label to apply to each item of each kit. "+
						"Each instance of --item-labels will create exactly one item label; they will not be split on commas")
					fs.StringArray("kit-label", nil, "Label to apply to each kit. "+
						"Each instance of --kit-labels will create exactly one kit label; they will not be split on commas")
					return fs
				},
			},
		},
	)
}

func upload() action.Pair {
	return scaffoldcreate.NewCreateAction("kit",
		map[string]scaffoldcreate.Field{
			"path": scaffoldcreate.FieldPath("kit", true),
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			ks, err := connection.Client.UploadKit(fields["path"].Provider.Get())
			if err != nil {
				return 0, "", nil
			}
			return ks.Name + " (ID: " + ks.ID + ")", "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:   "upload",
				Short: "upload a kit file",
				Long:  "Upload a kit file and stage it for installation.",
			},
		},
	)
}

func pull() action.Pair {
	return scaffoldselect.NewSelectAction(
		"pull a kit from a remote repository",
		"Pull a remote kit and stage it for installation in the local system.",
		"kit UUID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			meta, err := connection.Client.ListRemoteKits(false) // TODO incorporate QueryOptions.All
			if err != nil {
				return nil, err
			}
			items := []multiselectlist.SelectableItem[string]{}
			for i, m := range meta {
				items[i] = &listitem.Generic{
					ID_:        m.ID,
					Name:       m.Name,
					SecondLine: m.Description,
				}
			}
			return items, nil
		},
		func(UUIDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(UUIDs))
			for i, ID := range UUIDs {
				ks, err := connection.Client.PullKit(ID)
				if err != nil {
					results[i] = scaffold.Result{Output: err.Error()}
					continue
				}
				results[i] = scaffold.Result{
					Success: true,
					Output:  fmt.Sprintf("staged kit %s (ID: %s / KitID: %s)", ks.Name, ks.ID, ks.KitID),
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "pull"},
		})
}

// NOTE(rlandau): we don't have a great way to pass the QueryOptions into the SetArgs hooks in fields as fields has no way to access the current FlagSet.
// Until we rework scaffoldcreate+scaffoldedit, we are just going to assume admin mode is set.
func build() action.Pair {
	return scaffoldcreate.NewCreateAction("kit",
		map[string]scaffoldcreate.Field{
			"kit ID": {
				Title:    "KitID",
				Required: true,
				Flag:     scaffoldcreate.FlagConfig{Name: "kit-id", Usage: "ID to use for the kit"},
				Order:    600,
				Provider: &scaffoldcreate.TextProvider{},
			},
			"readme": {
				Title:    "README",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "readme", Usage: "Longform description of the new kit"},
				Order:    580,
				Provider: &scaffoldcreate.TextAreaProvider{},
			},
			"kit version": {
				Title:        "Kit Version",
				Required:     false,
				Flag:         scaffoldcreate.FlagConfig{Name: "kit-version", Usage: "Initial version for the new kit. Defaults to 1."},
				DefaultValue: "1",
				Order:        560,
				Provider:     nil, // TODO
			},
			"dashboards": {
				Title:    "Dashboards",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "dashboards", Usage: "Comma-separated list of dashboard IDs to include in the kit."},
				Order:    540,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListDashboards(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch dashboards", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapDashboards(lr.Results)
					},
				},
				},
			},
			"templates": {
				Title:    "Templates",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "templates", Usage: "Comma-separated list of template IDs to include in the kit."},
				Order:    520,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListTemplates(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch templates", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapTemplates(lr.Results)
					},
				},
				},
			},
			"actionables": {
				Title:    "Actionables",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "actionables", Usage: "Comma-separated list of actionable IDs to include in the kit."},
				Order:    500,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListActionables(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch actionables", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapActionables(lr.Results)
					},
				},
				},
			},
			"flows": {
				Title:    "Flows",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "flows", Usage: "Comma-separated list of flows IDs to include in the kit."},
				Order:    480,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListFlows(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch flows", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapFlows(lr.Results)
					},
				},
				},
			},
			"scheduled searches": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    460,
				Provider: nil, // TODO
			},
			"resources": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    440,
				Provider: nil, // TODO
			},
			"macros": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    420,
				Provider: nil, // TODO
			},
			"search libraries": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    400,
				Provider: nil, // TODO
			},
			"extractions": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    380,
				Provider: nil, // TODO
			},
			"files": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    360,
				Provider: nil, // TODO
			},
			"playbooks": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    340,
				Provider: nil, // TODO
			},
			"alerts": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    320,
				Provider: nil, // TODO
			},
			"embedded items": {
				Title:    "KitVersion",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "", Usage: ""}, // TODO
				Order:    300,
				Provider: nil, // TODO
			},
			"icon": {
				Title:    "Icon",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "kit-icon", Usage: "New kit's icon"},
				Order:    280,
				Provider: &scaffoldcreate.PathProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {

			kbr := types.KitBuildRequest{
				KitID:      fields["kitID"].Provider.Get(),
				Readme:     fields["readme"].Provider.Get(),
				KitVersion: 1,
			}
			resp, err := connection.Client.BuildKit(kbr)
			if err != nil {
				return 0, "", err
			}
			return resp.UID, "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "build",
				Example: "build --kit-id=com.mykit.", // TODO
				Aliases: []string{"pack", "create", "new"},
			},
		},
	)
}

// Rebuild a kit from a previous build request, incrementing the version.
// TODO rebuild. We can probably leverage build instead of a wholly new action.

func remote() action.Pair {
	return scaffoldlist.NewListAction("list remote kits", "List kits available in the configured remote repository.",
		types.KitMetadata{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.KitMetadata, error) {
			return connection.Client.ListRemoteKits(params.QueryOpts.AdminMode)
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "remotes",
				Aliases: []string{"list-remotes", "remote", "list-remote"},
			},
			DefaultColumns: []string{"ID", "KitID", "Name", "Description", "Version"},
			Omit: scaffold.OmitFlags{
				AllData:        false,
				IncludeDeleted: true,
				Limit:          true,
			},
		})
}

func download() action.Pair {
	return scaffoldselect.NewSelectAction(
		"download a kit to a local directory",
		"Download a kit, remote or on the connected Gravwell system, into a local directory",
		"kit ID",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			local, err := connection.Client.ListKits(nil)
			if err != nil {
				clilog.Writer.Warn("failed to list local kits", log.KVErr(err))
			}
			remote, err := connection.Client.ListRemoteKits(false)
			if err != nil {
				clilog.Writer.Warn("failed to list remote kits", log.KVErr(err))
			}
			if len(local.Results) > 1 && len(remote) > 1 {
				return nil, errors.New("both local and remote kits failed to return any results")
			}
			items := make([]multiselectlist.SelectableItem[string], len(local.Results)+len(remote))
			for i, k := range local.Results {
				items[i] = &listitem.Generic{
					ID_:        k.ID,
					Name:       k.Name,
					SecondLine: "(local) " + k.Description,
				}
			}
			for i, k := range remote {
				items[i+len(local.Results)] = &listitem.Generic{
					ID_:        k.ID,
					Name:       k.Name,
					SecondLine: "(remote) " + k.Description,
				}
			}
			return items, nil
		},
		func(UUIDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			noClobber, err := addtlFlags.GetBool("no-clobber")
			clilog.GetFlag(err)
			// root ourselves
			var root *os.Root
			dir, err := addtlFlags.GetString(ft.DirName)
			clilog.GetFlag(err)
			if err := os.MkdirAll(dir, fs.ModeDir); err != nil {
				return nil, err
			} else if root, err = os.OpenRoot(dir); err != nil {
				return nil, err
			}
			results = make([]scaffold.Result, len(UUIDs))
			for i, UUID := range UUIDs {
				resp, err := connection.Client.KitDownloadRequest(UUID)
				if err != nil {
					if phrases.IsNotFoundErr(err) {
						results[i].Output = phrases.ErrUnknownIdentifier(UUID, "kit ID").Error()
					} else {
						results[i].Output = err.Error()
					}
					continue
				} else if resp == nil { // this should never pop, but just to be safe...
					clilog.Writer.Error("Something is horribly broken: KitDownloadRequest returned a nil error and a nil response!",
						log.KV("kit ID", UUID))
					return nil, clilog.ErrInternal{}
				}
				fileName := UUID + ".kit"
				filePath := path.Join(dir, fileName)
				// if the file exists and noClobber was specified return an error for this ID
				if _, err := root.Stat(fileName); !errors.Is(err, fs.ErrNotExist) && noClobber {
					results[i].Output = filePath + " already exists and --no-clobber was specified"
					continue
				}
				f, err := root.Create(fileName)
				if err != nil {
					results[i].Output = ""
					continue
				}
				copied, err := io.Copy(f, resp.Body)
				if err != nil {
					clilog.Writer.Warn("failed to copy to kit data to file", log.KVErr(err))
					results[i].Output = err.Error()
				} else {
					results[i] = scaffold.Result{
						Output:  fmt.Sprintf("downloaded kit %s to %s (%d bytes written)", UUID, filePath, copied),
						Success: true,
					}
				}
				f.Close()
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "download",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String(ft.DirName, ".", ft.DirUsagePrefix+"place downloaded kits. Creates the directory if necessary.")
					fs.Bool("no-clobber", false, "do not truncate files with matching names. Instead, return an error.")
					return fs
				}},
		})
}
