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

func (c *Client) ListSecrets() (s []types.Secret, err error) {
	err = c.getStaticURL(secretsUrl(), &s)
	return
}

func (c *Client) CreateSecret(sc types.SecretCreate) (sf types.Secret, err error) {
	err = c.postStaticURL(secretsUrl(), sc, &sf)
	return
}

func (c *Client) SecretInfo(id uuid.UUID) (s types.Secret, err error) {
	err = c.getStaticURL(secretIdUrl(id), &s)
	return
}

func (c *Client) UpdateSecret(id uuid.UUID, sc types.SecretCreate) (s types.Secret, err error) {
	err = c.methodStaticPushURL(http.MethodPut, secretIdUrl(id), sc, &s)
	return
}

func (c *Client) DeleteSecret(id uuid.UUID) (err error) {
	return c.methodStaticPushURL(http.MethodDelete, secretIdUrl(id), nil, nil, http.StatusNoContent)
}
