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
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/tree/admin/groups"
	"github.com/gravwell/gravwell/v4/gwcli/tree/admin/users"
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
			users.NewNav(),
		},
		[]action.Pair{},
	)
}

// does not include "all"
var targets = map[string]func() error{}

// clean up is responsible for calling all specified cleanup functions, thus purging the respective type/resource/asset/entity
func cleanup() action.Pair {
	return scaffold.NewBasicAction(
		"cleanup",
		"purges deleted items from the system",
		"Purges deleted items of the given type, rendered them unable to be restored.\n"+
			"Available commands: ", // TODO
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
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
			if len(m) > 1 && all {
				fmt.Fprint(cmd.ErrOrStderr(), "\"all\" specified; other targets are redundant") // TODO do we need to differentiate between returning a string and just spitting to out?
				// run all and ignore everything else
				// TODO
				return "", nil
			}
			// validate all cleanups before calling *any*
			requested := slices.Collect(maps.Keys(m))
			invalid := []string{}
			for _, req := range requested {
				if _, found := targets[req]; !found {
					invalid = append(invalid, req)
				}
			}
			if len(invalid) > 0 {
				return "unknown cleanup targets: " + strings.Join(invalid, ", "), nil
			}
			for _, req := range requested {
				err := targets[req]()
				// TODO handle error
			}
		},
		scaffold.BasicOptions{
			Aliases: []string{"clean", "tidy", "purge"},
			CmdMods: func(c *cobra.Command) {
				c.SetUsageFunc(func(c *cobra.Command) error {
					fmt.Fprintf(c.OutOrStdout(), "cleanup %v", ft.Mandatory("TARGET1"), ft.Mandatory("TARGET2"), ft.Mandatory("..."))
					return nil
				})
				c.Example = "cleanup macros secrets"
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() < 1 {
					return "you must specify at least one item to clean up or \"all\"", nil
				}
			},
		})
}
