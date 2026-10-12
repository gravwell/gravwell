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
	httpTimeout            = 30 * time.Second
	maximumRetryDelay      = 5 * time.Minute
	defaultRetryDelay      = 30 * time.Second
	maximumAuditRecordSize = 8 << 20
	maximumAuditPageSize   = 16 << 20
)

var errInvalidCursor = errors.New("Slack rejected an expired pagination cursor")

type page struct {
	records    []json.RawMessage
	nextCursor string
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
	}, nil
}

func (c *client) fetch(ctx context.Context, oldest, latest int64, cursor string) (page, error) {
	if c == nil || c.endpoint == nil || c.http == nil || c.limiter == nil {
		return page{}, errors.New("Slack client is not initialized")
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return page{}, err
	}
	u := *c.endpoint
	query := make(url.Values)
	query.Set("oldest", strconv.FormatInt(oldest, 10))
	query.Set("latest", strconv.FormatInt(latest, 10))
	query.Set("limit", strconv.Itoa(pageSize))
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
		return page{}, fmt.Errorf("Slack Audit response exceeds %d bytes", maximumAuditPageSize)
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

func retryDelay(raw string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if seconds, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
		if seconds > uint64(maximumRetryDelay/time.Second) {
			return maximumRetryDelay
		}
		if seconds == 0 {
			return defaultRetryDelay
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(trimmed); err == nil {
		delay := when.Sub(now)
		if delay <= 0 {
			return defaultRetryDelay
		}
		return min(delay, maximumRetryDelay)
	}
	return defaultRetryDelay
}
