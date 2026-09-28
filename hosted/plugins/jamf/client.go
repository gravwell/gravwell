/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package jamf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"golang.org/x/time/rate"
)

// tokenExpiryBuffer forces a refresh a bit before the token would actually
// expire, so a slow request doesn't get rejected mid-flight.
const tokenExpiryBuffer = 30 * time.Second

// Response mirrors the envelope returned by the computers-inventory endpoint.
type Response struct {
	TotalCount int               `json:"totalCount"`
	Results    []json.RawMessage `json:"results"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Client wraps authenticated access to a Jamf Pro server: it handles
// acquiring and refreshing an OAuth client-credentials bearer token, and
// fetching pages of the computers-inventory endpoint.
type Client struct {
	host         string
	clientID     string
	clientSecret string
	http         *utils.RetryHttpClient

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewClient builds a Jamf API client. ctx is used to bound retry/backoff
// behavior on the underlying HTTP client for the lifetime of the plugin.
func NewClient(ctx context.Context, host, clientID, clientSecret string, requestsPerMinute int) *Client {
	limiter := rate.NewLimiter(rate.Every(time.Minute/time.Duration(requestsPerMinute)), requestsPerMinute)
	return &Client{
		host:         host,
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         utils.NewRetryHttpClient(limiter, 30*time.Second, 5*time.Second, ctx, nil),
	}
}

// token returns a valid bearer token, fetching or refreshing one against
// /api/oauth/token as needed.
func (c *Client) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting token: %w", err)
	}
	defer utils.DrainResponse(resp)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed: %s", resp.Status)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	c.token = tr.AccessToken
	c.expires = time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second - tokenExpiryBuffer)

	return c.token, nil
}

// FetchInventoryPage requests a single page of computers-inventory records
// matching filter, for the given sections.
func (c *Client) FetchInventoryPage(ctx context.Context, filter string, sections []string, page, pageSize int) (*Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	q := url.Values{}
	for _, s := range sections {
		q.Add("section", s)
	}
	q.Set("filter", filter)
	q.Set("page", strconv.Itoa(page))
	q.Set("page-size", strconv.Itoa(pageSize))

	reqURL := c.host + "/api/v1/computers-inventory?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building inventory request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting inventory: %w", err)
	}
	defer utils.DrainResponse(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("inventory request failed: %s: %s", resp.Status, string(body))
	}

	var r Response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decoding inventory response: %w", err)
	}
	return &r, nil
}
