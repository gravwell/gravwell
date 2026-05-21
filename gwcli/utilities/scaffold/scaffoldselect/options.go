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
)

// The Options struct allows developers to tweak parameters of an action's specific implementation.
type Options struct {
	scaffold.CommonOptions

	// The string returned to a user if collectItems returns no items.
	// If not set, defaults to a generic statement.
	NoItemsError string
}
