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

	"github.com/gravwell/gravwell/v4/client/types"
)

// CreateSavedQuery creates a new saved query for the current user.
func (c *Client) CreateSavedQuery(sl types.SavedQuery) (wsl types.SavedQuery, err error) {
	err = c.methodStaticPushURL(http.MethodPost, searchLibUrl(), sl, &wsl, nil, nil)
	return
}

// ListSavedQueries returns the list of queries in the search library available to the user.
func (c *Client) ListSavedQueries(opts *types.QueryOptions) (wsl types.SavedQueryListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(LIBRARY_LIST_URL, opts, &wsl)
	return
}

// ListAllSavedQueries (admin-only) returns the list of all search library entries for all users.
// Non-administrators will receive the same list as returned by ListSavedQueries.
func (c *Client) ListAllSavedQueries(opts *types.QueryOptions) (wsl types.SavedQueryListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(LIBRARY_LIST_URL, opts, &wsl)
	return
}

// GetSavedQuery returns a query which matches the UUID given.
// It first checks for a query with a matching ThingUUID.
// If that is not found, it looks for a query with a matching GUID, prioritizing
// queries belonging to the current user.
func (c *Client) GetSavedQuery(id string) (sl types.SavedQuery, err error) {
	err = c.getStaticURL(searchLibIdUrl(id), &sl)
	return
}

// DeleteSavedQuery deletes a specific library entry.
func (c *Client) DeleteSavedQuery(id string) (err error) {
	err = c.deleteStaticURL(searchLibIdUrl(id), nil)
	return
}

// PurgeSavedQuery deletes a specific library entry.
func (c *Client) PurgeSavedQuery(id string) (err error) {
	err = c.deleteStaticURL(searchLibIdUrl(id), nil, ezParam("purge", "true"))
	return
}

// UpdateSavedQuery updates a specific search library entry.
func (c *Client) UpdateSavedQuery(sl types.SavedQuery) (nsl types.SavedQuery, err error) {
	err = c.methodStaticPushURL(http.MethodPut, searchLibIdUrl(sl.ID), sl, &nsl, nil, nil)
	return
}
