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

	"github.com/gravwell/gravwell/v3/client/types"

	"github.com/google/uuid"
)

func (c *Client) TokenCapabilities() (cl []string, err error) {
	err = c.getStaticURL(tokenCapabilitiesUrl(), &cl)
	return
}

func (c *Client) CreateToken(tc types.TokenCreate) (tf types.TokenFull, err error) {
	err = c.postStaticURL(tokensUrl(), tc, &tf)
	return
}

func (c *Client) ListTokens() (ts []types.Token, err error) {
	err = c.getStaticURL(tokensUrl(), &ts)
	return
}

func (c *Client) TokenInfo(id uuid.UUID) (t types.Token, err error) {
	err = c.getStaticURL(tokenIdUrl(id), &t)
	return
}

func (c *Client) UpdateToken(id uuid.UUID, tr types.TokenCreate) (t types.Token, err error) {
	err = c.methodStaticPushURL(http.MethodPut, tokenIdUrl(id), tr, &t)
	return
}

func (c *Client) DeleteToken(id uuid.UUID) (err error) {
	return c.methodStaticPushURL(http.MethodDelete, tokenIdUrl(id), nil, nil, http.StatusNoContent)
}
