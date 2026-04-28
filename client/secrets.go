/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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

// ListSecrets returns all secrets accessible to the current user.
// The actual secret string will not be returned.
func (c *Client) ListSecrets(opts *types.QueryOptions) (ret types.SecretListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.postStaticURL(SECRETS_LIST_URL, opts, &ret)
	return
}

// ListAllSecrets (admin-only) returns all secrets on the system.
// The actual secret string will not be returned.
func (c *Client) ListAllSecrets(opts *types.QueryOptions) (ret types.SecretListResponse, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	opts.AdminMode = true // we'll reject this if the user isn't actually an admin
	err = c.postStaticURL(SECRETS_LIST_URL, opts, &ret)
	return
}

// CreateSecret instantiates and returns a new Secret.
// The actual secret string will not be returned.
func (c *Client) CreateSecret(sc types.SecretCreate) (sf types.Secret, err error) {
	err = c.postStaticURL(secretsUrl(), sc, &sf)
	return
}

// GetSecret fetches information about a particular Secret.
// The actual secret string will not be returned.
func (c *Client) GetSecret(id string) (s types.Secret, err error) {
	err = c.getStaticURL(secretIdUrl(id), &s)
	return
}

// GetSecretEx returns a particular secret. If the QueryOptions arg is
// not nil, applicable parameters (currently only IncludeDeleted) will
// be applied to the query.
// The actual secret string will not be returned.
func (c *Client) GetSecretEx(id string, opts *types.QueryOptions) (s types.Secret, err error) {
	if opts == nil {
		opts = &types.QueryOptions{}
	}
	err = c.getStaticURL(secretIdUrl(id), &s, ezParam("include_deleted", opts.IncludeDeleted))
	return
}

// UpdateSecretValue changes the value of a particular secret.
// The actual secret string will not be returned.
func (c *Client) UpdateSecretValue(id string, value string) (s types.Secret, err error) {
	sc := types.SecretCreate{Value: value}
	err = c.methodStaticPushURL(http.MethodPut, secretIdValueUrl(id), sc, &s, nil, nil)
	return
}

// UpdateSecret changes the details (not the value) of a particular secret.
// The actual secret string will not be returned.
func (c *Client) UpdateSecret(id string, sc types.SecretCreate) (s types.Secret, err error) {
	err = c.methodStaticPushURL(http.MethodPut, secretIdUrl(id), sc, &s, nil, nil)
	return
}

// DeleteSecret deletes a Secret.
func (c *Client) DeleteSecret(id string) (err error) {
	return c.deleteStaticURL(secretIdUrl(id), nil)
}

// PurgeSecret deletes a secret entirely, removing it from the database.
func (c *Client) PurgeSecret(id string) error {
	return c.deleteStaticURL(secretIdUrl(id), nil, ezParam("purge", "true"))
}

// CleanupSecrets (admin-only) purges all deleted secrets for all users.
func (c *Client) CleanupSecrets() error {
	return c.deleteStaticURL(SECRETS_URL, nil)
}

// GetFullSecret fetches the entire Secret, including the value.
// This can only be used if you have authenticated using the searchagent token.
// The search agent knows how to set up the Client object correctly for this.
// If you are not writing something which acts like the search agent, you don't
// want this function, it won't work.
func (c *Client) GetFullSecret(id string) (s types.SecretFull, err error) {
	err = c.getStaticURL(secretIdFullUrl(id), &s)
	return
}
