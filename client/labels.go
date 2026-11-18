/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v4/client/types"

// ListLabels returns a page of the distinct labels on assets readable by the
// current user. Filters and OrderBy in opts take the label fields (ex: Value).
func (c *Client) ListLabels(opts types.QueryOptions) (ret types.LabelListResponse, err error) {
	return c.post[types.QueryOptions, types.LabelListResponse](LIST_LABELS_URL, &opts)
}
