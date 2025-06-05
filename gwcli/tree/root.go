/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package tree supplies the root node of the command tree and the true "main" function.
Initializes itself and `Executes()`, triggering Cobra to assemble itself.
All invocations of the program operate via root, whether or not it hands off control to Mother.
All singletons are instantiated here or via the cobra pre-run.
*/
package tree

import (
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/tree/dashboards"
	"github.com/gravwell/gravwell/v4/gwcli/tree/extractors"
	"github.com/gravwell/gravwell/v4/gwcli/tree/kits"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query"
	"github.com/gravwell/gravwell/v4/gwcli/tree/resources"
	"github.com/gravwell/gravwell/v4/gwcli/tree/status"
	"github.com/gravwell/gravwell/v4/gwcli/tree/tree"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/spf13/cobra"
)

var profilerFile *os.File

// global PersistentPreRunE.
//
// Ensures the logger is set up and the user has logged into the gravwell instance,
// completing these actions if either is false.
func ppre(cmd *cobra.Command, args []string) error {
	// set up the logger, if it is not already initialized
	if clilog.Writer == nil {
		path, err := cmd.Flags().GetString("log")
		if err != nil {
			return err
		}
		lvl, err := cmd.Flags().GetString("loglevel")
		if err != nil {
			return err
		}
		clilog.Init(path, lvl)
	}

	// if this is a 'complete' request, do not enforce login
	if cmd.Name() == cobra.ShellCompRequestCmd || cmd.Name() == cobra.ShellCompNoDescRequestCmd {
		return nil
	}

	// if this is a 'help' action, do not enforce login
	if cmd.Name() == "help" {
		return nil
	}

	// if a profiler was specified, spin one up targeting the given path
	if fn, err := cmd.Flags().GetString("profile"); err != nil {
		panic(err)
	} else if fn = strings.TrimSpace(fn); fn != "" {
		profilerFile, err = os.Create(fn)
		if err != nil {
			clilog.Writer.Warnf("Failed to create file for profiler: %v", err)
			profilerFile = nil
		} else {
			if err := pprof.StartCPUProfile(profilerFile); err != nil {
				clilog.Writer.Infof("failed to enable cpu profiler: %v", err)
			} else {
				clilog.Writer.Infof("started cpu profiler on %v", profilerFile.Name())
			}
		}
	}

	return EnforceLogin(cmd, args)
}

// EnforceLogin initializes the connection singleton, which logs the client into the Gravwell instance dictated by the --server flag.
// Safe (ineffectual) to call if already logged in.
func EnforceLogin(cmd *cobra.Command, args []string) error {
	if connection.Client == nil || connection.Client.State() == client.STATE_CLOSED { // if we just started, initialize connection
		server, err := cmd.Flags().GetString("server")
		if err != nil {
			return err
		}
		insecure, err := cmd.Flags().GetBool("insecure")
		if err != nil {
			return err
		}
		if err = connection.Initialize(server, !insecure, insecure, ""); err != nil {
			return err
		}
	}

	// generate credentials
	var (
		err          error
		script       bool
		username     string
		password     string
		passfilePath string
		apiKey       string
	)
	if script, err = cmd.Flags().GetBool("script"); err != nil {
		return err
	}
	if username, err = cmd.Flags().GetString("username"); err != nil {
		return err
	}
	if password, err = cmd.Flags().GetString("password"); err != nil {
		return err
	}
	if passfilePath, err = cmd.Flags().GetString("passfile"); err != nil {
		return err
	}
	if apiKey, err = cmd.Flags().GetString("api"); err != nil {
		return err
	}

	// password/passfile/apikey are marked mutually exclusive, so we do not have to check here

	// need to check that, if password/passfile are supplied, username is also supplied
	if (passfilePath != "" || password != "") && username == "" {
		return errors.New("if password or passkey are specified, you must also specify username (-u)")
	}

	// if a passfile was specified, skim it out of the file
	if p, err := skimPassFile(passfilePath); err != nil {
		clilog.Writer.Warnf("failed to skim passfile: %v", err)
	} else if p != "" {
		password = p
	}

	// pass all information to Login to decide how to proceed
	if err := connection.Login(username, password, apiKey, script); err != nil {
		return err
	}

	clilog.Writer.Infof("Logged in successfully")

	return nil

}

// skimPassFile slurps the file at the given path if path != "".
// Returns the password found, an error opening/slurping the file, or "" (if path is empty).
func skimPassFile(path string) (password string, err error) {
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read password from %v: %v", path, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return "", nil

}

// global PersistentPostRunE.
// Ensure the client connection to the Gravwell backend is dead.
func ppost(cmd *cobra.Command, args []string) error {

	if err := connection.End(); err != nil {
		clilog.Writer.Debugf("failed to destroy connection singleton: %v", err)
	}

	pprof.StopCPUProfile() // idempotent if no profiler is running
	// if a profiler was enabled, make sure we flush it
	if profilerFile != nil {
		profilerFile.Sync()
		profilerFile.Close()
	}

	return nil
}

// GenerateFlags populates all root-relevant flags (ergo global and root-local flags)
func GenerateFlags(root *cobra.Command) {
	// global flags
	root.PersistentFlags().Bool("script", false,
		"disallows gwcli from entering interactive mode and prints context help instead.\n"+
			"Recommended for use in scripts to avoid hanging on a malformed command.")
	root.PersistentFlags().StringP("username", "u", "", "login credential.")
	root.PersistentFlags().String("password", "", "login credential.")
	root.PersistentFlags().StringP("passfile", "p", "", "the path to a file containing your password")
	root.PersistentFlags().String("api", "", "log in via API key instead of credentials")

	root.MarkFlagsMutuallyExclusive("password", "passfile", "api")
	root.MarkFlagsMutuallyExclusive("api", "username")

	root.PersistentFlags().Bool("no-color", false, "disables colourized output.")
	root.PersistentFlags().String("server", "localhost:80", "<host>:<port> of instance to connect to.\n")
	root.PersistentFlags().StringP("log", "l", cfgdir.DefaultStdLogPath, "log location for developer logs.\n")
	root.PersistentFlags().String("loglevel", "DEBUG", "log level for developer logs (-l).\n"+
		"Possible values: 'OFF', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL', 'FATAL'.\n")
	root.PersistentFlags().Bool("insecure", false, "do not use HTTPS and do not enforce certs.")
	root.PersistentFlags().String("profile", "", "spins up the native CPU profiler to log samples (in pprof format) into the given path")
	root.PersistentFlags().MarkHidden("profile")
}

const ( // usage
	use   string = "gwcli"
	short string = "Gravwell CLI Client"
)

// must be variable to allow lipgloss formatting
var long string = "gwcli is a CLI client for interacting with your Gravwell instance directly" +
	"from your terminal.\n" +
	"It can be used non-interactively in your scripts or interactively via the built-in TUI.\n" +
	"To invoke the TUI, simply call " + stylesheet.ExampleStyle.Render("gwcli") + ".\n" +
	"You can view help for any submenu or action by providing help a path.\n" +
	"For instance, try: " + stylesheet.ExampleStyle.Render("gwcli help macros create") +
	" or " + stylesheet.ExampleStyle.Render("gwcli query -h")

const ( // mousetrap
	mousetrapText string = "This is a command line tool.\n" +
		"You need to open gwcli.exe and run it from there.\n" +
		"Press Return to close.\n"
	mousetrapDuration time.Duration = (0 * time.Second)
)

// Execute adds all child commands to the root command, sets flags appropriately, and launches the
// program according to the given parameters
// (via cobra.Command.Execute()).
func Execute(args []string) int {
	// spawn the cobra commands in parallel
	var cmdFn = []func() *cobra.Command{
		macros.NewMacrosNav,
		queries.NewQueriesNav,
		kits.NewKitsNav,
		user.NewUserNav,
		extractors.NewExtractorsNav,
		dashboards.NewDashboardNav,
		resources.NewResourcesNav,
		status.NewStatusNav,
	}

	var (
		cmds  []*cobra.Command
		resCh = make(chan *cobra.Command)
	)
	for _, fn := range cmdFn {
		go func(f func() *cobra.Command) {
			// execute the builder and send the command pointer to the dispatcher
			resCh <- f()
		}(fn)
	}
	for range cmdFn { // wait for an equal number of results
		cmds = append(cmds, <-resCh)
	}

	rootCmd := treeutils.GenerateNav(use, short, long, []string{},
		cmds,
		[]action.Pair{
			query.NewQueryAction(),
			tree.NewTreeAction(),
		})
	rootCmd.SilenceUsage = true
	rootCmd.PersistentPreRunE = ppre
	rootCmd.PersistentPostRunE = ppost
	rootCmd.Version = "alpha 1"

	// associate flags
	GenerateFlags(rootCmd)

	if !rootCmd.AllChildCommandsHaveGroup() {
		panic("some children missing a group")
	}

	// configuration the completion command as an action
	rootCmd.SetCompletionCommandGroupID(group.ActionID)

	// configure Windows mouse trap
	cobra.MousetrapHelpText = mousetrapText
	cobra.MousetrapDisplayDuration = mousetrapDuration

	// configure root's Run to launch Mother
	rootCmd.Run = treeutils.NavRun

	// if args were given (ex: we are in testing mode)
	// use those instead of os.Args
	if args != nil {
		rootCmd.SetArgs(args)
	}

	rootCmd.SetUsageFunc(Usage)

	err := rootCmd.Execute()
	if err != nil {
		return 1
	}

	return 0
}

// Usage provides a replacement for cobra's usage command, dynamically building the usage based on pwd (/ the full path the user gave).
func Usage(c *cobra.Command) error {
	var bldr strings.Builder
	// pull off first string, recombine the rest to retrieve a usable path sans root
	root, path := func() (string, string) {
		// could do all of this in a one-liner in the fmt.Sprintf, but this is clearer
		p := strings.Split(c.CommandPath(), " ")
		if len(p) < 1 { // should be impossible
			clilog.Writer.Critical("exploded command path is zero-length")
			return "UNKNOWN", "UNKNOWN"
		}
		return p[0], strings.Join(p[1:], " ")
	}()

	bldr.WriteString(stylesheet.Header1Style.Render("Usage:") +
		strings.TrimRight(fmt.Sprintf(" %v %s",
			root, path,
		), " "))

	if c.GroupID == group.NavID { // nav
		bldr.WriteString(" [subcommand]\n")
	} else { // action
		bldr.WriteString(" [flags]\n\n")
		bldr.WriteString(stylesheet.Header1Style.Render("Local Flags:") + "\n")
		bldr.WriteString(c.LocalNonPersistentFlags().FlagUsages())
	}

	bldr.WriteRune('\n')

	if c.HasExample() {
		bldr.WriteString(stylesheet.Header1Style.Render("Example:") + " " + c.Example + "\n\n")
	}

	bldr.WriteString(stylesheet.Header1Style.Render("Global Flags:") + "\n")
	bldr.WriteString(c.Root().PersistentFlags().FlagUsages())

	bldr.WriteRune('\n')

	// print aliases
	if len(c.Aliases) != 0 {
		var s strings.Builder
		s.WriteString(stylesheet.Header1Style.Render("Aliases:") + " ")
		for _, a := range c.Aliases {
			s.WriteString(a + ", ")
		}
		bldr.WriteString(strings.TrimRight(s.String(), ", ") + "\n") // chomp
	}

	// split children by group
	navs := make([]*cobra.Command, 0)
	actions := make([]*cobra.Command, 0)
	children := c.Commands()
	for _, c := range children {
		if c.GroupID == group.NavID {
			navs = append(navs, c)
		} else {
			actions = append(actions, c)
		}
	}

	// output navs as submenus
	if len(navs) > 0 {
		var s strings.Builder
		s.WriteString(stylesheet.Header1Style.Render("Submenus"))
		for _, n := range navs {
			s.WriteString("\n  " + stylesheet.NavStyle.Render(n.Name()))
		}
		bldr.WriteString(s.String() + "\n")
	}

	// output actions
	if len(actions) > 0 {
		var s strings.Builder
		s.WriteString("\n" + stylesheet.Header1Style.Render("Actions"))
		for _, a := range actions {
			s.WriteString("\n  " + stylesheet.ActionStyle.Render(a.Name()))
		}
		bldr.WriteString(s.String())
	}

	fmt.Fprintln(c.OutOrStdout(), strings.TrimSpace(bldr.String()))
	return nil
}
