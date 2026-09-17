/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffolddelete

import (
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// Options allows developers to tweak parameters of a delete action's specific implementation.
type Options struct {
	scaffold.CommonOptions
	// QueryOptionsFlags can take a QOBuilder to configure how this action should handle query options flags for the fetchFunc.
	QueryOptionsFlags scaffold.QOBuilder
}

// DataParameters is the set of information that a user may provide the action that is unhandled by scaffolddelete itself.
// Follows the same logic as scaffoldlist.DataParameters and may be merged with it eventually.
type DataParameters struct {
	QueryOpts *types.QueryOptions
}
