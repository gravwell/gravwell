/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	managementMaximumAttempts     = 5
	managementMaximumResponseBody = 16 << 20
	managementMaximumErrorBody    = 64 << 10
	managementMaximumRetryAfter   = 24 * time.Hour
	// managementMaximumURLLength bounds every Management Activity URL this
	// plugin dispatches or persists. The source's own feed, content and
	// NextPageUri URLs are a few hundred bytes, and a contentUri is also state:
	// one is persisted per descriptor, so an unbounded URL is an unbounded
	// descriptor document as well as an unbounded request line. 2048 is the
	// conservative interoperable URL ceiling and roughly five times the longest
	// URL the source publishes.
	managementMaximumURLLength = 2048
)

type managementResponse struct {
	Body   []byte
	Header http.Header
	Status int
}

type managementHTTPError struct {
	status int
	code   string
}

type managementDeferredError struct {
	Delay time.Duration
}

func (e *managementDeferredError) Error() string {
	return "Microsoft Management Activity retry deferred until server eligibility"
}

func (e *managementHTTPError) Error() string {
	return fmt.Sprintf("Microsoft Management Activity request failed with HTTP %d", e.status)
}

func (e *managementHTTPError) expiredContent() bool {
	return e != nil && (e.status == http.StatusNotFound || e.status == http.StatusGone || e.code == "AF20050" || e.code == "AF20051")
}

func (c *Client) managementRequest(ctx context.Context, method, endpoint string) (managementResponse, error) {
	endpoint, err := c.withManagementPublisherIdentifier(endpoint)
	if err != nil {
		return managementResponse{}, err
	}
	if err := c.validateManagementURL(endpoint); err != nil {
		return managementResponse{}, err
	}
	if err := claimFetchRequest(ctx); err != nil {
		return managementResponse{}, err
	}
	token, err := c.token(ctx, c.conf.Management_Scope)
	if err != nil {
		return managementResponse{}, err
	}
	base := c.baseHTTP
	if base == nil {
		base = &http.Client{Timeout: c.retryTimeout}
	}
	client := c.managementHTTPClient(base)
	for attempt := 1; attempt <= managementMaximumAttempts; attempt++ {
		if delay := c.managementEligibilityDelay(); delay > 0 {
			if deadline, ok := ctx.Deadline(); ok && delay > time.Until(deadline) {
				return managementResponse{}, &managementDeferredError{Delay: delay}
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return managementResponse{}, safeTransportError(ctx.Err())
			case <-timer.C:
			}
		}
		if attempt > 1 {
			if err := claimFetchRequest(ctx); err != nil {
				return managementResponse{}, err
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return managementResponse{}, safeTransportError(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
		if err != nil {
			return managementResponse{}, errors.New("invalid Microsoft Management Activity request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "gravwell-microsoft/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return managementResponse{}, safeTransportError(err)
		}
		bodyLimit := int64(managementMaximumResponseBody)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyLimit = managementMaximumErrorBody
		}
		if resp.ContentLength > bodyLimit {
			resp.Body.Close()
			return managementResponse{}, fmt.Errorf("Microsoft Management Activity response exceeds %d bytes", bodyLimit)
		}
		body, readErr := readLimitedResponse(resp.Body, bodyLimit)
		resp.Body.Close()
		if readErr != nil {
			return managementResponse{}, readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return managementResponse{Body: body, Header: resp.Header.Clone(), Status: resp.StatusCode}, nil
		}
		code := managementErrorCode(body)
		httpErr := &managementHTTPError{status: resp.StatusCode, code: code}
		if !managementRetryable(resp.StatusCode) {
			return managementResponse{}, httpErr
		}
		rawRetryAfter := resp.Header.Get("Retry-After")
		wait, err := managementRetryDelay(rawRetryAfter, c.now().UTC(), c.retryBackoff)
		if err != nil {
			return managementResponse{}, err
		}
		serverDeferred := strings.TrimSpace(rawRetryAfter) != "" && wait > 0
		if serverDeferred {
			c.deferManagement(wait)
		}
		if attempt == managementMaximumAttempts {
			if serverDeferred {
				return managementResponse{}, &managementDeferredError{Delay: c.managementEligibilityDelay()}
			}
			return managementResponse{}, httpErr
		}
		if deadline, ok := ctx.Deadline(); ok && wait > time.Until(deadline) {
			if serverDeferred {
				return managementResponse{}, &managementDeferredError{Delay: c.managementEligibilityDelay()}
			}
			return managementResponse{}, errors.New("Microsoft Management Activity retry deferred beyond request deadline")
		}
		if !serverDeferred {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return managementResponse{}, safeTransportError(ctx.Err())
			case <-timer.C:
			}
		}
	}
	return managementResponse{}, errors.New("Microsoft Management Activity retry limit reached")
}

func (c *Client) deferManagement(delay time.Duration) {
	if delay <= 0 {
		return
	}
	notBefore := c.now().UTC().Add(delay)
	c.mu.Lock()
	if notBefore.After(c.managementNotBefore) {
		c.managementNotBefore = notBefore
	}
	c.mu.Unlock()
}

func (c *Client) managementEligibilityDelay() time.Duration {
	c.mu.Lock()
	notBefore := c.managementNotBefore
	c.mu.Unlock()
	delay := notBefore.Sub(c.now().UTC())
	if delay < 0 {
		return 0
	}
	return delay
}

func readLimitedResponse(body io.Reader, limit int64) ([]byte, error) {
	if limit < 1 {
		return nil, errors.New("invalid Microsoft response limit")
	}
	limited := io.LimitReader(body, limit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, safeTransportError(err)
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("Microsoft Management Activity response exceeds %d bytes", limit)
	}
	return payload, nil
}

func managementRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func managementRetryDelay(raw string, now time.Time, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	if seconds, err := strconv.ParseUint(raw, 10, 32); err == nil {
		if seconds > uint64(managementMaximumRetryAfter/time.Second) {
			return 0, errors.New("Microsoft Management Activity Retry-After exceeds the supported bound")
		}
		return time.Duration(seconds) * time.Second, nil
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		return 0, errors.New("Microsoft Management Activity Retry-After is out of range")
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return fallback, nil
	}
	delay := when.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if delay > managementMaximumRetryAfter {
		return 0, errors.New("Microsoft Management Activity Retry-After exceeds the supported bound")
	}
	return delay, nil
}

func managementErrorCode(body []byte) string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	if envelope.Error.Code != "" {
		return envelope.Error.Code
	}
	return envelope.Code
}

func (c *Client) managementHTTPClient(base *http.Client) *http.Client {
	clone := *base
	prior := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if err := c.validateManagementURL(req.URL.String()); err != nil {
			return http.ErrUseLastResponse
		}
		endpoint, err := c.withManagementPublisherIdentifier(req.URL.String())
		if err != nil {
			return http.ErrUseLastResponse
		}
		req.URL, err = url.Parse(endpoint)
		if err != nil {
			return http.ErrUseLastResponse
		}
		if len(via) != 0 && !sameEffectiveOrigin(via[0].URL, req.URL) {
			return http.ErrUseLastResponse
		}
		if prior != nil {
			return prior(req, via)
		}
		return nil
	}
	return &clone
}

func (c *Client) withManagementPublisherIdentifier(endpoint string) (string, error) {
	publisherID := strings.TrimSpace(c.conf.Publisher_Identifier)
	if publisherID == "" {
		return "", errors.New("Microsoft Management Activity Publisher-Identifier is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("invalid Microsoft Management Activity URL")
	}
	query := u.Query()
	for key, values := range query {
		if !strings.EqualFold(key, "PublisherIdentifier") {
			continue
		}
		if key != "PublisherIdentifier" || len(values) != 1 || values[0] != publisherID {
			return "", errors.New("Microsoft Management Activity URL changed PublisherIdentifier")
		}
	}
	query.Set("PublisherIdentifier", publisherID)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// errManagementURLPath is the single answer for every path this policy refuses,
// so a rejected URL never tells a hostile response which test caught it.
var errManagementURLPath = errors.New("unexpected Microsoft Management Activity tenant or API path")

// validateManagementURL is the one policy every Management Activity URL passes
// through before it is dispatched or persisted: the initial feed URL, a listed
// contentUri, a NextPageUri continuation, and every redirect target.
//
// The bound is measured on both the URL as written and the URL as dispatched.
// managementRequest applies the mandatory PublisherIdentifier before it
// validates, which adds roughly sixty bytes, so acceptance used to measure one
// string and dispatch another: a contentUri or continuation in the band just
// below the bound was accepted here, persisted, and then refused by every
// request that followed until the window it belonged to crossed the source's
// retention floor. Re-encoding a query can shorten it as well as lengthen it,
// so neither form may cross the bound.
func (c *Client) validateManagementURL(raw string) error {
	if len(raw) > managementMaximumURLLength {
		return fmt.Errorf("Microsoft Management Activity URL exceeds %d bytes", managementMaximumURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Opaque != "" || u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, `\`) {
		return errors.New("Microsoft Management Activity URL must be absolute HTTPS")
	}
	base, err := url.Parse(c.conf.Management_Host)
	if err != nil || !sameEffectiveOrigin(base, u) {
		return errors.New("unexpected Microsoft Management Activity origin")
	}
	prefix := "/api/v1.0/" + url.PathEscape(c.conf.Tenant_ID) + "/activity/feed/"
	if err := validateManagementFeedPath(u.EscapedPath(), prefix); err != nil {
		return err
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("Microsoft Management Activity URL contains an invalid query")
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "access_token", "authorization", "client_secret", "code", "sig", "token":
			return errors.New("Microsoft Management Activity URL contains a forbidden sensitive query key")
		}
	}
	effective, err := c.withManagementPublisherIdentifier(raw)
	if err != nil {
		return err
	}
	if len(effective) > managementMaximumURLLength {
		return fmt.Errorf("Microsoft Management Activity URL exceeds %d bytes once PublisherIdentifier is applied", managementMaximumURLLength)
	}
	return nil
}

// validateManagementFeedPath accepts only a path that both starts with the
// tenant's feed prefix as written and still addresses that exact subtree after
// any canonicalization a server may apply before routing. The prefix test alone
// accepted ".../activity/feed/../../../evil" and
// ".../activity/feed/%2e%2e%2fevil": both carry the prefix literally, yet the
// first leaves the subtree as soon as dot segments are resolved and the second
// smuggles both a dot segment and a separator through a single path segment.
//
// Ambiguous forms are rejected rather than normalized. This plugin constructs
// only "<prefix><operation>" and the source publishes only opaque content
// identifiers, so a dot segment, an encoded separator or a backslash in a
// Management Activity path is never a form either side had a reason to produce.
func validateManagementFeedPath(escaped, prefix string) error {
	if !strings.HasPrefix(escaped, prefix) {
		return errManagementURLPath
	}
	for _, segment := range strings.Split(escaped, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return errManagementURLPath
		}
		// A segment whose decoded form carries a separator is an encoded
		// separator however it was spelled: %2f, %5c and their case variants. A
		// decoded form that still carries a percent is a double encoding, which
		// a server that decodes twice resolves into one of the forms below; no
		// Management Activity path has a reason to contain a literal percent.
		if strings.ContainsAny(decoded, `/\%`) || strings.Contains(segment, `\`) {
			return errManagementURLPath
		}
		switch strings.TrimSpace(decoded) {
		case ".", "..":
			return errManagementURLPath
		}
	}
	// The segment loop has already refused every dot segment, literal or
	// encoded, so cleaning the path may now only collapse duplicate separators.
	// A path that still leaves the subtree here is one the loop missed.
	if cleaned := path.Clean("/" + strings.TrimLeft(escaped, "/")); !strings.HasPrefix(cleaned+"/", prefix) {
		return errManagementURLPath
	}
	return nil
}

// streamManagementArray decodes one JSON array from body, handing each element
// to visit as it is decoded. The whole array is never materialized, so the
// count bound each caller enforces inside visit applies before the element that
// exceeds it is retained rather than after the entire array has been decoded.
// Strict trailing-data rejection is unchanged.
func streamManagementArray(body []byte, kind string, visit func(json.RawMessage) error) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return fmt.Errorf("Microsoft Management Activity %s response must be a JSON array", kind)
	}
	for decoder.More() {
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return fmt.Errorf("decode Microsoft Management Activity %s response", kind)
		}
		if err := visit(element); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode Microsoft Management Activity %s response", kind)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("Microsoft Management Activity %s response contains trailing data", kind)
	}
	return nil
}

// decodeManagementSubscriptions streams the subscription list, refusing the
// entry past the bound before it is retained.
func decodeManagementSubscriptions(body []byte, limit int) ([]managementSubscription, error) {
	subscriptions := make([]managementSubscription, 0, 8)
	err := streamManagementArray(body, "subscription list", func(element json.RawMessage) error {
		if len(subscriptions) >= limit {
			return fmt.Errorf("Microsoft Management Activity subscription list exceeds %d entries", limit)
		}
		var subscription managementSubscription
		if err := json.Unmarshal(element, &subscription); err != nil {
			return errors.New("decode Microsoft Management Activity subscription list response")
		}
		subscriptions = append(subscriptions, subscription)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return subscriptions, nil
}

// errManagementContentPageOverflow marks a content-listing page carrying more
// descriptors than the state document may hold for a whole window. The listing
// loop narrows its window and lists again, so the bound is recoverable rather
// than a page this selector can never get past.
var errManagementContentPageOverflow = errors.New("Microsoft Management Activity content page exceeds the descriptor capacity")

// decodeManagementContentPage streams one content-listing page, refusing the
// descriptor past the bound before it is retained.
func decodeManagementContentPage(body []byte, limit int) ([]managementContentWire, error) {
	wire := make([]managementContentWire, 0, 32)
	err := streamManagementArray(body, "content list", func(element json.RawMessage) error {
		if len(wire) >= limit {
			return errManagementContentPageOverflow
		}
		var item managementContentWire
		if err := json.Unmarshal(element, &item); err != nil {
			return errors.New("decode Microsoft Management Activity content list response")
		}
		wire = append(wire, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return wire, nil
}

// decodeManagementEvents streams one content blob, refusing the event past the
// bound before it is retained. The events themselves stay raw: the delivery
// loop indexes them by ordinal and compacts each one as it is written.
func decodeManagementEvents(body []byte, limit int) ([]json.RawMessage, error) {
	events := make([]json.RawMessage, 0, 64)
	err := streamManagementArray(body, "content", func(element json.RawMessage) error {
		if len(events) >= limit {
			return fmt.Errorf("Microsoft Management Activity content exceeds %d events", limit)
		}
		events = append(events, element)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return events, nil
}
