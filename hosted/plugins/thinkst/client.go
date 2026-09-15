/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package thinkst

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	incidentsPath  = "/api/v1/incidents/all"
	auditTrailPath = "/api/v1/audit_trail/fetch"

	// TimeFormat is the timestamp layout used by the Thinkst Canary APIs.
	TimeFormat = "2006-01-02 15:04:05 MST-0700"

	// maxReadAttempts bounds how many times a single request+response-body
	// read is retried locally. This is distinct from (and on top of) the
	// production HTTP client's own retry-on-recoverable-status behavior:
	// that layer only guards the Do() call and returns as soon as it sees a
	// 200, so it cannot protect against the connection failing while the
	// body is being read afterward (e.g. an HTTP/2 GOAWAY arriving
	// mid-stream). That's exactly the gap this closes.
	maxReadAttempts = 3
)

// readRetryDelay is a var (not a const) so tests can shrink it.
var readRetryDelay = 2 * time.Second

// doer is satisfied by both *http.Client and utils.RetryHttpClient, and lets
// tests substitute an httptest server's client without a real rate limiter.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// incidentsResponse is a minimal decode of the incidents API response: only
// the fields needed to paginate and extract per-record metadata. The full
// record is passed through to the indexer unmodified as json.RawMessage.
type incidentsResponse struct {
	Cursor struct {
		Next     any `json:"next"`
		NextLink any `json:"next_link"`
	} `json:"cursor"`
	Incidents []json.RawMessage `json:"incidents"`
}

// incidentMeta is decoded per-incident just to extract the timestamp and the
// monotonic update ID used for the incidents_since cursor.
type incidentMeta struct {
	UpdatedID  int    `json:"updated_id"`
	UpdatedStd string `json:"updated_std"`
}

// auditTrailResponse is a minimal decode of the audit trail API response.
type auditTrailResponse struct {
	Cursor struct {
		Next any `json:"next"`
	} `json:"cursor"`
	AuditTrail []json.RawMessage `json:"audit_trail"`
}

// auditMeta is decoded per-record just to extract the timestamp.
type auditMeta struct {
	Timestamp string `json:"timestamp"`
}

// Client wraps HTTP access to the Thinkst Canary APIs.
type Client struct {
	host  string
	token string
	c     doer
}

// NewClient builds a Client. host may be a bare domain (e.g.
// "example.canary.tools") or a full base URL including scheme (used in
// tests to point at an httptest server); a bare domain is assumed to be
// HTTPS.
func NewClient(host, token string, c doer) *Client {
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	return &Client{host: host, token: token, c: c}
}

// GetIncidents fetches a single page of incidents. sinceID is the
// incidents_since cursor used to start a fresh backlog; it is ignored once
// cursor (a pagination token from a previous page) is non-empty.
//
// limit is only sent on the initial request. The Canary API documents this
// restriction explicitly for the audit trail endpoint ("limit ... Cannot be
// used with a cursor") and every pagination example for both endpoints
// follows the same pattern, so it's applied here uniformly: sending both
// together is rejected with a 400.
func (c *Client) GetIncidents(ctx context.Context, sinceID, cursor string) (*incidentsResponse, error) {
	data := url.Values{}
	if cursor != "" {
		data.Set("cursor", cursor)
	} else {
		if sinceID != "" {
			data.Set("incidents_since", sinceID)
		}
		data.Set("limit", "100")
	}
	data.Set("auth_token", c.token)

	endpoint := fmt.Sprintf("%s%s?%s", c.host, incidentsPath, data.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error building incidents request: %w", err)
	}
	req.Header.Set("accept", "application/json")

	var resp incidentsResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("error fetching incidents: %w", err)
	}
	return &resp, nil
}

// GetAuditTrail fetches a single page of audit trail records. limit is only
// sent when there is no cursor (see GetIncidents for why).
func (c *Client) GetAuditTrail(ctx context.Context, cursor string) (*auditTrailResponse, error) {
	data := url.Values{}
	if cursor != "" {
		data.Set("cursor", cursor)
	} else {
		data.Set("limit", "100")
	}
	data.Set("auth_token", c.token)

	endpoint := fmt.Sprintf("%s%s?%s", c.host, auditTrailPath, data.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error building audit trail request: %w", err)
	}
	req.Header.Set("accept", "application/json")

	var resp auditTrailResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("error fetching audit trail: %w", err)
	}
	return &resp, nil
}

// doJSON performs req and unmarshals its JSON body into out. It reissues the
// same request (GET, nil body, so safe to resend as-is) up to
// maxReadAttempts times if the transport errors or the body fails to read
// after a successful status; a non-200 status is returned immediately
// without retrying, since that's a real rejection rather than a transient
// connection problem.
func (c *Client) doJSON(req *http.Request, out any) error {
	var lastErr error
	for attempt := 0; attempt < maxReadAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return req.Context().Err()
			case <-time.After(readRetryDelay):
			}
		}

		resp, err := c.c.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("error making request: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("invalid status code %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("error reading response body: %w", err)
			continue
		}

		return json.Unmarshal(body, out)
	}
	return lastErr
}

// parseInto unmarshals a raw JSON record into v. Used to pull just the
// metadata needed for pagination/timestamps out of an otherwise-opaque record.
func parseInto(raw json.RawMessage, v any) error {
	return json.Unmarshal(raw, v)
}
