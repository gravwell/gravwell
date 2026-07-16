/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package admin provides actions reserved for admins.
// It should be hidden to non-admin users.
package admin

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize/english"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/internal/listitem"
	"github.com/gravwell/gravwell/v4/gwcli/internal/state"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func NewNav() *cobra.Command {
	const (
		use   string = "admin"
		short string = "actions reserved for use by admin users"
		long  string = "Admin contains actions that require elevated privileges." +
			" These actions span a variety of categories and have some overlap with general-use actions."
	)
	return treeutils.GenerateNav(use, short, long,
		nil,
		[]action.Pair{
			cleanup(),
			logLevel(),
			addIndexer(),
			backup(),
			restore(),
			Status("status"),
			listUserSearchStorage(),
			//validateBackup(),
			massChown(),
			chown(),
		},
		treeutils.NodeOptions{
			CommandAliases: []string{"administrator"},
			Requirements:   annotations.Requirements{UserIsAdmin: true},
		},
	)
}

// does not include "all"
var cleanupTargets = []string{
	"macros",
	"resources",
	"search_history",
	"secrets",
	"templates",
	"tokens",
	"user_preferences",
}

// getTarget returns the cleanup function associated to a given target in the targets list.
// We have to use this over making targets a map because Client will be nil until all actions have been built.
// Therefore, we cannot cache the cleanup functions.
//
// Returns nil if the target is unknown
func getTarget(target string) func() error {
	switch target {
	case "macros":
		return connection.Client.CleanupMacros
	case "resources":
		return connection.Client.CleanupResources
	case "search_history":
		return connection.Client.CleanupSearchHistory
	case "secrets":
		return connection.Client.CleanupSecrets
	case "templates":
		return connection.Client.CleanupTemplates
	case "tokens":
		return connection.Client.CleanupTokens
	case "user_preferences":
		return connection.Client.CleanupUserPreferences
	default:
		return nil
	}
}

// clean up is responsible for calling all specified cleanup functions, thus purging the respective type/resource/asset/entity
func cleanup() action.Pair {
	slices.Sort(cleanupTargets)
	return scaffold.NewBasicAction(
		"cleanup",
		"purges deleted items from the system",
		"Purges deleted items of the given type, rendered them unable to be restored.\n"+
			"Available targets:\n"+
			"- all\n- "+
			strings.Join(cleanupTargets, "\n- "),
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			// compact the list of items to clean so we don't make duplicate m
			var (
				m   = map[string]bool{}
				all = false
			)
			for _, arg := range fs.Args() {
				// sanitize text
				arg = strings.ToLower(strings.TrimSpace(arg))
				m[arg] = true
				if arg == "all" {
					all = true
				}
			}
			if all {
				var out string
				if len(m) > 1 {
					out = "\"all\" specified; other targets are redundant\n"
				}

				return out + strings.Join(runCleanup(cleanupTargets), "\n"), nil
			}

			// validate all cleanups before calling *any*
			requested := slices.Collect(maps.Keys(m))
			slices.Sort(requested)
			invalid := []string{}
			for _, req := range requested {
				if f := getTarget(req); f == nil {
					invalid = append(invalid, req)
				}
			}
			if len(invalid) > 0 {
				return "unknown cleanup targets: " + strings.Join(invalid, ", "), nil
			}

			return strings.Join(runCleanup(requested), "\n"), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Aliases: []string{"clean", "tidy", "purge", "burninate"},
				Usage:   "cleanup " + ft.VariadicArgs("target", true),
				Example: "cleanup macros secrets",
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() < 1 {
					return "you must specify at least one item to clean up or \"all\"", nil
				}
				return "", nil
			},
		})
}

// helper function for cleanup.
// msgs can contain a mix of success and error messages
func runCleanup(targetsToRun []string) (msgs []string) {
	for _, target := range targetsToRun {
		f := getTarget(target)
		if f == nil {
			msgs = append(msgs, target+" is not a valid target")
			continue
		}
		if err := f(); err != nil {
			msgs = append(msgs, "failed to clean up "+target+": "+err.Error())
			continue
		}
		msgs = append(msgs, "successfully purged "+target)
	}
	return
}

// get/set log level
func logLevel() action.Pair {
	return scaffold.NewBasicAction("log-level", "get or set the server log level",
		"Display the current server log level."+
			"Use --set to change it.\n"+
			"Valid levels are typically: OFF, ERROR, WARN, INFO, WEB ACCESS",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			if level, err := fs.GetString("set"); err != nil {
				clilog.GetFlag(err)
			} else if level != "" { // set
				if err := connection.Client.SetLogLevel(level); err != nil {
					return err.Error(), nil
				}
				return "log level set to " + level, nil
			}
			// get
			level, err := connection.Client.GetLogLevel()
			if err != nil {
				return err.Error(), nil
			}
			return "current log level: " + level, nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.String("set", "", "log level to set")
					return fs
				},
			},
		})
}

func addIndexer() action.Pair {
	return scaffold.NewBasicAction("add-indexer", "add an indexer to the system",
		"Tells the webserver to connect to a new indexer. "+
			"The indexer will be added to the list of indexers in the webserver's config file and persist in the future.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			dialstring := fs.Arg(0)
			errors, err := connection.Client.AddIndexer(dialstring)
			if err != nil {
				return err.Error(), nil
			}
			var sb strings.Builder
			for k, v := range errors {
				sb.WriteString(k + ": " + v + "\n")
			}
			out := strings.TrimRight(sb.String(), "\n")
			if out == "" {
				return "indexer added successfully", nil
			}
			return out, nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: fmt.Sprintf("add-indexer %s %s ", ft.Optional("Flags"), ft.Mandatory("host:port")),
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("dial string"), nil
				}
				return "", nil
			},
		})
}

func backup() action.Pair {
	return scaffold.NewBasicAction("backup", "backup the system",
		"Download a backup of the Gravwell system to a file.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			// ! failing to get ANY flag is fatal for this error; we don't want to screw up a user's backup.

			out := fs.Arg(0)
			f, err := os.Create(out)
			if err != nil {
				return err.Error(), nil
			}
			defer f.Close()

			ss, err := fs.GetBool("include-scheduled-searches")
			if err != nil {
				return clilog.GetFlag(err).Error(), nil
			}
			omitSensitive, err := fs.GetBool("omit-sensitive")
			if err != nil {
				return clilog.GetFlag(err).Error(), nil
			}
			pass, err := fs.GetString("encrypt")
			if err != nil {
				return clilog.GetFlag(err).Error(), nil
			}

			cfg := types.BackupConfig{
				IncludeSS:     ss,
				OmitSensitive: omitSensitive,
				Password:      pass,
			}
			var logPass string // "password" to log
			if pass != "" {
				logPass = "*****"
			}
			clilog.Writer.Info("issuing backup command",
				log.KV("IncludeSS", ss),
				log.KV("OmitSensitive", omitSensitive),
				log.KV("encryption", logPass))

			if err := connection.Client.BackupWithConfig(f, cfg); err != nil {
				return err.Error(), nil
			}
			f.Sync()
			return fmt.Sprintf("backup written to %s", out), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: fmt.Sprintf("backup %s %s", ft.Optional("flags"), ft.Mandatory("path/to/backup/file")),
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("include-scheduled-searches", false, "include scheduled searches in the backup")
					fs.Bool("omit-sensitive", false, "omit sensitive items")
					fs.String("encrypt", "", "encrypt the backup with the given password. No encryption will be applied if unset.")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("output path"), nil
				}
				return "", nil
			},
		})
}

func restore() action.Pair {
	return scaffold.NewBasicAction("restore", "restore the system from a backup",
		"Restore the Gravwell system from a backup file.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			path := fs.Arg(0)
			f, err := os.Open(path)
			if err != nil {
				return err.Error(), nil
			}
			defer f.Close()
			if err := connection.Client.Restore(f); err != nil {
				return err.Error(), nil
			}
			return fmt.Sprintf("successfully restored from %s", path), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: fmt.Sprintf("restore %s %s", ft.Optional("flags"), ft.Mandatory("path/to/backup/file")),
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("backup file path"), nil
				}
				if _, err := os.Stat(fs.Arg(0)); err != nil {
					return err.Error(), nil
				}
				return "", nil
			},
		})
}

// Status displays if your account is an administrator and if you are currently in admin mode.
// NOTE: this action is provided to both the `admin` nav (as `status`) and the `self` nav. Hence the export.
func Status(use string) action.Pair {
	return scaffold.NewBasicAction(use, "display your admin status or toggle admin mode", "Displays whether or not you are an admin.\n"+
		"In interactive mode, -t can be used to toggle "+stylesheet.Cur.ErrorText.Render("admin mode")+" which will attach admin=true to future request.\n"+
		"For the most part, this just implies --all in calls that support it, resulting in lists displaying results from all users.\n"+
		"\n"+
		"Exercise caution in admin mode, as it gives access to objects belonging to other users and makes it easy to break things.\n"+
		"Admin mode does not persist between sessions and has no effect if invoked non-interactively.",
		func(fs *pflag.FlagSet) (string, tea.Cmd) {
			isAdministrator, err := connection.Client.IsAdmin()
			if err != nil {
				return "failed to fetch administrator status: " + err.Error(), nil
			}
			var statusSB strings.Builder
			if isAdministrator {
				statusSB.WriteString("You are an administrator.\n")
			} else {
				statusSB.WriteString("You are not an administrator.\n")
			}
			// if we are not spinning up a full, interactive shell, admin mode doesn't matter.
			if !state.Interactive() && !state.DirectInvoked() {
				return statusSB.String(), nil
			}

			{ // branch on toggle flag
				t, err := fs.GetBool("toggle")
				clilog.GetFlag(err)
				if t {
					return toggle(isAdministrator)
				}
			}

			// attach admin mode to the output string
			if inAdminMode := connection.AdminMode(); inAdminMode {
				statusSB.WriteString("You are in admin mode.\n")
				if isAdministrator {
					statusSB.WriteString("Yet, you are somehow in admin mode.\n" +
						"Admin mode will be ineffectual.\n")
				}
			} else {
				statusSB.WriteString("You are not in admin mode.\n")
			}
			return statusSB.String(), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: use + " " + ft.Optional("--toggle"),
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.BoolP("toggle", "t", false, ft.InteractiveOnly()+" Toggle admin mode for this session.")
					return fs
				},
			},
		})
}

func toggle(isAdministrator bool) (string, tea.Cmd) {
	if !isAdministrator {
		return "Only administrators can toggle admin mode", nil
	}
	if !connection.Client.AdminMode() {
		connection.Client.SetAdminMode()
		return "You are now in admin mode", nil
	}
	connection.Client.ClearAdminMode()
	return "You are no longer in admin mode", nil

}

type userSearchStorage struct {
	UID      int32
	Username string // username
	Stored   string // humanized int64
}

func listUserSearchStorage() action.Pair {
	return scaffoldlist.NewListAction("show the storage of each user's searches",
		"Display the cumulative storage space consumed by each user's active searches.\n"+
			"This does not factor in other items related to this user that are stored on the system.",
		userSearchStorage{},
		func(addtlFlags *pflag.FlagSet, params scaffoldlist.DataParameters) ([]userSearchStorage, error) {
			statuses, err := connection.Client.ListAllSearchStatuses()
			if err != nil {
				return nil, err
			} else if len(statuses) < 1 {
				return nil, nil
			}
			storageMap := map[int32]int64{}
			for _, s := range statuses {
				if s.StoredData > 0 {
					storageMap[s.UID] += s.StoredData // starts as bytes
				}
			}
			if len(storageMap) < 1 {
				return nil, nil
			}

			// map usernames to their IDs and humanize storage costs
			userMap, err := connection.Client.GetUserMap()
			if err != nil { // failing to fetch usernames is fine, just make sure the map is initialized
				clilog.Writer.Warn("failed to fetch list of users", log.KVErr(err))
			} else if userMap == nil {
				userMap = map[int32]string{}
			}
			storage := make([]userSearchStorage, len(storageMap))
			// sort by size, descending
			sorted := slices.Collect(maps.Keys(storageMap))
			slices.SortStableFunc(sorted, func(uidA, uidB int32) int {
				if storageMap[uidA] < storageMap[uidB] {
					return 1
				} else if storageMap[uidA] > storageMap[uidB] {
					return -1
				}
				return 0
			})
			for i, uid := range sorted {
				username := "[unknown]"
				if u, found := userMap[uid]; found {
					username = u
				}
				storage[i] = userSearchStorage{
					UID:      uid,
					Username: username,
					Stored:   ingest.HumanSize(uint64(storageMap[uid])),
				}
			}
			return storage, nil
		},
		nil, scaffoldlist.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "search-storage",
			},
			DefaultColumns: []string{"Username", "UID", "Stored"},
			EmptyMessage:   "There are no active searches currently storing data."},
	)
}

/*func validateBackup() action.Pair {
	return scaffold.NewBasicAction("validate-backup", "validate backup files",
		"Test that local gravwell backups are valid.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			var sb strings.Builder
			for _, path := range fs.Args() {
				f, err := os.Open(path)
				if err != nil {
					sb.WriteString(stylesheet.Cur.ErrorText.Render(err.Error()))
					sb.WriteString("\n")
					continue
				}
				// the load does a full validation of the underlying
				ht, err := utils.LoadHashingTar(f, types.CanonicalVersion{}, types.CanonicalVersion{})
				if err != nil {
					fmt.Fprintf(&sb, "backup %s is invalid: %v\n", path, err)
					f.Close()
					continue
				}
				objCount, version, err := ht.Info()
				if err != nil {
					fmt.Fprintf(&sb, "backup %s is invalid: %v\n", path, err)
					f.Close()
					continue
				}
				fmt.Fprintf(&sb, "backup %s validated with %d objects (created by Gravwell version %s)\n",
					path, objCount, version.Build.CanonicalVersion.String())
				f.Close()
			}
			return sb.String(), nil
		},
		scaffold.BasicOptions{
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() < 1 {
					return phrases.AtLeast1ArgRequired("path to backup file"), nil
				}
				return "", nil
			},
		})
}*/

func massChown() action.Pair {
	return scaffold.NewBasicAction("mass-chown", "transfer all items to another user",
		"Mass transfer all items owned by one user to another user.\n"+
			"Please note that tokens and kits do not current support owner reassignment and will be skipped.",
		func(fs *pflag.FlagSet) (output string, addtlCmds tea.Cmd) {
			from, _ := fs.GetInt32("from")
			to, _ := fs.GetInt32("to")
			noFail, _ := fs.GetBool("no-fail")
			// ensure we are in admin mode to ensure we get all data
			if !connection.Client.AdminMode() {
				connection.Client.SetAdminMode()
				defer connection.Client.ClearAdminMode()
			}
			qo := &types.QueryOptions{
				Filters: []types.Filter{
					{Key: "OwnerID", Operation: "=", Values: []any{from}},
				},
			}

			var sb strings.Builder

			// saved queries
			if lr, err := connection.Client.ListAllSavedQueries(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get saved queries: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateSavedQuery(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown saved query %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "saved query")
			}
			// dashboards
			if lr, err := connection.Client.ListAllDashboards(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get dashboards: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateDashboard(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown dashboard %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "dashboard")
			}
			// kits
			sb.WriteString("chowning kits is not currently supported\n")

			// extractions
			if lr, err := connection.Client.ListAllExtractions(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get extractions: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateExtraction(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown extraction %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "extraction")
			}
			// actionables
			if lr, err := connection.Client.ListAllActionables(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get actionables: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateActionable(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown actionable %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "actionable")
			}
			// playbooks
			if lr, err := connection.Client.ListAllPlaybooks(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get playbooks: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdatePlaybook(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown playbook %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "playbook")
			}
			// scheduled searches
			if lr, err := connection.Client.ListAllScheduledSearches(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get scheduled searches: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if err := connection.Client.UpdateScheduledSearch(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown scheduled search %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "scheduled search")
			}
			// scheduled scripts
			if lr, err := connection.Client.ListAllScheduledScripts(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get scheduled scripts: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if err := connection.Client.UpdateScheduledScript(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown scheduled script %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "scheduled script")
			}
			// files
			if lr, err := connection.Client.ListAllFiles(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get files: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateFileMetadata(res.ID, res); err != nil {
						fmt.Fprintf(&sb, "failed to chown file %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "file")
			}
			// templates
			if lr, err := connection.Client.ListAllTemplates(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get templates: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateTemplate(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown template %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "template")
			}
			// resources
			if lr, err := connection.Client.ListAllResources(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get resources: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateResourceMetadata(res.ID, res); err != nil {
						fmt.Fprintf(&sb, "failed to chown resource %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "resource")
			}
			// macros
			if lr, err := connection.Client.ListAllMacros(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get macros: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if err := connection.Client.UpdateMacro(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown macro %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "macro")
			}
			// flows
			if lr, err := connection.Client.ListAllFlows(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get flows: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if err := connection.Client.UpdateFlow(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown flow %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "flow")
			}
			// alerts
			if lr, err := connection.Client.ListAllAlerts(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get alerts: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateAlert(res); err != nil {
						fmt.Fprintf(&sb, "failed to chown alert %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "alert")
			}

			// secrets // TODO test
			if lr, err := connection.Client.ListAllSecrets(qo); err != nil {
				clilog.Tee(clilog.ERROR, &sb, "failed to get secrets: "+err.Error()+"\n")
				if !noFail {
					return sb.String(), nil
				}
			} else {
				var success uint
				for _, res := range lr.Results {
					res.CommonFields.OwnerID = to
					if _, err := connection.Client.UpdateSecret(res.ID, types.SecretCreate{CommonFields: res.CommonFields}); err != nil {
						fmt.Fprintf(&sb, "failed to chown secret %s: %v", res.ID, err)
						if !noFail {
							return sb.String(), nil
						}
					} else {
						success += 1
					}
				}
				chownedString(&sb, success, "secret")
			}
			// tokens
			sb.WriteString(stylesheet.Italicize("NOTE: tokens do not support owner reassignment"))

			return sb.String(), nil
		},
		scaffold.BasicOptions{
			CommonOptions: scaffold.CommonOptions{
				Usage: "mass-chown --from=<UID> --to=<UID>",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Int32("to", 0, "user ID  to transfer ownership to")
					fs.Int32("from", 0, "user ID to transfer ownership from")
					fs.Bool("no-fail", false, "continue on failures, rather than immediately failing out")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				to, err := fs.GetInt32("to")
				clilog.GetFlag(err)
				from, err := fs.GetInt32("from")
				clilog.GetFlag(err)
				if to == 0 && from == 0 {
					return "--to and --from are required", nil
				} else if to == 0 {
					return "--to is required", nil
				} else if from == 0 {
					return "--from is required", nil
				} else if to == from {
					return "refusing to transfer items from and to the same user", nil
				}
				// make sure both from and to exist
				users, err := connection.Client.GetUserMap()
				if err != nil {
					return "", err
				}
				if _, found := users[to]; !found {
					return strconv.FormatInt(int64(to), 10) + " is not a valid uid", nil
				} else if _, found := users[from]; !found {
					return strconv.FormatInt(int64(from), 10) + " is not a valid uid", nil
				}
				return "", nil
			},
		})
}

func chownedString(out *strings.Builder, successes uint, noun string) {
	if successes < 1 {
		return
	}
	out.WriteString("successfully chowned ")
	out.WriteString(english.Plural(int(successes), noun, ""))

	out.WriteString("\n")
}

func chown() action.Pair {
	return scaffoldselect.NewSelectAction("change individual data ownership",
		"Transfer ownership of specific data from one user to another."+
			"Chowning kits and secrets is not currently supported, but all other asset types are.",
		"item",
		func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) {
			from, _ := addtlFlags.GetInt32("from")

			// ensure we are in admin mode to ensure we get all data
			if !connection.Client.AdminMode() {
				connection.Client.SetAdminMode()
				defer connection.Client.ClearAdminMode()
			}
			// fetch ALL items owned by the FROM user
			data := make([]multiselectlist.SelectableItem[string], 0)
			qo := &types.QueryOptions{
				Filters: []types.Filter{
					{Key: "OwnerID", Operation: "=", Values: []any{from}},
				},
			}

			// saved queries
			if lr, err := connection.Client.ListAllSavedQueries(qo); err != nil {
				clilog.Writer.Error("failed to get saved queries", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// dashboards
			if lr, err := connection.Client.ListAllDashboards(qo); err != nil {
				clilog.Writer.Error("failed to get dashboards", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// kits
			// TODO currently being skipped, as they were skipped pre-6.0.0
			// extractions
			if lr, err := connection.Client.ListAllExtractions(qo); err != nil {
				clilog.Writer.Error("failed to get extractions", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// actionables
			if lr, err := connection.Client.ListAllActionables(qo); err != nil {
				clilog.Writer.Error("failed to get actionables", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// playbooks
			if lr, err := connection.Client.ListAllPlaybooks(qo); err != nil {
				clilog.Writer.Error("failed to get playbooks", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// scheduled searches
			if lr, err := connection.Client.ListAllScheduledSearches(qo); err != nil {
				clilog.Writer.Error("failed to get scheduled searches", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// scheduled scripts
			if lr, err := connection.Client.ListAllScheduledScripts(qo); err != nil {
				clilog.Writer.Error("failed to get scheduled scripts", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// files
			if lr, err := connection.Client.ListAllFiles(qo); err != nil {
				clilog.Writer.Error("failed to get files", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// templates
			if lr, err := connection.Client.ListAllTemplates(qo); err != nil {
				clilog.Writer.Error("failed to get templates", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// resources
			if lr, err := connection.Client.ListAllResources(qo); err != nil {
				clilog.Writer.Error("failed to get resources", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// macros
			if lr, err := connection.Client.ListAllMacros(qo); err != nil {
				clilog.Writer.Error("failed to get macros", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// flows
			if lr, err := connection.Client.ListAllFlows(qo); err != nil {
				clilog.Writer.Error("failed to get flows", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			// alerts
			if lr, err := connection.Client.ListAllAlerts(qo); err != nil {
				clilog.Writer.Error("failed to get alerts", log.KVErr(err))
				return nil, err
			} else {
				for _, res := range lr.Results {
					data = append(data, &listitem.Generic{
						ID_:        res.ID,
						Name:       res.Name,
						SecondLine: res.Description,
					})
				}
			}
			return data, nil
		},
		func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) {
			from, _ := addtlFlags.GetInt32("from")
			to, _ := addtlFlags.GetInt32("to")
			noFail, _ := addtlFlags.GetBool("no-fail")

			results = make([]scaffold.Result, len(IDs))
			for i, ID := range IDs {
				err := transferEntity(ID, from, to)
				if err != nil {
					results[i] = scaffold.Result{
						Output: err.Error(),
					}
					if !noFail {
						return results, nil // we already made changes, so we want to make sure they get printed too
					}
				} else {
					results[i] = scaffold.Result{
						Success: true,
						Output:  "chowned " + ID,
					}
				}
			}
			return results, nil
		},
		scaffoldselect.Options{
			CommonOptions: scaffold.CommonOptions{
				Use: "chown",
				Usage: fmt.Sprintf("chown %s %s %s %s",
					ft.Mandatory("--to <UID>"), ft.Mandatory("--from <UID>"),
					ft.Optional("--no-fail"),
					ft.VariadicArgs("entity ID", true)),
				Example: "chown --to=1 --from 5 dashboard-abc-def-ghi scheduled-search-one-two-three",
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Int32("to", 0, "user ID  to transfer ownership to")
					fs.Int32("from", 0, "user ID to transfer ownership from")
					fs.Bool("no-fail", false, "continue on failures, rather than immediately failing out")
					return fs
				},
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				to, err := fs.GetInt32("to")
				clilog.GetFlag(err)
				from, err := fs.GetInt32("from")
				clilog.GetFlag(err)
				if to == 0 && from == 0 {
					return "--to and --from are required", nil
				} else if to == 0 {
					return "--to is required", nil
				} else if from == 0 {
					return "--from is required", nil
				} else if to == from {
					return "refusing to transfer items from and to the same user", nil
				}
				return "", nil
			},
		})
}

func transferEntity(ID string, from, to int32) error {
	// IDs are always prefixed with their type, but that type may span two words.
	exploded := strings.Split(ID, "-")
	if len(exploded) < 2 { // this should never be true
		return errors.New("failed to parse ID " + ID)
	}
	one, two := exploded[0], exploded[1]
	// check against 1 word types
	switch one {
	case "dashboard":
		itm, err := connection.Client.GetDashboard(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "dashboard")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("dashboard (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateDashboard(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "dashboard")
		} else if err != nil {
			return err
		}
		return nil
	case "extraction":
		itm, err := connection.Client.GetExtraction(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "extraction")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("extraction (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateExtraction(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "extraction")
		} else if err != nil {
			return err
		}
		return nil
	case "actionable":
		itm, err := connection.Client.GetActionable(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "actionable")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("actionable (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateActionable(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "actionable")
		} else if err != nil {
			return err
		}
		return nil
	case "playbook":
		itm, err := connection.Client.GetPlaybook(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "playbook")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("playbook (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdatePlaybook(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "playbook")
		} else if err != nil {
			return err
		}
		return nil
	case "template":
		itm, err := connection.Client.GetTemplate(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "template")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("template (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateTemplate(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "template")
		} else if err != nil {
			return err
		}
		return nil
	case "macro":
		itm, err := connection.Client.GetMacro(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "macro")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("macro (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if err := connection.Client.UpdateMacro(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "macro")
		} else if err != nil {
			return err
		}
		return nil
	case "flow":
		itm, err := connection.Client.GetFlow(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "flow")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("flow (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if err := connection.Client.UpdateFlow(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "flow")
		} else if err != nil {
			return err
		}
		return nil
	case "alert":
		itm, err := connection.Client.GetAlert(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "alert")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("alert (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateAlert(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "alert")
		} else if err != nil {
			return err
		}
		return nil
	case "file":
		itm, err := connection.Client.GetFileMetadata(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "file")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("file (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateFileMetadata(ID, itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "file")
		} else if err != nil {
			return err
		}
		return nil
	case "resource":
		itm, err := connection.Client.GetResourceMetadata(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "resource")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("resource (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateResourceMetadata(ID, itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "resource")
		} else if err != nil {
			return err
		}
		return nil
	}
	// check against 2 word types
	switch one + "-" + two {
	case "saved-query":
		itm, err := connection.Client.GetSavedQuery(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "saved query")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("saved query (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if _, err := connection.Client.UpdateSavedQuery(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "saved query")
		} else if err != nil {
			return err
		}
		return nil
	case "scheduled-search":
		itm, err := connection.Client.GetScheduledSearch(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "scheduled search")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("scheduled search (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if err := connection.Client.UpdateScheduledSearch(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "scheduled search")
		} else if err != nil {
			return err
		}
		return nil
	case "scheduled-script":
		itm, err := connection.Client.GetScheduledScript(ID)
		if phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "scheduled script")
		} else if err != nil {
			return err
		} else if from != itm.OwnerID {
			return fmt.Errorf("scheduled script (ID: %s) does not belong to from-user (ID: %d)", ID, from)
		}
		itm.OwnerID = to
		if err := connection.Client.UpdateScheduledScript(itm); phrases.IsNotFoundErr(err) {
			return phrases.ErrUnknownIdentifier(ID, "scheduled script")
		} else if err != nil {
			return err
		}
		return nil
	}

	// if we made it this far, we failed to identify the entity type
	err := fmt.Errorf("failed to map ID %s to an entity type", ID)
	clilog.Writer.Warn(err.Error())
	return err
}
