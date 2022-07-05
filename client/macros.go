/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import "github.com/gravwell/gravwell/v3/client/types"

// GetUserGroupsMacros returns all macros accessible to the current user.
func (c *Client) GetUserGroupsMacros() ([]types.SearchMacro, error) {
	var macros []types.SearchMacro
	if err := c.getStaticURL(MACROS_URL, &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// GetAllMacros (admin-only) returns all macros on the system.
func (c *Client) GetAllMacros() ([]types.SearchMacro, error) {
	var macros []types.SearchMacro
	if err := c.getStaticURL(MACROS_ALL_URL, &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// GetUserMacros returns macros belonging to the specified user.
func (c *Client) GetUserMacros(id int32) ([]types.SearchMacro, error) {
	var macros []types.SearchMacro
	if err := c.getStaticURL(userMacrosUrl(id), &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// GetGroupMacros returns macros shared with the specified group.
func (c *Client) GetGroupMacros(id int32) ([]types.SearchMacro, error) {
	var macros []types.SearchMacro
	if err := c.getStaticURL(groupMacrosUrl(id), &macros); err != nil {
		return nil, err
	}
	return macros, nil
}

// GetMacro returns detailed about a particular macro.
func (c *Client) GetMacro(id uint64) (types.SearchMacro, error) {
	var macro types.SearchMacro
	err := c.getStaticURL(macroUrl(id), &macro)
	return macro, err
}

// DeleteMacro deletes a macro.
func (c *Client) DeleteMacro(id uint64) error {
	return c.deleteStaticURL(macroUrl(id), nil)
}

// AddMacro creates a new macro with the specified name and expansion, returning
// the ID of the newly-created macro.
func (c *Client) AddMacro(m types.SearchMacro) (id uint64, err error) {
	err = c.postStaticURL(MACROS_URL, m, &id)
	return
}

// UpdateMacro modifies an existing macro.
func (c *Client) UpdateMacro(m types.SearchMacro) error {
	return c.putStaticURL(macroUrl(m.ID), m, nil)
}
