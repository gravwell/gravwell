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

// NewSearchLibrary creates a new search library entry for the current user.
func (c *Client) NewSearchLibrary(sl types.WireSearchLibrary) (wsl types.WireSearchLibrary, err error) {
	err = c.methodStaticPushURL(http.MethodPost, searchLibUrl(), sl, &wsl)
	return
}

// ListSearchLibrary returns the list of queries in the search library available to the user.
func (c *Client) ListSearchLibrary() (wsl []types.WireSearchLibrary, err error) {
	err = c.getStaticURL(searchLibUrl(), &wsl)
	return
}

// ListAllSearchLibrary (admin-only) returns the list of all search library entries for all users.
// Non-administrators will receive the same list as returned by ListSearchLibrary.
func (c *Client) ListAllSearchLibrary() (wsl []types.WireSearchLibrary, err error) {
	c.SetAdminMode()
	if err = c.getStaticURL(searchLibUrl(), &wsl); err != nil {
		wsl = nil
	}
	c.ClearAdminMode()
	return
}

// GetSearchLibrary returns a query which matches the UUID given.
// It first checks for a query with a matching ThingUUID.
// If that is not found, it looks for a query with a matching GUID, prioritizing
// queries belonging to the current user.
func (c *Client) GetSearchLibrary(id uuid.UUID) (sl types.WireSearchLibrary, err error) {
	err = c.getStaticURL(searchLibIdUrl(id), &sl)
	return
}

// DeleteSearchLibrary deletes a specific libary entry.
func (c *Client) DeleteSearchLibrary(id uuid.UUID) (err error) {
	err = c.deleteStaticURL(searchLibIdUrl(id), nil)
	return
}

// UpdateSearchLibrary updates a specific search library entry.
func (c *Client) UpdateSearchLibrary(sl types.WireSearchLibrary) (nsl types.WireSearchLibrary, err error) {
	err = c.methodStaticPushURL(http.MethodPut, searchLibIdUrl(sl.ThingUUID), sl, &nsl)
	return
}
