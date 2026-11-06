/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	auditPath              = "/audit/v1/logs"
	pageSize               = 1000
	minimumPageSize        = 1
	httpTimeout            = 30 * time.Second
	maximumRetrySleep      = 5 * time.Minute
	defaultRetryDelay      = 30 * time.Second
	maximumAuditRecordSize = 8 << 20
	maximumAuditPageSize   = 16 << 20
)

var errInvalidCursor = errors.New("Slack rejected an expired pagination cursor")

type page struct {
	records    []json.RawMessage
	nextCursor string
}

// oversizedPageError reports a successful Slack response whose body exceeded the
// response ceiling for one requested record count. It is internal to the client:
// fetch either resolves it by requesting fewer records or surfaces it once no
// smaller request is possible.
type oversizedPageError struct {
	limit int
}

func (e *oversizedPageError) Error() string {
	return fmt.Sprintf("Slack Audit response exceeds %d bytes at a requested limit of %d records", maximumAuditPageSize, e.limit)
}

type retryDeferral struct {
	delay   time.Duration
	retryAt time.Time
}

func (e *retryDeferral) Error() string { return "Slack requested a deferred retry" }

type client struct {
	endpoint *url.URL
	token    string
	http     *http.Client
	limiter  *rate.Limiter
	now      func() time.Time

	// limit is the record count requested from Slack. It starts at pageSize and
	// only ever shrinks, so a workspace that answers a full page with a body over
	// the response ceiling does not have to re-probe that ceiling on every page.
	limit int
}

func newClient(host, token string, requestsPerMinute int, hc *http.Client) (*client, error) {
	base, err := url.Parse(host)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("invalid Slack host")
	}
	endpoint, err := base.Parse(auditPath)
	if err != nil || endpoint.Scheme != base.Scheme || !strings.EqualFold(endpoint.Host, base.Host) || endpoint.User != nil {
		return nil, errors.New("Slack Audit endpoint escaped the configured origin")
	}
	if requestsPerMinute < 1 {
		return nil, errors.New("Slack request rate must be positive")
	}
	if hc == nil {
		hc = &http.Client{Timeout: httpTimeout}
	}
	copyHTTP := *hc
	copyHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{
		endpoint: endpoint,
		token:    token,
		http:     &copyHTTP,
		limiter:  rate.NewLimiter(rate.Every(time.Minute/time.Duration(requestsPerMinute)), 1),
		now:      time.Now,
		limit:    pageSize,
	}, nil
}

// fetch requests one page of audit records for the active window and cursor.
//
// Slack decides how many records a requested limit returns and an individual
// accepted record may be up to maximumAuditRecordSize, so a valid full page can
// answer with a body above maximumAuditPageSize. Rather than repeating one
// unreadable request forever, an oversized successful response is retried at the
// same cursor with progressively fewer requested records, down to a single
// record. Every attempt passes through the request rate limiter, and the reduced
// limit is retained for subsequent requests. One record that is still oversized
// on its own cannot be made to fit, so it is returned as a visible error instead
// of being silently dropped.
func (c *client) fetch(ctx context.Context, oldest, latest int64, cursor string) (page, error) {
	if c == nil || c.endpoint == nil || c.http == nil || c.limiter == nil {
		return page{}, errors.New("Slack client is not initialized")
	}
	limit := c.limit
	if limit < minimumPageSize || limit > pageSize {
		limit = pageSize
	}
	for {
		result, err := c.fetchPage(ctx, oldest, latest, cursor, limit)
		var oversized *oversizedPageError
		if !errors.As(err, &oversized) {
			return result, err
		}
		if limit <= minimumPageSize {
			return page{}, err
		}
		// Halving reaches minimumPageSize in a bounded number of attempts and
		// every attempt strictly shrinks the request, so this cannot spin.
		limit = max(minimumPageSize, limit/2)
		c.limit = limit
	}
}

func (c *client) fetchPage(ctx context.Context, oldest, latest int64, cursor string, limit int) (page, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return page{}, err
	}
	u := *c.endpoint
	query := make(url.Values)
	query.Set("oldest", strconv.FormatInt(oldest, 10))
	query.Set("latest", strconv.FormatInt(latest, 10))
	query.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return page{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		receivedAt := c.now().UTC()
		delay := retryDelay(resp.Header.Get("Retry-After"), receivedAt)
		return page{}, &retryDeferral{delay: delay, retryAt: receivedAt.Add(delay)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope)
		if envelope.Error == "invalid_cursor" {
			return page{}, errInvalidCursor
		}
		return page{}, fmt.Errorf("Slack Audit request failed with HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		OK               *bool           `json:"ok"`
		Error            string          `json:"error"`
		Entries          json.RawMessage `json:"entries"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maximumAuditPageSize+1))
	if err != nil {
		return page{}, fmt.Errorf("read Slack Audit response: %w", err)
	}
	if len(body) > maximumAuditPageSize {
		return page{}, &oversizedPageError{limit: limit}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return page{}, fmt.Errorf("decode Slack Audit response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return page{}, errors.New("Slack Audit response contains multiple JSON values")
		}
		return page{}, fmt.Errorf("decode Slack Audit response trailer: %w", err)
	}
	if envelope.Error == "invalid_cursor" {
		return page{}, errInvalidCursor
	}
	if envelope.Error != "" || (envelope.OK != nil && !*envelope.OK) {
		return page{}, errors.New("Slack Audit API returned an application error")
	}
	if len(envelope.Entries) == 0 || string(envelope.Entries) == "null" {
		return page{}, errors.New("Slack Audit response is missing entries")
	}
	var records []json.RawMessage
	if err := json.Unmarshal(envelope.Entries, &records); err != nil {
		return page{}, errors.New("Slack Audit entries is not an array")
	}
	return page{records: records, nextCursor: strings.TrimSpace(envelope.ResponseMetadata.NextCursor)}, nil
}

// retryDelay converts an untrusted Retry-After header into a deferral this
// runtime can actually honor. Slack controls the header, so the value is clamped
// before any duration conversion or persistence: the plugin re-examines a
// persisted deadline at most maximumRetrySleep apart, so a larger deadline would
// only be woken on that same cadence and never cleared, and a deadline past the
// largest representable instant could not be persisted at all. A zero, negative,
// past, or malformed value falls back to defaultRetryDelay under the same
// ceiling.
func retryDelay(raw string, now time.Time) time.Duration {
	maximumDelay := min(maximumRetrySleep, maximumPersistableRetryDelay(now))
	trimmed := strings.TrimSpace(raw)
	if seconds, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
		if seconds == 0 {
			return min(defaultRetryDelay, maximumDelay)
		}
		if seconds > uint64(maximumDelay/time.Second) {
			return maximumDelay
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(trimmed); err == nil {
		delay := when.Sub(now)
		if delay <= 0 {
			return min(defaultRetryDelay, maximumDelay)
		}
		return min(delay, maximumDelay)
	}
	return min(defaultRetryDelay, maximumDelay)
}

func maximumPersistableRetryDelay(now time.Time) time.Duration {
	maximumRetryAt := time.Unix(0, math.MaxInt64).UTC()
	if !now.Before(maximumRetryAt) {
		return 0
	}
	return maximumRetryAt.Sub(now)
}
