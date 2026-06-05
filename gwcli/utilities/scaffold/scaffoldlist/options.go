/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/ingest/log"
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
	Everything bool // disable everything. This is useful if the ListDataFunc doesn't take query opts.

	AllData        bool
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

	fs.StringSliceP(FlagNameSelectColumns, "", defaultColumnsAliased,
		"comma-separated list of columns to include in the results.\n"+
			"Use --"+FlagNameShowColumns+" to see the full list of columns.\n"+
			"Mutually exclusive with --"+FlagNameSelectAllColumns)

	fs.Bool(FlagNameShowColumns, false, "display available columns (for use with --columns) and exit.\n"+
		"Causes all other flags to be ignored")

	ft.Output.Register(&fs)
	ft.Append.Register(&fs)
	fs.Bool(FlagNameSelectAllColumns, false,
		"displays data from all columns, ignoring the default column set.\n"+
			"Mutually exclusive with --"+FlagNameSelectColumns)

	// attach query option flags, depending on their omit state
	if !omit.Everything {
		if !omit.AllData {
			fs.Bool(FlagNameAllData, false, "requests that results include data from "+stylesheet.Italicize("all")+" users and groups instead of just yours.\n"+
				"Ignored if you are not an admin.\n"+
				"Implied by admin mode")
		}
		if !omit.IncludeDeleted {
			ft.IncludeDeleted.Register(&fs)
		}
	}

	// if prettyFunc was defined, bolt on pretty
	if prettyDefined {
		fs.Bool("pretty", false, "display results as prettified text.\n"+
			"Takes precedence over other format flags.\n"+
			"May or may not respect columns, default or selected")
	}

	return &fs
}

// fetches values from the flagset that scaffoldlist uses directly (as opposed to getQueryOptions()).
func getFlags(fs *pflag.FlagSet, DQToAlias, AliasToDQ map[string]string, prettyDefined bool) (
	showColumns bool, columns []string, outFile *os.File, format outputFormat, invalid string,
) {
	show, err := fs.GetBool(FlagNameShowColumns)
	clilog.GetFlag(err)
	if show { // job's done
		return true, nil, nil, 0, ""
	}
	if outFile, err = initOutFile(fs); err != nil {
		return true, nil, nil, 0, err.Error()
	}
	if columns, invalid = getColumns(fs, DQToAlias, AliasToDQ); invalid != "" {
		return true, nil, nil, 0, invalid
	}
	format = determineFormat(fs, prettyDefined)
	return
}

// getColumns figures out which columns this request should receive and returns the DQ version of each.
//
// In order of priority:
//
//  1. all columns (if --all), sorted alphabetically
//
//  2. selected columns (if --columns=<>), retaining given order
//
//  3. default columns, sorted alphabetically
func getColumns(fs *pflag.FlagSet, DQToAlias, AliasToDQ map[string]string) (_ []string, invalid string) {
	selectAll, err := fs.GetBool(FlagNameSelectAllColumns)
	clilog.GetFlag(err)
	selectColumns, err := fs.GetStringSlice(FlagNameSelectColumns) // this will return either the user-spec'd columns or the default columns
	clilog.GetFlag(err)

	// MX check
	if selectAll && fs.Changed(FlagNameSelectColumns) {
		return nil, ft.ErrMutuallyExclusive(FlagNameSelectAllColumns, FlagNameSelectColumns).Error()
	}

	// collect columns
	if selectAll {
		// normalize all column names
		normal, unknown := normalizeToDQ(sortColumns(slices.Collect(maps.Keys(DQToAlias))), DQToAlias, AliasToDQ)
		// we should never get unknown columns when giving the full set; this is a developer error
		if len(unknown) > 0 {
			clilog.Writer.Error("got unknown columns while normalizing the full column set.",
				log.KV("unknown columns", unknown),
				scaffold.IdentifyCaller())
			return nil, clilog.ErrInternal{}.Error() // this isn't technically an invalid but its also super unlikely to ever happen so...
		}
		return normal, ""
	}

	normalized, unknown := normalizeToDQ(selectColumns, DQToAlias, AliasToDQ)
	if len(unknown) > 0 {
		return nil, fmt.Sprintf("unknown columns: %v", unknown)
	}
	return normalized, ""
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

	return qo
}
