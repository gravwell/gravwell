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
	//
	// Use should be a single word using only ASCII characters and may be coerced for usability.
	Use string
	// Override scaffold's default one-line action description.
	Short string
	// Override scaffold's default action description.
	Long string
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
	if co.Short = strings.TrimSpace(co.Short); co.Short != "" {
		cmd.Short = co.Short
	}
	if co.Long = strings.TrimSpace(co.Long); co.Long != "" {
		cmd.Long = co.Long
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
	FlagNameAllData  string = "all" // fetch data from all users instead of just the current user
	FlagUsageAllData string = "Requests that results include data from all users and groups instead of just yours.\n" +
		"Ignored if you are not an admin.\n" +
		"Implied by admin mode"
	FlagNameLimit string = "limit" // limit the number of elements returned
)

type QOBuilder interface {
	// Install flags into the given set based on what options should be available to the user for this action.
	Install(fs *pflag.FlagSet)
	// QueryOptions composes a QO from the flagset
	QueryOptions(fs *pflag.FlagSet) *types.QueryOptions
}

var _ QOBuilder = QOOmit{}
var _ QOBuilder = QOInclude{}

type QOInclude struct {
	Everything     bool
	AllData        bool
	IncludeDeleted bool
	Limit          bool
}

func (o QOInclude) Install(fs *pflag.FlagSet) {
	if o.Everything || o.AllData {
		fs.Bool(FlagNameAllData, false, FlagUsageAllData)
	}
	if o.Everything || o.IncludeDeleted {
		ft.IncludeDeleted.Register(fs)
	}
	if o.Everything || o.Limit {
		fs.Int(FlagNameLimit, 0, "Limit the number of items to return")
	}
}

func (o QOInclude) QueryOptions(fs *pflag.FlagSet) *types.QueryOptions {
	var err error
	var qo = &types.QueryOptions{}

	if o.Everything || o.IncludeDeleted {
		qo.IncludeDeleted, err = fs.GetBool(ft.IncludeDeleted.Name())
		clilog.GetFlag(err)
	}
	if o.Everything || o.AllData {
		qo.AdminMode = connection.AdminMode()
		if !qo.AdminMode { // check for --all override
			qo.AdminMode, err = fs.GetBool(FlagNameAllData)
			clilog.GetFlag(err)
		}
	}
	if o.Everything || o.Limit {
		lim, err := fs.GetInt(FlagNameLimit)
		clilog.GetFlag(err)
		if lim > 0 {
			qo.Limit = lim
		}
	}

	return qo
}

// QOOmit is a blacklist; itenables QueryOptions by default, requiring each be turned off individually.
// Anything set to true in here will have its equivalent flag turned off for this action.
// For example: if an asset type doesn't tombstone, --include-deleted should probably be disabled.
type QOOmit struct {
	Everything bool // disable everything. This is useful if the ListDataFunc doesn't take query opts.

	AllData        bool
	IncludeDeleted bool
	Limit          bool
}

func (o QOOmit) Install(fs *pflag.FlagSet) {
	if o.Everything {
		return
	}

	if !o.AllData {
		fs.Bool(FlagNameAllData, false, FlagUsageAllData)
	}
	if !o.IncludeDeleted {
		ft.IncludeDeleted.Register(fs)
	}
	if !o.Limit {
		fs.Int(FlagNameLimit, 0, "Limit the number of items to return")
	}
}

func (o QOOmit) QueryOptions(fs *pflag.FlagSet) *types.QueryOptions {
	var err error
	var qo = &types.QueryOptions{}
	if o.Everything {
		return qo
	}

	if !o.IncludeDeleted {
		qo.IncludeDeleted, err = fs.GetBool(ft.IncludeDeleted.Name())
		clilog.GetFlag(err)
	}
	if !o.AllData {
		qo.AdminMode = connection.AdminMode()
		if !qo.AdminMode { // check for --all override
			qo.AdminMode, err = fs.GetBool(FlagNameAllData)
			clilog.GetFlag(err)
		}
	}
	if !o.Limit {
		lim, err := fs.GetInt(FlagNameLimit)
		clilog.GetFlag(err)
		if lim > 0 {
			qo.Limit = lim
		}
	}

	return qo
}
