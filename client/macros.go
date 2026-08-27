/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/gravwell/gravwell/v4/client/types"
)

// ListMacros returns all macros accessible to the current user.
func (c *Client) ListMacros(opts *types.QueryOptions) (ret types.MacroListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(MACROS_LIST_URL, opts, &ret)
	return
}

// ListAllMacros (admin-only) returns all macros on the system.
func (c *Client) ListAllMacros(opts *types.QueryOptions) (ret types.MacroListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(MACROS_LIST_URL, opts, &ret)
	return
}

// GetMacro returns a particular macro.
func (c *Client) GetMacro(id string) (types.Macro, error) {
	var macro types.Macro
	err := c.getStaticURL(macroUrl(id), &macro)
	return macro, err
}

// GetMacroEx returns a particular macro. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetMacroEx(id string, opts *types.QueryOptions) (types.Macro, error) {
	var macro types.Macro
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(macroUrl(id), &macro, ezParam("include_deleted", opts.IncludeDeleted))
	return macro, err
}

// DeleteMacro deletes a macro by marking it deleted in the database.
func (c *Client) DeleteMacro(id string) error {
	return c.deleteStaticURL(macroUrl(id), nil)
}

// PurgeMacro deletes a macro entirely, removing it from the database.
func (c *Client) PurgeMacro(id string) error {
	return c.deleteStaticURL(macroUrl(id), nil, ezParam("purge", "true"))
}

// CreateMacro creates a new macro, returning the newly-created macro.
func (c *Client) CreateMacro(m types.Macro) (result types.Macro, err error) {
	err = c.postStaticURL(MACROS_URL, m, &result)
	return
}

var (
	ErrEmptyPatch  = errors.New("empty PATCHs are ineffectual")
	ErrNilResponse = errors.New("nil response")
	ErrNilID       = errors.New("ID must not be nil")
)

// UpdateMacro modifies an existing macro and returns complete, updated struct.
func (c *Client) UpdateMacro(ID string, p types.MacroPatch) (updated types.Macro, _ error) {
	if ID == "" {
		return types.Macro{}, ErrNilID
	}
	return c.PATCH[types.MacroPatch, types.Macro](macroUrl(ID), p)
}

// PATCH marshals data to JSON and submits a PATCH request against the target URL.
//
// TODO we can probably consolidate a lot of the methodStaticPushURLs by taking a method param in
// wrapper calls and converting this to a driver func
func (c *Client) PATCH[T types.PatchType, K any](url string, data T) (patched K, _ error) {
	body, err := json.Marshal(data, json.OmitZeroStructFields(true))
	if err != nil {
		return patched, err
	} else if string(body) == "{}" { // if this marshaled to no data, throw away the request
		return patched, ErrEmptyPatch
	}

	uri := fmt.Sprintf("%s://%s%s", c.httpScheme, c.server, url)
	req, err := http.NewRequest(http.MethodPatch, uri, bytes.NewBuffer(body))
	if err != nil {
		return patched, err
	}
	req.Header.Set("Content-Type", "application/json")

	c.hm.populateRequest(req.Header) // add in the headers

	// add in any queries like ?admin=true
	if req.URL.RawQuery, err = c.qm.appendEncode(req.URL.RawQuery); err != nil {
		return patched, err
	}

	c.objLog.Log("WEB REQ "+http.MethodPatch, url, data)
	resp, err := c.clnt.Do(req)
	if err != nil {
		c.objLog.Log("WEB "+http.MethodPatch+" Error "+err.Error(), url, nil)
		return patched, err
	}
	if resp == nil {
		return patched, ErrNilResponse
	}
	if resp.StatusCode != http.StatusOK {
		c.objLog.Log("WEB "+http.MethodPatch, url+" "+resp.Status, nil)
		return patched, aliasResponseError(c, resp)
	}
	defer drainResponse(resp)

	if err := json.UnmarshalRead(resp.Body, &patched); err != nil {
		return patched, err
	}

	c.objLog.Log("WEB RECV", url, patched)
	return patched, nil
}

// CleanupMacros (admin-only) purges all deleted macros for all users.
func (c *Client) CleanupMacros() error {
	return c.deleteStaticURL(MACROS_URL, nil)
}
