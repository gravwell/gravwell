/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	// Overrides the default "list" action name.
	Use string
	// Pretty defines a free-form, pretty-printing function, allowing this action to be displayed in a user-friendly (albeit likely script-unfriendly) way.
	// If !nil, --pretty will also be defined and set as the default.
	Pretty PrettyPrinterFunc
	// Replace the default list example.
	Example string
	// AddtlFlags defines a function that generates a fresh flagset to be bolted on to the default list flagset.
	// NOTE(rlandau): It must be a function returning a fresh struct because FlagSets are shallow copies, even when passed by reference.
	AddtlFlags AddtlFlagFunction
}
