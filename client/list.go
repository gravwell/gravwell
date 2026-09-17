/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v4/client/types"

// ListAll lists all assets on the system. You can filter on the Type field to restrict to a subset of asset types.
func (c *Client) ListAll(opts *types.QueryOptions) (ret types.ListAllResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(LIST_URL, opts, &ret)
	return
}
