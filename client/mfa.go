package client

import "github.com/gravwell/gravwell/v3/client/types"
import "net/http"

// GetTOTPSetup requests the parameters necessary for configuring TOTP on an
// account which does not yet have any MFA configured.
func (c *Client) GetTOTPSetup(user, pass string) (types.MFATOTPSetupResponse, error) {
	rq := types.MFAAuthRequest{
		User: user,
		Pass: pass,
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
	err := c.methodStaticPushURL(http.MethodPut, totpSetupUrl(), rq, &resp)
	return resp, err
}

// TOTPLogin does a login using TOTP as the second factor.
func (c *Client) TOTPLogin(user, pass, code string) (types.LoginResponse, error) {
	return c.MFALogin(user, pass, types.AUTH_TYPE_TOTP, code)
}

// TOTPClear deletes the user's TOTP setup.
// Note that this may return an error if another MFA method is not configured.
func (c *Client) TOTPClear(user, pass, code string) error {
	rq := types.MFAAuthRequest{
		User:     user,
		Pass:     pass,
		AuthType: types.AUTH_TYPE_TOTP,
		AuthCode: code,
	}
	err := c.methodStaticPushURL(http.MethodPost, totpClearUrl(), rq, nil)
	return err
}

// GetMFAInfo returns information about the system's MFA policies and
// the user's MFA setup.
func (c *Client) GetMFAInfo() (resp types.MFAInfo, err error) {
	err = c.getStaticURL(mfaUrl(), &resp)
	return
}

// AdminClearUserMFA completely clears the specified user's MFA
// configuration. They will have to re-configure MFA on their next
// login.
func (c *Client) AdminClearUserMFA(uid int32) error {
	return c.methodStaticParamURL(http.MethodDelete, clearUserMFAUrl(uid), nil, nil)
}
