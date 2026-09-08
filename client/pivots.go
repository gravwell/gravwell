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
	return c.post[types.QueryOptions, types.ActionableListResponse](ACTIONABLES_LIST_URL, opts)
}

// ListAllActionables (admin-only) returns all actionables on the system.
func (c *Client) ListAllActionables(opts *types.QueryOptions) (ret types.ActionableListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true
	return c.post[types.QueryOptions, types.ActionableListResponse](ACTIONABLES_LIST_URL, opts)
}

// GetActionable returns a particular actionable by ID.
func (c *Client) GetActionable(id string) (types.Actionable, error) {
	return c.GetActionableEx(id, GetOptions{})
}

// GetActionableEx returns a particular actionable, modified by opts.
func (c *Client) GetActionableEx(id string, opts GetOptions) (types.Actionable, error) {
	return c.get[types.Actionable](actionableIdUrl(id), opts.params()...)
}

// DeleteActionable deletes an actionable by marking it deleted in the database.
func (c *Client) DeleteActionable(id string) error {
	return c.delete(actionableIdUrl(id), false)
}

// PurgeActionable deletes an actionable entirely, removing it from the database.
func (c *Client) PurgeActionable(id string) error {
	return c.delete(actionableIdUrl(id), true)
}

// CreateActionable creates a new actionable, returning the newly-created actionable.
func (c *Client) CreateActionable(a types.Actionable) (result types.Actionable, err error) {
	return c.post[types.Actionable, types.Actionable](ACTIONABLES_URL, &a)
}

// UpdateActionable modifies an existing actionable and returns the complete, updated struct.
func (c *Client) UpdateActionable(ID string, p types.ActionablePatch) (updated types.Actionable, err error) {
	if ID == "" {
		return types.Actionable{}, ErrEmptyID
	}
	return c.patch[types.ActionablePatch, types.Actionable](actionableIdUrl(ID), p)
}

// CleanupActionables (admin-only) purges all deleted actionables for all users.
func (c *Client) CleanupActionables() error {
	return c.delete(ACTIONABLES_URL, false)
}
