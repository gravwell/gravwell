/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package clearresults provides shared logic for the clear actions of automation types
// (flows, scheduled searches, scripts).
// It lives in its own package to avoid an import cycle with the automation nav.
package clearresults

import (
	"fmt"
	"strings"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

// ToResult transforms the output of a Clear*Results call into a scaffold.Result.
// kind is the singular, human-readable name of the automation type (ex: "flow").
//
// If any results were skipped, they are listed in the output.
// The clear is only considered a failure if err is non-nil or every result was skipped.
func ToResult(kind, ID string, response types.ClearResultsResponse, err error) scaffold.Result {
	if err != nil {
		return scaffold.Result{Success: false, Output: fmt.Sprintf("failed to clear results for %s %s: %v", kind, ID, err)}
	} else if len(response.Skipped) == 0 {
		return scaffold.Result{Success: true, Output: fmt.Sprintf("successfully cleared results for %s %s", kind, ID)}
	}
	var sb strings.Builder
	res := scaffold.Result{Success: true}
	if response.Deleted == 0 {
		res.Success = false
		fmt.Fprintf(&sb, "failed to clear results for %s %s: every result was skipped.", kind, ID)
	} else {
		fmt.Fprintf(&sb, "partially cleared results for %s %s.", kind, ID)
	}
	for _, r := range response.Skipped {
		fmt.Fprintf(&sb, "\n%s: %s", r.ID, r.Reason)
	}

	res.Output = sb.String()
	return res
}
