/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/client/types"
)

// ListSecrets returns a list of all Secret objects the user has access to.
// The actual secret string will not be returned.
func (c *Client) ListSecrets() (s []types.Secret, err error) {
	err = c.getStaticURL(secretsUrl(), &s)
	return
}

// CreateSecret instantiates and returns a new Secret.
// The actual secret string will not be returned.
func (c *Client) CreateSecret(sc types.SecretCreate) (sf types.Secret, err error) {
	err = c.postStaticURL(secretsUrl(), sc, &sf)
	return
}

// SecretInfo fetches information about a particular Secret.
// The actual secret string will not be returned.
func (c *Client) SecretInfo(id uuid.UUID) (s types.Secret, err error) {
	err = c.getStaticURL(secretIdUrl(id), &s)
	return
}

// UpdateSecret changes the settings of a particular secret.
// The actual secret string will not be returned.
func (c *Client) UpdateSecret(id uuid.UUID, sc types.SecretCreate) (s types.Secret, err error) {
	err = c.methodStaticPushURL(http.MethodPut, secretIdUrl(id), sc, &s)
	return
}

// DeleteSecret deletes a Secret.
func (c *Client) DeleteSecret(id uuid.UUID) (err error) {
	return c.methodStaticPushURL(http.MethodDelete, secretIdUrl(id), nil, nil, http.StatusNoContent)
}

// GetFullSecret fetches the entire Secret, including the value.
// This can only be used if you have authenticated using the searchagent token.
// The search agent knows how to set up the Client object correctly for this.
// If you are not writing something which acts like the search agent, you don't
// want this function, it won't work.
func (c *Client) GetFullSecret(id uuid.UUID) (s types.SecretFull, err error) {
	err = c.getStaticURL(secretIdFullUrl(id), &s)
	return
}
