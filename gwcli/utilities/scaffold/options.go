/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffold

import (
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
