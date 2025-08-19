/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v4/client/types"

// ListMacros returns all macros accessible to the current user.
func (c *Client) ListMacros(opts *types.QueryOptions) ([]types.Macro, error) {
	var macros []types.Macro
	if err := c.postStaticURL(MACROS_LIST_URL, opts, &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// ListAllMacros (admin-only) returns all macros on the system.
func (c *Client) ListAllMacros(opts *types.QueryOptions) ([]types.Macro, error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	var macros []types.Macro
	if err := c.postStaticURL(MACROS_LIST_URL, opts, &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// GetMacro returns detailed about a particular macro.
func (c *Client) GetMacro(id string) (types.Macro, error) {
	var macro types.Macro
	err := c.getStaticURL(macroUrl(id), &macro)
	return macro, err
}

// DeleteMacro deletes a macro by marking it deleted in the database.
func (c *Client) DeleteMacro(id string) error {
	return c.deleteStaticURL(macroUrl(id), nil)
}

// PurgeMacro deletes a macro entirely, removing it from the database.
func (c *Client) PurgeMacro(id string) error {
	return c.deleteStaticURL(macroPurgeUrl(id), nil)
}

// CreateMacro creates a new macro with the specified name and expansion, returning
// the ID of the newly-created macro.
func (c *Client) CreateMacro(m types.Macro) (id string, err error) {
	err = c.postStaticURL(MACROS_URL, m, &id)
	return
}

// UpdateMacro modifies an existing macro.
func (c *Client) UpdateMacro(m types.Macro) error {
	return c.putStaticURL(macroUrl(m.ID), m)
}

// CleanupMacros (admin-only) purges all deleted macros for all users.
func (c *Client) CleanupMacros() error {
	return c.deleteStaticURL(MACROS_URL, nil)
}
