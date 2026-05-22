/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldselect

import (
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
)

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	scaffold.CommonOptions

	// The string returned to a user if collectItems returns no items.
	// If not set, defaults to a generic statement.
	//
	// NOTE(rlandau): Because collectItems() is only called for interactive mode
	// (cobra-mode passes the given IDs directly to operate()),
	// non-interactive mode does not know if there are any valid targets in the first place.
	// Therefore, this is never shown in non-interactive mode; all IDs would simply be treated as invalid by operate().
	NoItemsError func(*pflag.FlagSet) string
	// Called as soon as the action is invoked.
	// You may assume that the flags have already been parsed, but that no additional actions have been taken on them.
	ValidateArgs func(*pflag.FlagSet) (invalid string, err error)
}
