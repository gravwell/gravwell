/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// Label is a single distinct label in use on assets the caller can read,
// as returned by the label list endpoint.
type Label struct {
	Value string
}

// LabelListResponse is a page of labels returned by the label list endpoint.
type LabelListResponse = ListResponse[Label]
