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
	"strings"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	pflagtypes "github.com/gravwell/gravwell/v4/gwcli/utilities/pflag_types"
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
	const (
		use   string = "kits"
		short string = "view kits associated to this instance"
		long  string = "Kits bundle up of related items (dashboards, queries, scheduled searches," +
			" autoextractors) for easy installation."
	)
	var aliases = []string{"kit"}
	return treeutils.GenerateNav(use, short, long, aliases,
		[]*cobra.Command{},
		[]action.Pair{
			listAction(),
			uninstall(),
			//install(),
			upload(),
			pull(),
			remote(),
			download(),
		})
}

func listAction() action.Pair {
	var uuidsFlag *pflag.Flag
	return scaffoldlist.NewListAction(
		"list installed and staged kits", "Lists system kits visible to you or, if you are an admin, available on this system.",
		types.IdKitState{}, func(fs *pflag.FlagSet, param scaffoldlist.DataParameters) ([]types.IdKitState, error) {
			var err error
			var kits []types.IdKitState
			if param.QueryOpts.AdminMode {
				kits, err = connection.Client.AdminListKits()
			} else {
				kits, err = connection.Client.ListKits()
			}
			if err != nil {
				return nil, err
			}
			if implodedFilters := strings.Trim(uuidsFlag.Value.String(), "[]"); implodedFilters != "" {
				var filters = make(map[uuid.UUID]any, 0)
				for raw := range strings.SplitSeq(implodedFilters, ",") {
					filter, err := uuid.Parse(raw)
					clilog.Writer.Warn("failed to parse uuid from UUIDSliceValue.String(). This should have been caught by UUIDSliceValue.Set().",
						log.KV("raw", raw),
						log.KVErr(err))
					filters[filter] = 0
				}
				// one more sanity check: only filter if we actually, you know, got filters.
				if len(filters) > 0 {
					var filtered []types.IdKitState
					// filter down kits
					for _, kit := range kits {
						if _, found := filters[kit.UUID]; found {
							filtered = append(filtered, kit)
						}
					}
					kits = filtered
				}
			}
			return kits, nil
		},
		nil,
		scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"get"},
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					uuidsFlag = fs.VarPF(pflagtypes.NewUUIDSliceValue(nil, ','), "uuids", "", "Fetch a specific set of kits by UUID")
					return fs
				},
			},
			DefaultColumns: []string{
				"UUID",
				"KitState.Name",
				"KitState.Description",
				"KitState.Version",
			},
			Omit: scaffold.OmitFlags{IncludeDeleted: true, Limit: true},
		})
}

func uninstall() action.Pair {
	return scaffolddelete.NewDeleteAction("kit", "kits",
		func(dryrun bool, id string) error {
			if dryrun {
				uid, err := uuid.Parse(id)
				if err != nil {
					return err
				}
				_, err = connection.Client.KitInfo(uid)
				return err
			}
			return connection.Client.DeleteKit(id)
		},
		func() ([]multiselectlist.SelectableItem[string], error) {
			pkgs, err := connection.Client.ListKits()
			if err != nil {
				return nil, err
			}
			var items = make([]multiselectlist.SelectableItem[string], len(pkgs))
			for i, kit := range pkgs {
				items[i] = &listitem.Generic{
					Selected_:  false,
					ID_:        kit.ID,
					Name:       kit.Name,
					SecondLine: kit.Description,
				}
			}

			return items, nil
		}, scaffolddelete.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "uninstall",
				Aliases: []string{"delete", "remove"},
			},
		})
}

// TODO
/*func install() action.Pair {
	return scaffoldselect.NewSelectAction("install a staged kit",
		"Install a kit that has been uploaded/staged, queuing it for full installed.",
		"kit",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[uuid.UUID], error) {
			ks, err := connection.Client.KitStatuses()
			if err != nil {
				return nil, err
			}
			items := []multiselectlist.SelectableItem[uuid.UUID]{}
			for _, kit := range ks {
				if kit.CurrentStep
			}
		},
		func(IDs []ID_t, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			for i, ID := range IDs {

			}
		},
		scaffoldselect.Options{},
	)
}*/

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
			CommonOptions: scaffold.CommonOptions{Use: "upload"},
			Short:         "upload a kit file",
			Long:          "Upload a kit file and stage it for installation.",
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
					ID_:        m.UUID,
					Name:       m.Name,
					SecondLine: m.Description,
				}
			}
			return items, nil
		},
		func(UUIDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			results = make([]scaffold.Result, len(UUIDs))
			for i, id := range UUIDs {
				parsed, err := uuid.Parse(id)
				if err != nil {
					err = fmt.Errorf("failed to parse remote kit UUID: %w", err)
					clilog.Writer.Error(err.Error())
					results[i] = scaffold.Result{Output: err.Error()}
					continue
				}
				ks, err := connection.Client.PullKit(parsed) // TODO GUID is depreciated. Using UUID until we have final confirmation
				if err != nil {
					results[i] = scaffold.Result{Output: err.Error()}
					continue
				}
				results[i] = scaffold.Result{
					Success: true,
					Output:  fmt.Sprintf("staged kit %s (ID: %s / UUID: %s)", ks.Name, ks.ID, ks.UUID),
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{Use: "pull"},
		})
}

func remote() action.Pair {
	return scaffoldlist.NewListAction("list remote kits", "List kits available in the configured remote repository.",
		types.KitMetadata{},
		func(fs *pflag.FlagSet, params scaffoldlist.DataParameters) ([]types.KitMetadata, error) {
			return connection.Client.ListRemoteKits(params.QueryOpts.AdminMode)
		},
		nil,
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{Use: "remote"},
			DefaultColumns: []string{"UUID", "Name", "Description", "Version"},
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
			local, err := connection.Client.ListKits()
			if err != nil {
				clilog.Writer.Warn("failed to list local kits", log.KVErr(err))
			}
			remote, err := connection.Client.ListRemoteKits(false)
			if err != nil {
				clilog.Writer.Warn("failed to list remote kits", log.KVErr(err))
			}
			if len(local) > 1 && len(remote) > 1 {
				return nil, errors.New("both local and remote kits failed to return any results")
			}
			items := make([]multiselectlist.SelectableItem[string], len(local)+len(remote))
			for i, k := range local {
				items[i] = &listitem.Generic{
					ID_:        k.ID,
					Name:       k.Name,
					SecondLine: "(local) " + k.Description,
				}
			}
			for i, k := range remote {
				items[i+len(local)] = &listitem.Generic{
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

// buildKit assembles a kit from a JSON build-request file and returns the resulting kit UUID.
// The JSON must match the types.KitBuildRequest structure.
// See https://docs.gravwell.io/api/kits.html for the spec.
/*func buildKit() action.Pair {
	return scaffold.NewBasicAction("build", "build a kit from a JSON spec file",
		"Assemble a new kit from a JSON file that describes its contents.\n"+
			"The JSON must conform to the KitBuildRequest schema.\n\n"+
			"On success the new kit UUID is printed; the kit will then appear in the staged list\n"+
			"and can be downloaded with the 'kits download' action.\n\n"+
			"See https://docs.gravwell.io/api/kits.html for the expected JSON structure.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			path := fs.Arg(0)
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Sprintf("failed to read file '%s': %v", path, err), nil
			}
			var req types.KitBuildRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				return fmt.Sprintf("failed to parse build-request JSON: %v", err), nil
			}
			resp, err := connection.Client.BuildKit(req)
			if err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("built kit '%s' (UUID: %s, size: %d bytes)", req.Name, resp.UUID, resp.Size), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: fmt.Sprintf("build %s", ft.Mandatory("build-spec.json")),
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("path to build-spec JSON file"), nil
				}
				if _, err := os.Stat(fs.Arg(0)); err != nil {
					return fmt.Sprintf("cannot access file '%s': %v", fs.Arg(0), err), nil
				}
				return "", nil
			},
		})
}
*/
