/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v3/client/types"

// ExploreGenerate takes a tag name and an array of one or more SearchEntry objects as arguments.
// It has the webserver attempt various data exploration extractions and returns a map of the results.
// The map keys are extraction modules, e.g. "json" or "winlog". The map values are arrays of
// GenerateAXResponse structures, each representing one possible extraction of the data, including
// an AX definition which can be installed if the user deems the extraction appropriate.
func (c *Client) ExploreGenerate(tag string, ents []types.SearchEntry) (mp map[string][]types.GenerateAXResponse, err error) {
	req := types.GenerateAXRequest{
		Tag:     tag,
		Entries: ents,
	}
	err = c.postStaticURL(exploreGenerateUrl(), req, &mp)
	return
}
