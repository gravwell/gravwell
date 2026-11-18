/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package okta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/go-jose/go-jose/v4"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/okta/okta-sdk-golang/v6/okta"
	goCache "github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"
)

const (
	tokenPath = `/oauth2/v1/token`
	userAgent = `gravwell-hosted-okta/` + Version
)

var (
	oauthScopes = []string{`okta.logs.read`, `okta.users.read`}
	authParamRx = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^\s,]+))`)
)

// oauthState holds the OAuth 2.0 service app (private_key_jwt) state, it is unused with an SSWS Token
type oauthState struct {
	signer    jose.Signer       // signs the client assertions
	oauthBase http.RoundTripper // base transport, nil uses a clone of http.DefaultTransport
}

// loadPrivateKey builds the client assertion signer. The key is handed to the SDK as a signer,
// given a string the SDK would first stat it as a possible file path.
func (o *OktaIngester) loadPrivateKey() error {
	key, kid, err := o.cfg.privateKey() // already parsed by Verify
	if err != nil {
		return err
	}
	if o.cfg.Private_Key_ID != `` {
		kid = o.cfg.Private_Key_ID
	}
	var so *jose.SignerOptions
	if kid != `` {
		so = (&jose.SignerOptions{}).WithHeader(`kid`, kid)
	}
	if o.signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, so); err != nil {
		return fmt.Errorf("failed to build client assertion signer %w", err)
	}
	return nil
}

// newOAuthDoer builds the doer the fetch loops use in OAuth mode. The SDK is used for token
// acquisition only (client assertion, DPoP handshake, token cache).
func (o *OktaIngester) newOAuthDoer(ctx context.Context, rl *rate.Limiter, lg hosted.Logger) *oauthDoer {
	base := o.oauthBase
	if base == nil {
		base = http.DefaultTransport
		if dt, ok := base.(*http.Transport); ok {
			base = dt.Clone()
		}
	}
	// no Client.Timeout on the token client, it would also cover the rate limiter wait
	tokens := &tokenTransport{lg: lg, next: &rateLimitTransport{ctx: ctx, rl: rl, timeout: defaultRequestTimeout, next: base}}
	return &oauthDoer{
		ctx:     ctx,
		rl:      rl,
		backoff: httpBackoff,
		origin:  o.baseURL,
		cli:     &http.Client{Timeout: httpTimeout, Transport: base},
		tokens:  tokens,
		auth: okta.PrivateKeyAuthConfig{
			TokenCache:       goCache.New(5*time.Minute, 0), // the SDK's default, tokens carry their own expiry; no janitor
			HttpClient:       &http.Client{Transport: tokens},
			PrivateKeySigner: o.signer,
			ClientId:         o.cfg.Client_ID,
			OrgURL:           o.baseURL,
			UserAgent:        userAgent,
			Scopes:           oauthScopes,
		},
	}
}

// oauthDoer is the doer for OAuth mode. Like utils.RetryHttpClient it rate limits every attempt and
// retries in place, but it authorizes each attempt anew: a DPoP proof is single use, and a token that
// Okta rejects is replaced rather than sent again. Requests must not carry a body.
type oauthDoer struct {
	ctx     context.Context
	rl      *rate.Limiter
	backoff time.Duration
	origin  string       // scheme://host of the org, credentials go nowhere else
	cli     *http.Client // API requests
	tokens  *tokenTransport

	mtx  sync.Mutex // one token is shared by both routines
	auth okta.PrivateKeyAuthConfig
}

func (d *oauthDoer) Do(req *http.Request) (*http.Response, error) {
	var reauthed bool
	for {
		if err := d.rl.Wait(d.ctx); err != nil {
			return nil, err
		}
		areq := req.Clone(d.ctx)
		var resp *http.Response
		retry, err := d.authorize(areq)
		if err == nil {
			// outside the authorization, so a token request does not eat into the API timeout
			resp, err = d.cli.Do(areq)
			retry = true // as in Token mode, transport errors are retried until shutdown
		}
		switch {
		case err != nil:
			if !retry || d.ctx.Err() != nil {
				return nil, err
			}
		case retryStatus(resp.StatusCode):
			utils.DrainResponse(resp)
		case resp.StatusCode == http.StatusUnauthorized && !reauthed:
			// the token was revoked or expired early, try once more with a new one
			utils.DrainResponse(resp)
			d.dropToken()
			reauthed = true
			continue
		default:
			// a token keeps the scopes it was issued with, drop it so a later grant takes effect
			if resp.StatusCode == http.StatusUnauthorized || insufficientScope(resp) {
				d.dropToken()
			}
			return resp, nil
		}
		if utils.QuitableSleep(d.ctx, d.backoff) {
			return nil, d.ctx.Err()
		}
	}
}

// authorize adds the access token, and the DPoP proof if the org asks for one, fetching a token
// when none is cached. retry reports whether a failure is worth another attempt.
func (d *oauthDoer) authorize(req *http.Request) (retry bool, err error) {
	// the system log cursor is an absolute URL from a Link header or the state store
	if origin := req.URL.Scheme + `://` + req.URL.Host; !strings.EqualFold(origin, d.origin) {
		return false, fmt.Errorf("refusing to send okta credentials to %s", origin)
	}
	htu := *req.URL
	htu.RawQuery, htu.Fragment, htu.RawFragment = ``, ``, ``
	req.Header.Del(`Authorization`)
	req.Header.Set(`User-Agent`, userAgent)

	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.tokens.failed = nil
	cfg := d.auth
	cfg.Req = req
	if err = okta.NewPrivateKeyAuth(cfg).Authorize(req.Method, htu.String()); err == nil {
		return false, nil
	}
	// the SDK reports a refused token request only as an empty token
	if te := d.tokens.failed; te != nil {
		return retryStatus(te.status), te
	}
	var ue *url.Error
	return errors.As(err, &ue), fmt.Errorf("okta token request failed: %w", err)
}

func (d *oauthDoer) dropToken() {
	d.mtx.Lock()
	d.auth.TokenCache.Flush()
	d.mtx.Unlock()
}

// retryStatus mirrors utils.RetryHttpClient: server errors and the Token mode retry codes
func retryStatus(status int) bool {
	return status >= 500 || slices.Contains(httpRetryCodes, status)
}

// apiError is a non-200 response from the Okta API
type apiError struct {
	status int
	code   string // OAuth error from WWW-Authenticate, e.g. insufficient_scope
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func isStatus(err error, status int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.status == status
}

func isInsufficientScope(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.status == http.StatusForbidden && ae.code == `insufficient_scope`
}

func authParams(resp *http.Response) map[string]string {
	params := map[string]string{}
	for _, m := range authParamRx.FindAllStringSubmatch(resp.Header.Get(`WWW-Authenticate`), -1) {
		params[m[1]] = m[2] + m[3]
	}
	return params
}

func insufficientScope(resp *http.Response) bool {
	return resp.StatusCode == http.StatusForbidden && authParams(resp)[`error`] == `insufficient_scope`
}

// newAPIError describes a failed response. Okta puts the reason for scope failures only in the
// WWW-Authenticate header, the body is empty.
func newAPIError(resp *http.Response) *apiError {
	params := authParams(resp)
	e := &apiError{status: resp.StatusCode, code: params[`error`], msg: `okta returned ` + resp.Status}

	var body struct {
		ErrorSummary string `json:"errorSummary"`
	}
	if b, _ := io.ReadAll(resp.Body); json.Unmarshal(b, &body) == nil && body.ErrorSummary != `` {
		e.msg += `: ` + body.ErrorSummary
	} else if e.code != `` {
		e.msg += `: ` + e.code + `: ` + params[`error_description`]
	}
	if e.status == http.StatusForbidden {
		e.msg += `; check that the service app is assigned the Read-only Administrator role and granted the okta.logs.read and okta.users.read scopes`
	}
	return e
}

// rateLimitTransport holds token requests to the configured rate, API requests are limited by the oauthDoer.
// The request timeout starts once the limiter admits the request and runs until the body is closed.
type rateLimitTransport struct {
	ctx     context.Context // Run context, the SDK builds its token requests on context.Background()
	rl      *rate.Limiter
	timeout time.Duration
	next    http.RoundTripper
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// a shutdown cancels the request as well as its own context
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	release := func() {
		stop()
		cancel()
	}
	if err := t.rl.Wait(ctx); err != nil {
		release()
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}
	tctx, cancelTimeout := context.WithTimeout(ctx, t.timeout)
	resp, err := t.next.RoundTrip(req.WithContext(tctx))
	if err != nil {
		cancelTimeout()
		release()
		return nil, err
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: func() {
		cancelTimeout()
		release()
	}}
	return resp, nil
}

// cancelBody releases the request timeout when the response body is closed
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// tokenError is a token request the token endpoint refused
type tokenError struct {
	status     int
	code, desc string
}

func (e *tokenError) Error() string {
	msg := fmt.Sprintf("okta token request failed with status %d", e.status)
	if e.code != `` {
		msg += `: ` + e.code
	}
	if e.desc != `` {
		msg += `: ` + e.desc
	}
	return msg
}

// tokenTransport logs token endpoint failures and missing scopes, the SDK reports neither.
// It never retries, a replayed request would replay its DPoP proof, and never logs a token or assertion.
type tokenTransport struct {
	lg     hosted.Logger
	next   http.RoundTripper
	failed *tokenError // the last refused token request, guarded by oauthDoer.mtx
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || req.URL.Path != tokenPath {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var tr struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Scope       string `json:"scope"`
	}
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		kvs := []rfc5424.SDParam{log.KV("status", resp.StatusCode), log.KV("error", tr.Error), log.KV("description", tr.Description)}
		// requesting a proof, then a nonce, is the normal DPoP handshake
		if tr.Error == `use_dpop_nonce` || (tr.Error == `invalid_dpop_proof` && req.Header.Get(`DPoP`) == ``) {
			t.lg.Debug("okta token endpoint requested DPoP", kvs...)
		} else {
			t.failed = &tokenError{status: resp.StatusCode, code: tr.Error, desc: tr.Description}
			t.lg.Error("okta token request failed", kvs...)
		}
		return resp, nil
	}
	granted := strings.Fields(tr.Scope)
	for _, s := range oauthScopes {
		if !slices.Contains(granted, s) {
			t.lg.Warn("okta access token is missing a scope, grant it on the service app's Okta API Scopes tab", log.KV("scope", s))
		}
	}
	return resp, nil
}
