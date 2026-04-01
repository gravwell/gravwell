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
		},
	)
}

// does not include "all"
var targets = []string{
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
	slices.Sort(targets)
	return scaffold.NewBasicAction(
		"cleanup",
		"purges deleted items from the system",
		"Purges deleted items of the given type, rendered them unable to be restored.\n"+
			"Available targets:\n"+
			"- all\n- "+
			strings.Join(targets, "\n- "),
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

				return out + strings.Join(runCleanup(targets), "\n"), nil
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
