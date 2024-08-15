/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Help and usage generation subroutines for use by Mother and root (the Cobra tree)
package usage

import (
	"fmt"
	"github.com/gravwell/gravwell/v3/gwcli/clilog"
	"github.com/gravwell/gravwell/v3/gwcli/group"
	"github.com/gravwell/gravwell/v3/gwcli/stylesheet"
	"strings"

	"github.com/spf13/cobra"
)

func Usage(c *cobra.Command) error {

	var bldr strings.Builder
	// pull off first string, recombinate the rest to retrieve a usable path sans root
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
