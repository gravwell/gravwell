/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffold

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type BasicOptions struct {
	// AddtlFlagFunc provides a function that defines additional flags for this specific action.
	// These flags will be added to the standard flagset of a basic action.
	AddtlFlagFunc func() pflag.FlagSet
	// Other names for this action.
	Aliases []string
	// Free-form function called in SetArgs or at the start of run to validate the given flags.
	// Called after the cmd's .Args() function (if !nil and !err).
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)

	//#region command functions

	// Override the default usage line printed as part of `help`/`-h`.
	Usage string

	// Provide an example of using this action. Should start with the action's name.
	Example string

	//#endregion command functions

}

// CommonOptions are options that span all scaffolds and thus should be applied the same for each.
// Would be a pretty crap UX if scaffoldList's Usage override applied differently than scaffoldCreate's.
type CommonOptions struct {
	// Override the scaffold's default use/handle.
	// Ex: call the action something other than "list", despite using scaffoldlist.
	//
	// Use should be a single word using only ASCII characters and may be coerced for usability.
	Use string
	// Override the default usage line printed in this command's help text.
	// Usage should follow the format: "<use> <mandatory flags> [optional flags] [parameters...].
	Usage string
	// Provide an example of calling this action/override the scaffold's default example.
	// Example should start with Use.
	Example string
	// Other names for this action.
	Aliases []string

	// A function to generate action-specific flags that should be bolted on.
	// CommonOptions.Apply will attach these flags to the command, but remember to utilize them in interactive mode (likely during SetArgs).
	AddtlFlags func() *pflag.FlagSet
}

// Apply alters the given cmd such that all set CommonOptions are effectual.
func (co CommonOptions) Apply(cmd *cobra.Command) {
	if co.Use = strings.TrimSpace(co.Use); co.Use != "" {
		co.Use = strings.ReplaceAll(co.Use, " ", "_")
		cmd.Use = co.Use
	}

	if co.Usage != "" {
		cmd.SetUsageFunc(func(c *cobra.Command) error {
			_, err := fmt.Fprint(c.OutOrStdout(), co.Usage)
			return err
		})
	}
	if co.Example != "" {
		cmd.Example = co.Example
	}
	if len(co.Aliases) > 0 {
		cmd.Aliases = co.Aliases
	}
	if co.AddtlFlags != nil {
		cmd.Flags().AddFlagSet(co.AddtlFlags())
	}
}
