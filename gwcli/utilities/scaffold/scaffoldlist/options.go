/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"regexp"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
)

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	scaffold.CommonOptions

	// Pretty defines a free-form, pretty-printing function, allowing this action to be displayed in a user-friendly
	// (albeit likely script-unfriendly) way.
	// If !nil, --pretty will also be defined and set as the default.
	//
	// Pretty functions may or may not respect columns.
	Pretty PrettyPrinterFunc
	// Sets the default columns to display if --columns is not specified.
	// Column names must be dot-qualified exact matches, not aliases.
	// Column names must include the "CommonFields." prefix, if applicable.
	//
	// Order is respected.
	//
	// Mutually exclusive with ExcludeColumnsFromDefault.
	DefaultColumns []string
	// A list of regex patterns that OMIT matching dot-qualified columns from the set of defaults.
	// Unlike DefaultColumns, DefaultColumnsFromExcludeRegex regex matches each value against each column;
	// if a column matches any value, that column is omitted.
	//
	// Ex:
	// - ^CommonFields.* will omit ALL CommonFields from the set of default columns.
	// - CommonFields.* will omit ALL CommonFields and ALL AutomationCommonFields from the set of default columns.
	//
	// Because this option matches against DQs, it WILL omit columns irrelevant of their alias!
	//
	// Remaining columns will be sorted alphabetically.
	DefaultColumnsFromExcludeRegex []*regexp.Regexp
	// Free-form function when this action is called.
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	//
	// Will not be called if --show-columns is specified.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)

	// The message that will be printed if the listFunc returns no data (and no error).
	// Uses DefaultEmptyMessage if unset.
	EmptyMessage string

	// Select flags to omit from this action, causing their values to always default to false/nil and the flags themselves to not be shown in help text.
	Omit OmitFlags
}

// OmitFlags allows disabling flags for a specific action.
// Anything set to true in here will have its equivalent flag turned off for this action.
// For example: if an asset type doesn't tombstone, --include-delete should probably be disabled.
type OmitFlags struct {
	All            bool
	IncludeDeleted bool
}

// buildFlagSet returns a flagset composed of the default list flags,
// additional flags defined for this action,
// and --pretty if a prettyFunc was defined.
//
// defaultColumnsAliased are the columns to display as defaults alongside --columns.
// They are expected to have aliases applied and will not be coerced.
func buildFlagSet(prettyDefined bool, defaultColumnsAliased []string, omit OmitFlags) *pflag.FlagSet {
	fs := pflag.FlagSet{}
	ft.CSV.Register(&fs)
	ft.JSON.Register(&fs)
	ft.Table.Register(&fs)
	fs.StringSliceP( // manually register string slice so we can set a default
		ft.SelectColumns.Name(),
		ft.SelectColumns.Shorthand(),
		defaultColumnsAliased,
		ft.SelectColumns.Usage())

	ft.ShowColumns.Register(&fs)

	ft.Output.Register(&fs)
	ft.Append.Register(&fs)
	if !omit.All {
		ft.AllColumns.Register(&fs)
	}
	if !omit.IncludeDeleted {
		ft.IncludeDeleted.Register(&fs)
	}
	// if prettyFunc was defined, bolt on pretty
	if prettyDefined {
		fs.Bool("pretty", false, "display results as prettified text.\n"+
			"Takes precedence over other format flags.\n"+
			"May or may not respect columns, default or selected via --"+ft.SelectColumns.Name()+".")
	}

	return &fs
}

// DataParameters is the set of information that a user may provide the action that is unhandled by scaffoldlist itself.
//
// For example, --show-columns will not be included as it is handled automatically,
// but --all will be as it must be handled by the ListDataFunc itself.
type DataParameters struct {
	QueryOpts *types.QueryOptions
}

// getQueryOptions generates a QueryOptions struck from the given flagset (omit flags that were not set).
func getQueryOptions(fs *pflag.FlagSet, omit OmitFlags) *types.QueryOptions {
	var err error
	var qo = &types.QueryOptions{}

	if !omit.IncludeDeleted {
		qo.IncludeDeleted, err = fs.GetBool(ft.IncludeDeleted.Name())
		clilog.GetFlag(err)
	}
	if !omit.All {
		qo.AdminMode = connection.AdminMode()
		if !qo.AdminMode { // check for --all override
			qo.AdminMode, err = fs.GetBool(ft.AllColumns.Name())
			clilog.GetFlag(err)
		}
	}

	return qo
}
