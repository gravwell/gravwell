/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	// Overrides the default "list" action name.
	Use string
	// Other names for this action.
	Aliases []string
	// Pretty defines a free-form, pretty-printing function, allowing this action to be displayed in a user-friendly (albeit likely script-unfriendly) way.
	// If !nil, --pretty will also be defined and set as the default.
	Pretty PrettyPrinterFunc
	// AddtlFlags defines a function that generates a fresh flagset to be bolted on to the default list flagset.
	// NOTE(rlandau): It must be a function returning a fresh struct because FlagSets are shallow copies, even when passed by reference.
	AddtlFlags AddtlFlagFunction
	// Sets the default columns to return if --columns is not specified.
	// If not set, defaults to all exported fields.
	DefaultColumns []string
	// ! Currently only applies to tables.
	//
	// ColumnAliases maps fully-dot-qualified field names -> display names in the table header.
	// Keys must exactly match native column names (from weave.StructFields());
	// unmatched aliases will be unused and native column names are case-sensitive.
	// Operates in O(len(columns)) time, if not nil.
	ColumnAliases map[string]string
	// A free-form function allowing implementations to directly alter properties on the command scaffold list creates.
	// Applied after all other options, so changes made here may override prior options (such as Use and Aliases).
	//
	// ! Do not rely on cobra.Args, as they will not be respected in interactive mode.
	// Use the ValidateArgs option instead.
	CmdMods func(*cobra.Command)
	// Free-form function called in SetArgs or at the start of run to validate the given flags.
	// You can assume that the flags have already been parsed, but that no additional actions have been taken on them.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
}
