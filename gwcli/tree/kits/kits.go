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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
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
			"name": {
				Title:    "Name",
				Required: true,
				Flag:     scaffoldcreate.FlagConfig{Name: "name", Usage: "Name to use for the kit"},
				Order:    650,
				Provider: &scaffoldcreate.TextProvider{},
			},
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
				Flag:     scaffoldcreate.FlagConfig{Name: "flows", Usage: "Comma-separated list of flow IDs to include in the kit."},
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
				Title:    "Scheduled Searches",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "scheduled-searches", Usage: "Comma-separated list of scheduled search IDs to include in the kit."},
				Order:    460,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListScheduledSearches(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch scheduled searches", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapScheduledSearches(lr.Results)
					},
				},
				},
			},
			"resources": {
				Title:    "Resources",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "resources", Usage: "Comma-separated list of resource IDs to include in the kit."},
				Order:    440,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListResources(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch scheduled searches", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapResources(lr.Results)
					},
				},
				},
			},
			"macros": {
				Title:    "Macros",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "macros", Usage: "Comma-separated list of macro IDs to include in the kit."},
				Order:    420,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListMacros(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch macros", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapMacros(lr.Results)
					},
				},
				},
			},
			"ax": {
				Title:    "AXs",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "ax", Usage: "Comma-separated list of extractor IDs to include in the kit."},
				Order:    400,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListExtractions(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch extractors", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapAXs(lr.Results)
					},
				},
				},
			},
			"files": {
				Title:    "Files",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "files", Usage: "Comma-separated list of file IDs to include in the kit."},
				Order:    380,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListFiles(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch files", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapFiles(lr.Results)
					},
				},
				},
			},
			"playbooks": {
				Title:    "Playbooks",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "playbooks", Usage: "Comma-separated list of playbook IDs to include in the kit."},
				Order:    360,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListPlaybooks(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch playbooks", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapPlaybooks(lr.Results)
					},
				},
				},
			},
			"saved queries": {
				Title:    "Saved Queries",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "saved-queries", Usage: "Comma-separated list of saved query IDs to include in the kit."},
				Order:    340,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListSavedQueries(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch saved queries", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapSavedQueries(lr.Results)
					},
				},
				},
			},
			"alerts": {
				Title:    "Alerts",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "alerts", Usage: "Comma-separated list of alert IDs to include in the kit."},
				Order:    320,
				Provider: &scaffoldcreate.MSLProvider{Options: scaffoldcreate.MSLOptions{
					SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
						lr, err := connection.Client.ListAlerts(&types.QueryOptions{AdminMode: true})
						if err != nil {
							clilog.Writer.Warn("failed to fetch alert", scaffold.IdentifyCaller(), log.KVErr(err))
							return nil
						}
						return listitem.WrapAlerts(lr.Results)
					},
				},
				},
			},
			"embedded items": {
				Title:    "Embedded Items",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "embedded-items", Usage: "Path to directory of auxiliary items to embed in the kit."},
				Order:    300,
				Provider: &scaffoldcreate.PathProvider{
					Options: pathtextinput.Options{
						CustomTI: func() textinput.Model {
							ti := stylesheet.NewTI("", true)
							ti.Placeholder = "path/to/directory"
							return ti
						},
					},
				},
			},
			"recursive embed": {
				Title:    "Recur into Embedded Dir?",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "recursive-embed", Usage: "Should the --embedded-items directory recur into subdirectories?"},
				Order:    299,
				Provider: &scaffoldcreate.BoolProvider{},
			},
			"icon": {
				Title:    "Icon",
				Required: false,
				Flag:     scaffoldcreate.FlagConfig{Name: "kit-icon", Usage: "New kit's icon"},
				Order:    280,
				Provider: &scaffoldcreate.PathProvider{},
			},
			"local copy": {
				Title:    "Download Local Copy",
				Required: false,
				Flag: scaffoldcreate.FlagConfig{Name: "download", Usage: "Local path to download the new kit to after creation. " +
					"If given a directory, a new file will be created inside of it. " +
					"If given a path to a file that already exists, that file will be truncated unless --no-clobber is set"},
				Order:    260,
				Provider: &scaffoldcreate.PathProvider{},
			},
		},
		func(fields map[string]scaffoldcreate.Field, afs *pflag.FlagSet) (id any, invalid string, err error) {
			kitVersion, err := strconv.ParseInt(fields["kit version"].Provider.Get(), 10, 32)
			if err != nil {
				return 0, fmt.Sprint("failed to get kit version: ", err), nil
			}

			// upload icon as a file
			var iconID string
			if iconPath := strings.TrimSpace(fields["icon"].Provider.Get()); iconPath != "" {
				resp, err := connection.Client.CreateFile(types.File{
					CommonFields: types.CommonFields{
						Name:        "Kit Icon (" + fields["name"].Provider.Get() + ")",
						Description: "Icon for locally built kit " + fields["name"].Provider.Get() + " (KitID: " + fields["kitID"].Provider.Get() + ")",
					},
				})
				if err != nil {
					clilog.Writer.Error("failed to create icon as file", log.KVErr(err))
					return 0, "", err
				}
				resp, err = connection.Client.PopulateFileFromPath(resp.ID, iconPath)
				if err != nil {
					clilog.Writer.Error("failed to populate icon file", log.KVErr(err), log.KV("path", iconPath))
					return 0, "", err
				}
				clilog.Writer.Info("created icon as file", log.KV("ID", resp.ID), log.KV("path", iconPath))
				iconID = resp.ID
			}

			// collected items to embed
			var embed []types.KitEmbeddedItem
			if embedDir := strings.TrimSpace(fields["embedded items"].Provider.Get()); embedDir != "" {
				recur, err := strconv.ParseBool(fields["recursive embed"].Provider.Get())
				if err != nil {
					clilog.Writer.Error("failed to parse bool provider", log.KVErr(err))
					return 0, "", clilog.ErrInternal{}
				}
				dir, err := os.ReadDir(embedDir)
				if err != nil {
					clilog.Writer.Warn("failed to read embed directory", log.KVErr(err))
					return 0, err.Error(), nil
				}
				for _, entry := range dir {
					if !entry.IsDir() {
						embed = append(embed, types.KitEmbeddedItem{KitItem: types.KitItem{
							Name: filepath.Base(s),
							Type: types.KitAssetFile,
						}})
					}
				}
			}

			kbr := types.KitBuildRequest{
				CommonFields: types.CommonFields{
					Name: fields["name"].Provider.Get(),
				},
				KitID:             fields["kitID"].Provider.Get(),
				Readme:            fields["readme"].Provider.Get(),
				KitVersion:        int(kitVersion),
				Dashboards:        strings.Split(strings.TrimSpace(fields["dashboards"].Provider.Get()), ","),
				Templates:         strings.Split(strings.TrimSpace(fields["templates"].Provider.Get()), ","),
				Actionables:       strings.Split(strings.TrimSpace(fields["actionables"].Provider.Get()), ","),
				Flows:             strings.Split(strings.TrimSpace(fields["flows"].Provider.Get()), ","),
				ScheduledSearches: strings.Split(strings.TrimSpace(fields["scheduled searches"].Provider.Get()), ","),
				Resources:         strings.Split(strings.TrimSpace(fields["resources"].Provider.Get()), ","),
				Macros:            strings.Split(strings.TrimSpace(fields["macros"].Provider.Get()), ","),
				Extractors:        strings.Split(strings.TrimSpace(fields["ax"].Provider.Get()), ","),
				Files:             strings.Split(strings.TrimSpace(fields["files"].Provider.Get()), ","),
				Playbooks:         strings.Split(strings.TrimSpace(fields["playbook"].Provider.Get()), ","),
				SavedQueries:      strings.Split(strings.TrimSpace(fields["saved queries"].Provider.Get()), ","),
				Alerts:            strings.Split(strings.TrimSpace(fields["alerts"].Provider.Get()), ","),
				EmbeddedItems:     nil, // TODO
				Icon:              iconID,
			}

			if err := kbr.Validate(); err != nil {
				return 0, err.Error(), nil
			}

			resp, err := connection.Client.BuildKit(kbr)
			if err != nil {
				return 0, "", err
			}

			if dlPath := strings.TrimSpace(fields["local copy"].Provider.Get()); dlPath != "" {
				clilog.Writer.Debug("downloading local copy of new kit", log.KV("path", dlPath))
				pth := dlPath
				if fi, err := os.Stat(dlPath); err != nil {
					return 0, "", fmt.Errorf("failed to download local copy: %w", err)
				} else if errors.Is(err, fs.ErrNotExist) {
					// DNE, do nothing
				} else if fi.IsDir() { // create path under directory
					pth = filepath.Join(dlPath, kbr.Name+".kit")
				} else { // path already exists, points to a pre-existing file
					noClobber, err := afs.GetBool("no-clobber")
					clilog.GetFlag(err)
					if noClobber {
						return 0, dlPath + " already exists and --no-cobber was specified", nil
					}
				}

				f, err := os.Create(pth)
				if err != nil {
					clilog.Writer.Warn("failed to download local copy",
						log.KVErr(err),
						log.KV("stage", "create file"),
						log.KV("path", pth))
					return 0, err.Error(), nil
				}
				defer f.Close()

				// if a download path was specified, retrieve a local copy
				dlresp, err := connection.Client.KitDownloadRequest(resp.UUID)
				if err != nil {
					clilog.Writer.Warn("failed to download local copy",
						log.KVErr(err),
						log.KV("stage", "issue dl request"),
						log.KV("kit ID", resp.UUID))
					return 0, "", fmt.Errorf("failed to download local copy: %w", err)
				}
				defer dlresp.Body.Close()
				if _, err := io.Copy(f, dlresp.Body); err != nil {
					clilog.Writer.Warn("failed to download local copy",
						log.KVErr(err),
						log.KV("stage", "write to file"),
						log.KV("path", pth),
						log.KV("kit ID", resp.UUID))
					return 0, "", err
				}
				return fmt.Sprintf("created new kit %s (ID: %v/KitID: %v/Version: %v) and downloaded local copy to %s",
					kbr.Name, resp.UUID, kbr.KitID, kbr.KitVersion, pth), "", nil
			}

			return fmt.Sprintf("created new kit %s (ID: %v/KitID: %v/Version: %v)", kbr.Name, resp.UUID, kbr.KitID, kbr.KitVersion), "", nil
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "build",
				Example: "build --kit-id=com.me.mykit --name=mykit " +
					"--dashboards=dashboard-one-two-three-four,dashboard-yi-er-san-si " +
					"--icon=/home/me/icon.png " +
					"--download=/home/me/Downloads/",
				Aliases: []string{"pack", "create", "new"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("no-clobber", false, "do not truncate files with matching names. Instead, return an error.")
					return fs
				},
			},
			IDIsSuccessMessage: true,
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
