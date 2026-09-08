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

// TokenCapabilities returns a list of strings which are valid options
// for the Capabilities in a token definition.
func (c *Client) TokenCapabilities() (cl []string, err error) {
	return c.get[[]string](tokenCapabilitiesUrl())
}

// CreateToken instantiates a new token. CreateToken and RegenToken are
// the only cases in which a TokenFull object (containing the Value field)
// is returned.
func (c *Client) CreateToken(tc types.Token) (tf types.TokenFull, err error) {
	return c.post[types.Token, types.TokenFull](tokensUrl(), &tc)
}

// ListTokens gets a list of tokens accessible to the user. If
// non-nil, the QueryOptions will be applied for pagination,
// filtering, etc.
func (c *Client) ListTokens(opts *types.QueryOptions) (ts types.TokenListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	return c.post[types.QueryOptions, types.TokenListResponse](TOKENS_LIST_URL, opts)
}

// GetToken returns a particular token.
func (c *Client) GetToken(id string) (t types.Token, err error) {
	return c.GetTokenEx(id, GetOptions{})
}

// GetTokenEx returns a particular token. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
func (c *Client) GetTokenEx(id string, opts GetOptions) (types.Token, error) {
	return c.get[types.Token](tokenIdUrl(id), opts.params()...)
}

// UpdateToken modifies an existing token and returns the complete, updated struct.
func (c *Client) UpdateToken(id string, p types.TokenPatch) (updated types.Token, err error) {
	if id == "" {
		return types.Token{}, ErrEmptyID
	}
	return c.patch[types.TokenPatch, types.Token](tokenIdUrl(id), p)
}

// RegenToken requests that the secret token string be regenerated without modifying the token contents or permissions
func (c *Client) RegenToken(id string, tr types.TokenRegeneration) (t types.TokenFull, err error) {
	return c.patch[types.TokenRegeneration, types.TokenFull](tokenIDRegenURL(id), tr)
}

// DeleteToken removes a token value without deleting the data around the token, it essentially disables the token
func (c *Client) DeleteToken(id string) (err error) {
	return c.delete(tokenIdUrl(id), false)
}

// PurgeToken completely deletes a token.
func (c *Client) PurgeToken(id string) (err error) {
	return c.delete(tokenIdUrl(id), true)
}

// CleanupTokens (admin-only) purges all deleted tokens for all users.
func (c *Client) CleanupTokens() error {
	return c.delete(tokensUrl(), false)
}
