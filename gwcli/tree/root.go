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

	"github.com/charmbracelet/lipgloss"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/tree/dashboards"
	"github.com/gravwell/gravwell/v4/gwcli/tree/extractors"
	"github.com/gravwell/gravwell/v4/gwcli/tree/ingest"
	"github.com/gravwell/gravwell/v4/gwcli/tree/kits"
	"github.com/gravwell/gravwell/v4/gwcli/tree/macros"
	"github.com/gravwell/gravwell/v4/gwcli/tree/queries"
	"github.com/gravwell/gravwell/v4/gwcli/tree/query"
	"github.com/gravwell/gravwell/v4/gwcli/tree/resources"
	systemshealth "github.com/gravwell/gravwell/v4/gwcli/tree/systems"
	"github.com/gravwell/gravwell/v4/gwcli/tree/tree"
	"github.com/gravwell/gravwell/v4/gwcli/tree/user"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var profilerFile *os.File

// global PersistentPreRunE.
//
// Before any command is executed, ppre checks for NOCOLOR,
// ensures the logger is set up,
// and attempts to log the user into the gravwell instance.
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

	if isNoColor(cmd.Flags()) {
		stylesheet.Cur = stylesheet.Plain()
		stylesheet.NoColor = true
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

// helper function for ppre.
//
// Checks for --no-color, $env.NO_COLOR, and --no-interactive in that order.
func isNoColor(fs *pflag.FlagSet) bool {
	// check --no-color
	if nc, err := fs.GetBool(ft.NoColor.Name()); err != nil {
		panic(err)
	} else if nc {
		clilog.Writer.Debug("disabled_color",
			rfc5424.SDParam{
				Name:  "reason",
				Value: "--" + ft.NoColor.Name(),
			})
		return true
	}
	// check NO_COLOR env var
	if _, found := os.LookupEnv("NO_COLOR"); found { // https://no-color.org/
		clilog.Writer.Debug("disabled_color",
			rfc5424.SDParam{
				Name:  "reason",
				Value: "NO_COLOR",
			})
		return true
	}

	if noInteractive, err := fs.GetBool(ft.NoInteractive.Name()); err != nil {
		panic(err)
	} else if noInteractive {
		clilog.Writer.Debug("disabled_color",
			rfc5424.SDParam{
				Name:  "reason",
				Value: "--" + ft.NoInteractive.Name(),
			})
		return true
	}
	return false

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
		err           error
		noInteractive bool
		username      string
		password      string
		passfilePath  string
		apiKey        string
	)
	if noInteractive, err = cmd.Flags().GetBool(ft.NoInteractive.Name()); err != nil {
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
	if err := connection.Login(username, password, apiKey, noInteractive); err != nil {
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

// Execute adds all child commands to the root command, sets flags appropriately, and launches the
// program according to the given parameters
// (via cobra.Command.Execute()).
func Execute(args []string) int {
	const (
		// usage
		use   string = "gwcli"
		short string = "Gravwell CLI Client"
	)

	// must be variable to allow lipgloss formatting
	var long = "gwcli is a CLI client for interacting with your Gravwell instance directly from your terminal.\n" +
		"It can be used non-interactively in your scripts or interactively via the built-in TUI.\n" +
		"To invoke the TUI, simply call " + stylesheet.Cur.ExampleText.Render("gwcli") + ".\n" +
		"You can view help for any submenu or action by providing help a path.\n" +
		"For instance, try: " + stylesheet.Cur.ExampleText.Render("gwcli help macros create") +
		" or " + stylesheet.Cur.ExampleText.Render("gwcli query -h")

	// spawn the cobra commands in parallel
	var cmdFn = []func() *cobra.Command{
		macros.NewMacrosNav,
		queries.NewQueriesNav,
		kits.NewKitsNav,
		user.NewUserNav,
		extractors.NewExtractorsNav,
		dashboards.NewDashboardNav,
		resources.NewResourcesNav,
		systemshealth.NewSystemsNav,
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
			ingest.NewIngestAction(),
		})
	rootCmd.SilenceUsage = true
	rootCmd.PersistentPreRunE = ppre
	rootCmd.PersistentPostRunE = ppost
	rootCmd.Version = uniques.Version

	// associate flags
	uniques.AttachPersistentFlags(rootCmd)

	if !rootCmd.AllChildCommandsHaveGroup() {
		panic("some children missing a group")
	}

	// configuration the completion command as an action
	rootCmd.SetCompletionCommandGroupID(group.ActionID)

	// configure Windows mouse trap
	cobra.MousetrapHelpText = "This is a command line tool.\n" +
		"You need to open gwcli.exe and run it from there.\n" +
		"Press Return to close.\n"
	cobra.MousetrapDisplayDuration = 0

	// configure root's Run to launch Mother
	rootCmd.Run = treeutils.NavRun

	// if args were given (ex: we are in testing mode)
	// use those instead of os.Args
	if args != nil {
		rootCmd.SetArgs(args)
	}

	// override the help command to just call usage
	rootCmd.SetHelpFunc(help)
	rootCmd.SetUsageFunc(func(c *cobra.Command) error {
		fmt.Fprintf(c.OutOrStdout(), "gwcli %s %s", ft.Optional("flags"), ft.Optional("subcommand path"))
		return nil
	})

	{ // build a set of examples
		fields := "  " + stylesheet.Cur.ExampleText.Render("Invoke an action directly:") +
			"\n  " + stylesheet.Cur.ExampleText.Render("Invoke the interactive prompt:") +
			"\n  " + stylesheet.Cur.ExampleText.Render("Invoke in a script:")
		examples := " gwcli -u USERNAME system indexers list --json" +
			"\n gwcli --server=gravwell.io:4090" +
			"\n" + ` gwcli --api APIKEY query "tag=gravwell stats count | chart count"`
		rootCmd.Example = "\n" + lipgloss.JoinHorizontal(lipgloss.Left, fields, examples)

	}

	err := rootCmd.Execute()
	if err != nil {
		return 1
	}

	return 0
}

// Help generates the full help text for a command and prints it on c.Out.
// The specific command's Usage and Example are displayed, if provided, along with all available flags.
func help(c *cobra.Command, _ []string) {
	var sb strings.Builder

	// write the description block
	sb.WriteString(stylesheet.Cur.Field("Synopsis", 0) + "\n" + lipgloss.NewStyle().PaddingLeft(2).Render(strings.TrimSpace(c.Long)) + "\n\n")

	// write usage line, if available
	// NOTE(rlandau): assumes usage is in the form "<cmd.Name> <following usage>"
	if usage := c.UsageString(); usage != "" {
		fmt.Fprintf(&sb, "%s %s\n\n", stylesheet.Cur.Field("Usage", 0), usage)
	}

	// write aliases line, if available
	if aliases := strings.Join(c.Aliases, ", "); aliases != "" {
		fmt.Fprintf(&sb, "%s %s\n\n", stylesheet.Cur.Field("Aliases", 0), aliases)
	}

	// write example line, if available
	// NOTE(rlandau): assumes example is in the form "<cmd.Name> <following example>"
	if ex := strings.TrimSpace(c.Example); ex != "" {
		fmt.Fprintf(&sb, "%s %s\n\n", stylesheet.Cur.Field("Example", 0), c.Example) // use the untrimmed version
	}

	// write local flags
	if lf := c.LocalNonPersistentFlags().FlagUsages(); lf != "" {
		sb.WriteString(stylesheet.Cur.Field("Flags", 0) + "\n" + lf + "\n")
	}

	// write global flags
	if gf := c.Root().PersistentFlags().FlagUsages(); gf != "" {
		sb.WriteString(stylesheet.Cur.Field("Global Flags", 0) + "\n" + gf)
	}

	// attach children

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
		for _, n := range navs {
			s.WriteString("\n  " + stylesheet.Cur.Nav.Render(n.Name()))
		}
		fmt.Fprintf(&sb, "\n%s%s", stylesheet.Cur.FieldText.Render("Submenus"), s.String())
	}

	// output actions
	if len(actions) > 0 {
		if len(navs) > 0 {
			sb.WriteString("\n")
		}
		var s strings.Builder
		for _, a := range actions {
			s.WriteString("\n  " + stylesheet.Cur.Action.Render(a.Name()))
		}
		fmt.Fprintf(&sb, "\n%s%s", stylesheet.Cur.FieldText.Render("Actions"), s.String())
	}

	fmt.Fprint(c.OutOrStdout(), sb.String())
}
