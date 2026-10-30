/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit

import (
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// Options allows developers to tweak parameters of a delete action's specific implementation.
type Options struct {
	scaffold.CommonOptions
}
