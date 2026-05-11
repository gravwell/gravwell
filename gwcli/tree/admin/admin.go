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
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/tree/admin/groups"
	"github.com/gravwell/gravwell/v4/gwcli/tree/admin/license"
	admin_users "github.com/gravwell/gravwell/v4/gwcli/tree/admin/users"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
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
	return treeutils.GenerateNav(use, short, long, []string{"administrator"},
		[]*cobra.Command{
			groups.NewNav(),
			admin_users.NewNav(),
			license.NewNav(),
		},
		[]action.Pair{
			cleanup(),
			logLevel(),
			addIndexer(),
			backup(),
			restore(),
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
				Usage:   fmt.Sprintf("cleanup %v %v ...", ft.Mandatory("TARGET1"), ft.Optional("TARGET2")),
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
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("include-scheduled-searches", false, "include scheduled searches in the backup")
					fs.Bool("omit-sensitive", false, "include scheduled searches in the backup")
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
