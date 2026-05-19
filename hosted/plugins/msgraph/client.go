/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package msgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	AlertsEndpoint          = "/v1.0/security/alerts_v2"
	SecureScoresEndpoint    = "/v1.0/security/secureScores"
	ControlProfilesEndpoint = "/v1.0/security/secureScoreControlProfiles"
	tokenEndpoint           = "/oauth2/v2.0/token"
	graphScope              = "https://graph.microsoft.com/.default"
	authRequestTimeout      = 10 * time.Second
)

var (
	ErrAuthentication = errors.New("authentication failure")
)

// Doer is the interface for performing HTTP requests.
// Allows injection of rate-limited/test clients.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client wraps authenticated HTTP requests to the MS Graph Security API.
type Client struct {
	graphHost    string
	authHost     string
	tenantID     string
	clientID     string
	clientSecret string
	httpClient   Doer
	token        AuthToken

	mu sync.RWMutex
}

func NewClient(graphHost, authHost, tenantID, clientID, clientSecret string, httpClient Doer) *Client {
	return &Client{
		graphHost:    graphHost,
		authHost:     authHost,
		tenantID:     tenantID,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

func (c *Client) authenticate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token.expiresAt.After(time.Now()) {
		return nil
	}

	data := url.Values{}
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("scope", graphScope)
	data.Set("grant_type", "client_credentials")

	endpoint := fmt.Sprintf("%s/%s%s", c.authHost, c.tenantID, tokenEndpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: request failed: %w", ErrAuthentication, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		var authErr AuthErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&authErr); err != nil {
			return fmt.Errorf("%w: status %d, decode error response body: %w", ErrAuthentication, resp.StatusCode, err)
		}
		return fmt.Errorf("%w: status %d, [%s] %s", ErrAuthentication, resp.StatusCode, authErr.Error, authErr.ErrorDescription)
	}

	var token AuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return fmt.Errorf("%w: failed to decode token response: %w", ErrAuthentication, err)
	}

	token.expiresAt = time.Now().Add(time.Duration(token.ExpiresIn)*time.Second - 5*time.Minute)
	c.token = token

	return nil
}

// Do performs an authenticated request to the Graph API.
func (c *Client) Do(r *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(r.Context(), authRequestTimeout)
	err := c.authenticate(ctx)
	cancel()
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	r.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	c.mu.RUnlock()
	r.Header.Set("Accept", "application/json")
	return c.httpClient.Do(r)
}

// List performs a GET request and returns one page of results.
// If nextLink is not empty, it is used as the full URL for pagination.
func (c *Client) List(ctx context.Context, endpoint string, params url.Values, nextLink string) (*ODataResponse, error) {
	var reqURL string
	if nextLink != "" {
		reqURL = nextLink
	} else {
		sb := strings.Builder{}
		sb.WriteString(c.graphHost + endpoint)
		if len(params) > 0 {
			sb.WriteString("?" + params.Encode())
		}
		reqURL = sb.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("make request: %w", err)
	}
	defer utils.DrainResponse(resp)

	if resp.StatusCode != http.StatusOK {
		var graphErr GraphErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&graphErr); err != nil {
			return nil, fmt.Errorf("status %d, decode error response body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("status %d, [%s] %s", resp.StatusCode, graphErr.Error.Code, graphErr.Error.Message)
	}

	var result ODataResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// ListAll consumes all pages for a given endpoint and returns every item.
func (c *Client) ListAll(ctx context.Context, endpoint string, params url.Values) ([]json.RawMessage, error) {
	var all []json.RawMessage
	var nextLink string
	for {
		resp, err := c.List(ctx, endpoint, params, nextLink)
		if err != nil {
			return all, err
		}
		all = append(all, resp.Value...)
		if resp.NextLink == "" {
			break
		}
		nextLink = resp.NextLink
	}
	return all, nil
}
