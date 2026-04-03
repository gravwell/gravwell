/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import "github.com/spf13/pflag"

type Options struct {
	// Overrides the default "create" action name.
	Use string
	// Other names for this action.
	Aliases []string
	// Defines a function to generate a fresh flagset to bolt onto the default flags.
	AddtlFlags func() pflag.FlagSet
}
