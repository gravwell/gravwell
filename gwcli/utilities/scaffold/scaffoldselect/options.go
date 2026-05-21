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

	// if opFunc succeeds, this function will be called on the ID.
	// If this function returns a string, it will be printed as the success statement.
	SuccessString func(ID any) string
}
