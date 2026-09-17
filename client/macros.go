/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"github.com/gravwell/gravwell/v4/client/types"
)

// ListMacros returns all macros accessible to the current user.
func (c *Client) ListMacros(opts *types.QueryOptions) (ret types.MacroListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.MacroListResponse](MACROS_LIST_URL, opts)
}

// ListAllMacros (admin-only) returns all macros on the system.
func (c *Client) ListAllMacros(opts *types.QueryOptions) (ret types.MacroListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	return c.post[types.QueryOptions, types.MacroListResponse](MACROS_LIST_URL, opts)
}

// GetMacro returns a particular macro.
func (c *Client) GetMacro(id string) (types.Macro, error) {
	return c.GetMacroEx(id, GetOptions{})
}

// GetMacroEx returns a particular macro, modified by opts.
func (c *Client) GetMacroEx(id string, opts GetOptions) (types.Macro, error) {
	return c.get[types.Macro](macroIDUrl(id), opts.params()...)
}

// DeleteMacro deletes a macro by marking it deleted in the database.
func (c *Client) DeleteMacro(id string) error {
	return c.delete(macroIDUrl(id), false)
}

// PurgeMacro deletes a macro entirely, removing it from the database.
func (c *Client) PurgeMacro(id string) error {
	return c.delete(macroIDUrl(id), true)
}

// CreateMacro creates a new macro, returning the newly-created macro.
func (c *Client) CreateMacro(m types.Macro) (result types.Macro, err error) {
	return c.post[types.Macro, types.Macro](MACROS_URL, &m)
}

// UpdateMacro modifies an existing macro and returns the complete, updated struct.
func (c *Client) UpdateMacro(ID string, p types.MacroPatch) (updated types.Macro, _ error) {
	if ID == "" {
		return types.Macro{}, ErrEmptyID
	}
	return c.patch[types.MacroPatch, types.Macro](macroIDUrl(ID), p)
}

// CleanupMacros (admin-only) purges all deleted macros for all users.
func (c *Client) CleanupMacros() error {
	return c.delete(MACROS_URL, false)
}
