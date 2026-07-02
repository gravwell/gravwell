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

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type BasicOptions struct {
	CommonOptions

	// Free-form function called in SetArgs or at the start of run to validate the given flags.
	// Called after the cmd's .Args() function (if !nil and !err).
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
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

//#region OmitFlags

const (
	FlagNameAllData string = "all"   // fetch data from all users instead of just the current user
	FlagNameLimit   string = "limit" // limit the number of elements returned
)

// OmitFlags allows disabling flags for a specific action.
// Anything set to true in here will have its equivalent flag turned off for this action.
// For example: if an asset type doesn't tombstone, --include-deleted should probably be disabled.
type OmitFlags struct {
	Everything bool // disable everything. This is useful if the ListDataFunc doesn't take query opts.

	AllData        bool
	IncludeDeleted bool
	Limit          bool
}

// InstallQueryOptionsFlags attaches query option flags to the flagset if there were not omitted.
//
// Should be paired with GetQueryOptions.
func InstallQueryOptionsFlags(fs *pflag.FlagSet, omit OmitFlags) {
	if omit.Everything {
		return
	}
	// attach query option flags, depending on their omit state

	if !omit.AllData {
		fs.Bool(FlagNameAllData, false, "Requests that results include data from "+stylesheet.Italicize("all")+" users and groups instead of just yours.\n"+
			"Ignored if you are not an admin.\n"+
			"Implied by admin mode")
	}
	if !omit.IncludeDeleted {
		ft.IncludeDeleted.Register(fs)
	}
	if !omit.Limit {
		fs.Int(FlagNameLimit, 0, "Limit the number of items to return")
	}
}

// GetQueryOptions extracts QueryOptions from the given flagset.
//
// Should be paired with InstallQueryOptionsFlags().
func GetQueryOptions(fs *pflag.FlagSet, omit OmitFlags) *types.QueryOptions {
	var err error
	var qo = &types.QueryOptions{}
	if omit.Everything {
		return qo
	}

	if !omit.IncludeDeleted {
		qo.IncludeDeleted, err = fs.GetBool(ft.IncludeDeleted.Name())
		clilog.GetFlag(err)
	}
	if !omit.AllData {
		qo.AdminMode = connection.AdminMode()
		if !qo.AdminMode { // check for --all override
			qo.AdminMode, err = fs.GetBool(FlagNameAllData)
			clilog.GetFlag(err)
		}
	}
	if !omit.Limit {
		lim, err := fs.GetInt(FlagNameLimit)
		clilog.GetFlag(err)
		if lim > 0 {
			qo.Limit = lim
		}
	}

	return qo
}
