/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v4/client/types"

// ExploreGenerate asks the webserver which autoextractors it could install for a tag.
// The webserver samples the tag itself and tries every extraction module against what it
// finds, so the caller supplies nothing but the tag name. Each result carries a candidate
// AX definition, a confidence score, and the sample rendered through that definition.
// Results come back sorted by confidence, highest first.
func (c *Client) ExploreGenerate(tag string) (pa []types.PotentialAutoExtractor, err error) {
	err = c.getStaticURL(exploreGenerateUrl(), &pa, ezParam("tag", tag))
	return
}
