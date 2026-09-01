/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package wiz

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/utils/jsoncompat"
)

var (
	// ErrAuthenticationFailure is returned when we cannot obtain or use an
	// OAuth token even after refreshing it.
	ErrAuthenticationFailure = errors.New("authentication failure")
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// AuthToken mirrors the OAuth client_credentials response from Wiz.
type AuthToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpireIn    int64     `json:"expires_in"`
	Scope       string    `json:"scope"`
	ExpireAt    time.Time `json:"-"`
}

func (t AuthToken) valid() bool {
	return t.AccessToken != "" && t.ExpireAt.After(time.Now())
}

// Client is a thin GraphQL client for the Wiz API. It manages an OAuth
// client_credentials token and transparently refreshes it whenever a request
// comes back unauthenticated.
type Client struct {
	endpoint string
	authURL  string
	audience string
	id       string
	secret   string
	c        doer

	mtx   sync.RWMutex
	token AuthToken
}

func NewClient(endpoint, authURL, audience, id, secret string, c doer) *Client {
	return &Client{
		endpoint: endpoint,
		authURL:  authURL,
		audience: audience,
		id:       id,
		secret:   secret,
		c:        c,
	}
}

// authenticate exchanges the client id/secret for a bearer token. When force
// is false a still-valid token short circuits the request; when force is true
// (used after an unauthenticated response) a fresh token is always requested.
func (c *Client) authenticate(ctx context.Context, force bool) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if !force && c.token.valid() {
		return nil
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.id)
	data.Set("client_secret", c.secret)
	data.Set("audience", c.audience)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("%w, failed to build auth request: %w", ErrAuthenticationFailure, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	res, err := c.c.Do(req)
	if err != nil {
		return fmt.Errorf("%w, request failed: %w", ErrAuthenticationFailure, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("%w, got status %d: %s", ErrAuthenticationFailure, res.StatusCode, strings.TrimSpace(string(body)))
	}

	token, err := parse[AuthToken](res.Body)
	if err != nil {
		return fmt.Errorf("%w, failed to parse auth response: %w", ErrAuthenticationFailure, err)
	}
	if token.AccessToken == "" {
		return fmt.Errorf("%w, auth response contained no access token", ErrAuthenticationFailure)
	}
	// expire 'early' so we don't race a request against the real expiry.
	token.ExpireAt = time.Now().Add(time.Duration(token.ExpireIn)*time.Second - 5*time.Minute)
	c.token = *token
	return nil
}

func (c *Client) bearer() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.token.AccessToken
}

// GraphQLError is a single error entry from a GraphQL response.
type GraphQLError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
	} `json:"extensions"`
}

func (e GraphQLError) unauthenticated() bool {
	if strings.EqualFold(e.Extensions.Code, "UNAUTHENTICATED") {
		return true
	}
	msg := strings.ToLower(e.Message)
	return strings.Contains(msg, "unauthenticated") || strings.Contains(msg, "unauthorized")
}

func (e GraphQLError) accessDenied() bool {
	switch strings.ToUpper(e.Extensions.Code) {
	case "FORBIDDEN", "ACCESS_DENIED", "DENIED":
		return true
	}
	return strings.Contains(strings.ToLower(e.Message), "access denied")
}

func (e GraphQLError) internalError() bool {
	switch strings.ToUpper(e.Extensions.Code) {
	case "INTERNAL_SERVER_ERROR", "INTERNAL_ERROR", "INTERNAL":
		return true
	}
	return strings.Contains(strings.ToLower(e.Message), "internal error")
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   jsontext.Value `json:"data"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

// Query executes a GraphQL query and unmarshals the returned data into out. If
// the request comes back unauthenticated the OAuth token is refreshed and the
// request is retried exactly once.
func (c *Client) Query(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: vars}, jsoncompat.Options)
	if err != nil {
		return fmt.Errorf("failed to marshal graphql request: %w", err)
	}

	// attempt 0 uses the cached token; attempt 1 forces a refresh after an
	// unauthenticated response.
	for attempt := range 2 {
		if err = c.authenticate(ctx, attempt == 1); err != nil {
			return err
		}

		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if rerr != nil {
			return fmt.Errorf("failed to build graphql request: %w", rerr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.bearer())

		resp, rerr := c.c.Do(req)
		if rerr != nil {
			return fmt.Errorf("graphql request failed: %w", rerr)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			drain(resp)
			if attempt == 0 {
				continue // refresh token and retry
			}
			return fmt.Errorf("%w, graphql request returned 401", ErrAuthenticationFailure)
		}

		payload, perr := parse[graphQLResponse](resp.Body)
		drain(resp)
		if perr != nil {
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("graphql request returned status %d", resp.StatusCode)
			}
			return fmt.Errorf("failed to parse graphql response: %w", perr)
		}

		if gerr := payload.errorState(); gerr != nil {
			if gerr == errUnauthenticated && attempt == 0 {
				continue // refresh token and retry
			}
			if gerr == errUnauthenticated {
				return fmt.Errorf("%w, graphql request unauthenticated", ErrAuthenticationFailure)
			}
			return gerr
		}

		if out != nil {
			if err = json.Unmarshal(payload.Data, out, jsoncompat.Options); err != nil {
				return fmt.Errorf("failed to unmarshal graphql data: %w", err)
			}
		}
		return nil
	}
	return ErrAuthenticationFailure
}

var (
	errUnauthenticated = errors.New("unauthenticated")
	// ErrAccessDenied indicates the credentials cannot access a queried field.
	// The specific error payload is intentionally dropped by the classifier.
	ErrAccessDenied = errors.New("access denied")
	// ErrInternal indicates the Wiz API returned an internal server error. The
	// specific error payload is intentionally dropped by the classifier.
	ErrInternal = errors.New("internal error")
)

// errorState inspects the GraphQL errors array and classifies it. Auth,
// access-denied, and internal errors are collapsed to sentinel errors that omit
// the (often large and noisy) upstream payload; anything else is returned as a
// combined message. Returns nil when there are no errors.
func (r *graphQLResponse) errorState() error {
	if len(r.Errors) == 0 {
		return nil
	}
	var accessDenied, internal bool
	msgs := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		switch {
		case e.unauthenticated():
			return errUnauthenticated
		case e.accessDenied():
			accessDenied = true
		case e.internalError():
			internal = true
		}
		msgs = append(msgs, e.Message)
	}
	switch {
	case accessDenied:
		return ErrAccessDenied
	case internal:
		return ErrInternal
	default:
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}
}

// parse reads and unmarshals bytes into a type T. Intended for http response bodies.
func parse[T any](rc io.Reader) (*T, error) {
	t := new(T)
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, t, jsoncompat.Options); err != nil {
		return nil, err
	}
	return t, nil
}

func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
