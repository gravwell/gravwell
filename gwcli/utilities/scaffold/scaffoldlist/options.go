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
	DefaultColumnsFromExcludeRegex []*regexp.Regexp
	// Free-form function when this action is called.
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	//
	// Will not be called if --show-columns is specified.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
}
