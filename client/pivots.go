/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

// ListPivots returns a list of pivots accessible to the current user.
func (c *Client) ListPivots() (pivots []types.WirePivot, err error) {
	err = c.getStaticURL(pivotsUrl(), &pivots)
	return
}

// ListAllPivots returns the list of all pivots in the system
func (c *Client) ListAllPivots() (pivots []types.WirePivot, err error) {
	if !c.userDetails.Admin {
		err = ErrNotAdmin
	} else {
		c.SetAdminMode()
		if err = c.getStaticURL(pivotsUrl(), &pivots); err != nil {
			pivots = nil
		}
		c.ClearAdminMode()
	}
	return
}

// NewPivot creates a new pivot with the given GUID, name, description, contents.
// If guid is set to uuid.Nil, a random GUID will be chosen automatically.
func (c *Client) NewPivot(guid uuid.UUID, name, description string, contents types.RawObject) (storedGuid uuid.UUID, err error) {
	pivot := types.Pivot{GUID: guid, Contents: contents, Name: name, Description: description}
	var ret types.WirePivot
	err = c.methodStaticPushURL(http.MethodPost, pivotsUrl(), pivot, &ret)
	storedGuid = ret.GUID
	return
}

// GetPivot returns a types.WirePivot with the requested GUID.
// Because unique GUIDs are not enforced, the following precedence
// is used when selecting a pivot to return:
// 1. Pivots owned by the user always have highest priority
// 2. Pivots shared with a group to which the user belongs are next
// 3. Global pivots are the lowest priority
func (c *Client) GetPivot(guid uuid.UUID) (pivot types.WirePivot, err error) {
	err = c.getStaticURL(pivotsGuidUrl(guid), &pivot)
	return
}

// SetPivot allows the owner of a pivot (or an admin) to update
// the contents of the pivot.
func (c *Client) SetPivot(guid uuid.UUID, pivot types.WirePivot) (details types.WirePivot, err error) {
	err = c.methodStaticPushURL(http.MethodPut, pivotsGuidUrl(guid), pivot, &details)
	return
}

// DeletePivot deletes the pivot with the specified GUID
func (c *Client) DeletePivot(guid uuid.UUID) (err error) {
	err = c.deleteStaticURL(pivotsGuidUrl(guid), nil)
	return
}
