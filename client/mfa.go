package client

import "github.com/gravwell/gravwell/v3/client/types"
import "net/http"

func (c *Client) GetTOTPSetup(user, pass string) (types.MFATOTPSetupResponse, error) {
	rq := types.MFAAuthRequest{
		User: user,
		Pass: pass,
	}
	var resp types.MFATOTPSetupResponse
	err := c.postStaticURL(totpSetupUrl(), rq, &resp)
	return resp, err
}

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

func (c *Client) TOTPLogin(user, pass, code string) (types.LoginResponse, error) {
	return c.MFALogin(user, pass, types.AUTH_TYPE_TOTP, code)
}
