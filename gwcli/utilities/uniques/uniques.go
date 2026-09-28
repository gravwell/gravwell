/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package uniques contains global constants and functions that must be referenced across multiple packages but cannot belong to any.
// Ultimately, we should seek to move away from this package.
package uniques

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/internal/annotations"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/spf13/cobra"
)

const (
	// the string format the Gravwell client requires
	SearchTimeFormat string = "2006-01-02T15:04:05.999999999Z07:00"
)

// AttachPersistentFlags populates all persistent flags and attaches them to the given command.
// This subroutine should ONLY be used by Mother when building the root command or by test suites that omit Mother.
func AttachPersistentFlags(cmd *cobra.Command) {
	ft.NoInteractive.Register(cmd.PersistentFlags())
	// login flags
	cmd.PersistentFlags().StringP("username", "u", "", "login credential. Requires either -p or \""+cfgdir.EnvKeyPassword+"\"."+
		" If your account has MFA enabled, you must use an API token (--api or --eapi) or login interactively.")
	cmd.PersistentFlags().StringP("passfile", "p", "", "the path to a file containing your password")
	ft.API.Register(cmd.PersistentFlags())
	ft.EAPI.Register(cmd.PersistentFlags())
	cmd.MarkFlagsMutuallyExclusive("username", ft.API.Name(), ft.EAPI.Name())

	ft.NoColor.Register(cmd.PersistentFlags())
	cmd.PersistentFlags().String("server", "localhost:80", "<host>:<port> of instance to connect to.\n")
	cmd.PersistentFlags().Bool("insecure", false, "do not use HTTPS and do not enforce certs.")
	cmd.PersistentFlags().String("profile", "", "spins up the native CPU profiler to log samples (in pprof format) into the given path")
	cmd.PersistentFlags().MarkHidden("profile")

	cmd.PersistentFlags().Bool("no-local-permissions", false, "disables local permission checks, allowing all requests to hit the server. "+
		"Permissions will still be enforced on the server-side.")
	cmd.PersistentFlags().String("restlog", cfgdir.DefaultRestLogPath, "log location for raw REST calls made to the server")

	// NOTE: to enable clilog to come online immediately, these flags are never actually handled.
	// Instead, clilog.InitializeFromArgs is used.
	// These definitions are here to act as descriptor text for a user.
	//
	// This is distinction must be made because we cannot parse all flags early as we do not know the full list of acceptable flags until an action has been determined.
	// However, we want the logger to come online early.
	cmd.PersistentFlags().StringP(clilog.FlagLogPath.Name, clilog.FlagLogPath.Shorthand, clilog.FlagLogPath.DefaultValue, clilog.FlagLogPath.Description)
	cmd.PersistentFlags().String(clilog.FlagLogLevel.Name, clilog.FlagLogLevel.DefaultValue, clilog.FlagLogLevel.Description)
}

// Help generates the full help text for a command and prints it on c.Out.
// The specific command's Usage and Example are displayed, if provided, along with all available flags.
//
// This subroutine should only see production use in root.
// However, it is extracted to uniques to facilitate its use in tests.
func Help(c *cobra.Command, _ []string) {
	var sb strings.Builder

	// write the description block
	sb.WriteString(stylesheet.Cur.Field("Synopsis", 0))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(c.Long))
	sb.WriteString("\n\n")

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

	// write requirements lines, if available
	if rqs := annotations.RequirementsStrings(c); len(rqs) > 0 {
		fmt.Fprintf(&sb, "%s\n%s\n\n", stylesheet.Cur.Field("Requirements", 0), strings.Join(rqs, "\n"))
	}

	// write local flags
	if lf := c.LocalNonPersistentFlags().FlagUsages(); lf != "" {
		sb.WriteString(stylesheet.Cur.Field("Flags", 0))
		sb.WriteString("\n")
		sb.WriteString(lf)
	}

	// write global flags (except for the completion command)
	if c.Name() != "completion" && (!c.HasParent() || (c.HasParent() && c.Parent().Name() != "completion")) {
		if gf := c.Root().PersistentFlags().FlagUsages(); gf != "" {
			sb.WriteString("\n")
			sb.WriteString(stylesheet.Cur.Field("Global Flags", 0))
			sb.WriteString("\n")
			sb.WriteString(gf)
		}
	}

	// attach children

	// split children by group
	navs := make([]*cobra.Command, 0)
	actions := make([]*cobra.Command, 0)
	children := c.Commands()
	for _, c := range children {
		if c.Hidden {
			continue
		}
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
			s.WriteString("\n  ")
			s.WriteString(stylesheet.Cur.Nav.Render(n.Name()))
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
			s.WriteString("\n  ")
			s.WriteString(stylesheet.Cur.Action.Render(a.Name()))
		}
		fmt.Fprintf(&sb, "\n%s%s", stylesheet.Cur.FieldText.Render("Actions"), s.String())
	}

	fmt.Fprint(c.OutOrStdout(), sb.String())
}

// DerivePath returns the path from root to the given command.
//
// !includeRoot omits "~" from the path
func DerivePath(cmd *cobra.Command, includeRoot bool) []string {
	if cmd == nil || cmd.Parent() == nil {
		if includeRoot {
			return []string{"~"}
		}
		return []string{}
	}
	pth := []string{cmd.Name()}

	// start from the command and work our way to root
	x := cmd
	for {
		x = x.Parent()
		if x.Parent() == nil { // we are at root
			if includeRoot {
				pth = append(pth, "~")
			}
			break
		}
		pth = append(pth, x.Name())

	}

	// reverse
	slices.Reverse(pth)

	return pth
}

// SliceToSet transforms arr into a hashset of T -> true.
//
// O(arr)
func SliceToSet[T comparable](arr []T) map[T]bool {
	if arr == nil {
		return nil
	}
	m := make(map[T]bool)
	for _, t := range arr {
		m[t] = true
	}
	return m
}
