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

// TokenCapabilities returns a list of strings which are valid options
// for the Capabilities in a token definition.
func (c *Client) TokenCapabilities() (cl []string, err error) {
	err = c.getStaticURL(tokenCapabilitiesUrl(), &cl)
	return
}

// CreateToken instantiates a new token. Note that this is the only
// case in which a TokenFull object (containing the Value field) is
// returned.
func (c *Client) CreateToken(tc types.TokenCreate) (tf types.TokenFull, err error) {
	err = c.postStaticURL(tokensUrl(), tc, &tf)
	return
}

// ListTokens gets a list of tokens accessible to the user. If
// non-nil, the QueryOptions will be applied for pagination,
// filtering, etc.
func (c *Client) ListTokens(opts *types.QueryOptions) (ts []types.Token, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(TOKENS_LIST_URL, opts, &ts)
	return
}

// GetToken returns a particular token.
func (c *Client) GetToken(id string) (t types.Token, err error) {
	err = c.getStaticURL(tokenIdUrl(id), &t)
	return
}

// GetTokenEx returns a particular token. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetTokenEx(id string, opts *types.QueryOptions) (types.Token, error) {
	var token types.Token
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err := c.getStaticURL(tokenIdUrl(id), &token, ezParam("include_deleted", opts.IncludeDeleted))
	return token, err
}

// UpdateToken modifies an existing token.
func (c *Client) UpdateToken(id string, tr types.TokenCreate) (t types.Token, err error) {
	err = c.methodStaticPushURL(http.MethodPut, tokenIdUrl(id), tr, &t, nil, nil)
	return
}

// DeleteToken marks a token as deleted.
func (c *Client) DeleteToken(id string) (err error) {
	return c.methodStaticPushURL(http.MethodDelete, tokenIdUrl(id), nil, nil, []int{http.StatusNoContent}, nil)
}

// PurgeToken completely deletes a token.
func (c *Client) PurgeToken(id string) (err error) {
	return c.methodStaticPushURL(http.MethodDelete, tokenIdUrl(id), nil, nil, []int{http.StatusNoContent}, []urlParam{ezParam("purge", "true")})
}

// CleanupTokens (admin-only) purges all deleted tokens for all users.
func (c *Client) CleanupTokens() error {
	return c.deleteStaticURL(tokensUrl(), nil)
}
