//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package clearresults_test

import (
	"errors"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/tree/automation/internal/clearresults"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
)

func TestToResult(t *testing.T) {
	tests := []struct {
		name     string
		response types.ClearResultsResponse
		err      error
		want     scaffold.Result
	}{
		{"error", types.ClearResultsResponse{}, errors.New("boom"),
			scaffold.Result{Success: false, Output: "failed to clear results for flow 1: boom"}},
		{"none skipped", types.ClearResultsResponse{Deleted: 3}, nil,
			scaffold.Result{Success: true, Output: "successfully cleared results for flow 1"}},
		{"nothing to clear", types.ClearResultsResponse{}, nil,
			scaffold.Result{Success: true, Output: "successfully cleared results for flow 1"}},
		{"partial",
			types.ClearResultsResponse{Deleted: 2, Skipped: []types.BulkDeleteSkip{
				{ID: "a", Reason: types.BulkDeleteSkipPermissionDenied},
			}}, nil,
			scaffold.Result{Success: true,
				Output: "partially cleared results for flow 1.\na: permission_denied"}},
		{"all skipped",
			types.ClearResultsResponse{Skipped: []types.BulkDeleteSkip{
				{ID: "a", Reason: types.BulkDeleteSkipPermissionDenied},
				{ID: "b", Reason: types.BulkDeleteSkipNotFound},
			}}, nil,
			scaffold.Result{Success: false,
				Output: "failed to clear results for flow 1: every result was skipped." +
					"\na: permission_denied\nb: not_found"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clearresults.ToResult("flow", "1", tt.response, tt.err); got != tt.want {
				t.Errorf("ToResult() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
