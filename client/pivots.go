/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListActionables returns all actionables accessible to the current user.
func (c *Client) ListActionables(opts *types.QueryOptions) (ret types.ActionableListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(ACTIONABLES_LIST_URL, opts, &ret)
	return
}

// ListAllActionables (admin-only) returns all actionables on the system.
func (c *Client) ListAllActionables(opts *types.QueryOptions) (ret types.ActionableListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	err = c.postStaticURL(ACTIONABLES_LIST_URL, opts, &ret)
	return
}

// GetActionable returns a particular actionable by ID.
func (c *Client) GetActionable(id string) (types.Actionable, error) {
	var actionable types.Actionable
	err := c.getStaticURL(actionableIdUrl(id), &actionable)
	return actionable, err
}

// GetActionableEx returns a particular actionable. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetActionableEx(id string, opts *types.QueryOptions) (types.Actionable, error) {
	var actionable types.Actionable
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(actionableIdUrl(id), &actionable, ezParam("include_deleted", opts.IncludeDeleted))
	return actionable, err
}

// DeleteActionable deletes an actionable by marking it deleted in the database.
func (c *Client) DeleteActionable(id string) error {
	return c.deleteStaticURL(actionableIdUrl(id), nil)
}

// PurgeActionable deletes an actionable entirely, removing it from the database.
func (c *Client) PurgeActionable(id string) error {
	return c.deleteStaticURL(actionableIdUrl(id), nil, ezParam("purge", "true"))
}

// CreateActionable creates a new actionable, returning the newly-created actionable.
func (c *Client) CreateActionable(a types.Actionable) (result types.Actionable, err error) {
	err = c.postStaticURL(ACTIONABLES_URL, a, &result)
	return
}

// UpdateActionable modifies an existing actionable and returns the complete, updated struct.
func (c *Client) UpdateActionable(ID string, p types.ActionablePatch) (updated types.Actionable, err error) {
	if ID == "" {
		return types.Actionable{}, ErrNilID
	}
	return c.patch[types.ActionablePatch, types.Actionable](actionableIdUrl(ID), p)
}

// CleanupActionables (admin-only) purges all deleted actionables for all users.
func (c *Client) CleanupActionables() error {
	return c.deleteStaticURL(ACTIONABLES_URL, nil)
}
