/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"net/http"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
)

// GetTOTPSetup requests the parameters necessary for configuring
// TOTP when the user does not have any MFA set up at all.
func (c *Client) GetTOTPSetup(user, pass string) (types.MFATOTPSetupResponse, error) {
	return c.GetTOTPSetupEx(user, pass, types.AUTH_TYPE_NONE, "")
}

// GetTOTPSetupEx requests the parameters necessary for configuring
// TOTP. If any form of MFA is already configured for that account, a
// valid authtype and MFA code must be specified in addition to
// username and password. If MFA is not set up, "AUTH_TYPE_NONE" may
// be passed along with an empty code.
func (c *Client) GetTOTPSetupEx(user, pass string, authtype types.AuthType, code string) (types.MFATOTPSetupResponse, error) {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: authtype,
		AuthCode: code,
	}
	var resp types.MFATOTPSetupResponse
	err := c.postStaticURL(totpSetupUrl(), rq, &resp)
	return resp, err
}

// InstallTOTPSetup installs the parameters requested by
// GetTOTPSetup. The code parameter should be generated from the URL
// in the reponse.
func (c *Client) InstallTOTPSetup(user, pass, code string) (types.MFATOTPInstallResponse, error) {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: types.AUTH_TYPE_TOTP,
		AuthCode: code,
	}
	var resp types.MFATOTPInstallResponse
	err := c.methodStaticPushURL(http.MethodPut, totpSetupUrl(), rq, &resp, nil, nil)
	return resp, err
}

// TOTPLogin does a login using TOTP as the second factor.
func (c *Client) TOTPLogin(user, pass, code string) (types.LoginResponse, error) {
	return c.MFALogin(user, pass, types.AUTH_TYPE_TOTP, code)
}

// TOTPClear deletes the user's TOTP setup.
// Note that this may return an error if another MFA method is not configured.
func (c *Client) TOTPClear(user, pass string, authtype types.AuthType, code string) error {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: authtype,
		AuthCode: code,
	}
	err := c.methodStaticPushURL(http.MethodPost, totpClearUrl(), rq, nil, nil, nil)
	return err
}

// GetMFAInfo returns information about the system's MFA policies and
// the user's MFA setup.
func (c *Client) GetMFAInfo() (resp types.MFAInfo, err error) {
	err = c.getStaticURL(mfaUrl(), &resp)
	return
}

// ClearAllMFA completely clears the current user's MFA configuration, if allowed by site policy.
func (c *Client) ClearAllMFA(user, pass string, authtype types.AuthType, code string) error {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: authtype,
		AuthCode: code,
	}
	return c.methodStaticPushURL(http.MethodPost, mfaClearAllUrl(), rq, nil, nil, nil)
}

// AdminClearUserMFA completely clears the specified user's MFA
// configuration. They will have to re-configure MFA on their next
// login.
func (c *Client) AdminClearUserMFA(uid int32) error {
	return c.methodStaticParamURL(http.MethodDelete, clearUserMFAUrl(uid), nil, nil)
}

// GenerateRecoveryCodes regenerates the user's recovery codes.
func (c *Client) GenerateRecoveryCodes(user, pass string, authtype types.AuthType, code string) (codes types.RecoveryCodes, err error) {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: authtype,
		AuthCode: code,
	}
	// We have to do a little dance here because to be extra safe,
	// we've specified that types.RecoveryCodes should ignore the
	// Codes field when doing JSON encoding/decoding.
	var resp struct {
		Enabled   bool
		Codes     []string
		Remaining int
		Generated time.Time
	}
	err = c.methodStaticPushURL(http.MethodPost, mfaGenerateRecoveryCodesUrl(), rq, &resp, nil, nil)
	codes = types.RecoveryCodes{resp.Enabled, resp.Codes, resp.Remaining, resp.Generated}
	return
}
