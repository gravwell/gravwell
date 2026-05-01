/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
)

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	scaffold.CommonOptions

	// Pretty defines a free-form, pretty-printing function, allowing this action to be displayed in a user-friendly (albeit likely script-unfriendly) way.
	// If !nil, --pretty will also be defined and set as the default.
	//
	// Pretty functions may or may not respect columns.
	Pretty PrettyPrinterFunc

	// Sets the default columns to display if --columns is not specified.
	// Column names must be dot-qualified exact matches, not aliases.
	// If set, only these columns will be displayed by default.
	//
	// Mutually exclusive with ExcludeColumnsFromDefault.
	DefaultColumns []string
	// Sets the list to display all columns EXCEPT for these by default.
	// Column names must be dot-qualified exact matches, not aliases.
	// Unknown columns will be ignored.
	// Overridden by --columns.
	//
	// Mutually exclusive with DefaultColumns.
	ExcludeColumnsFromDefault []string
	// Free-form function called in SetArgs or at the start of run to validate the given flags.
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	//
	// Will not be called if --show-columns is specified.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
}
