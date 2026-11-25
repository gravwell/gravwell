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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"golang.org/x/time/rate"
)

const (
	// maximumAPIResponseBody bounds one ordinary Graph/ARM/Defender/PowerBI
	// response. http.Client.Timeout bounds how long a transfer may take, not how
	// much of it is buffered, and every page is retained in memory as raw JSON
	// plus a compacted copy per record. The Management Activity path carries the
	// same bound as managementMaximumResponseBody.
	maximumAPIResponseBody = 16 << 20
	// maximumAPIErrorBody bounds the body of a failed request, which is only
	// ever drained and discarded.
	maximumAPIErrorBody = 64 << 10
	// maximumTokenResponseBody bounds an OAuth token response, which holds a
	// handful of short fields.
	maximumTokenResponseBody = 64 << 10
)

type FetchedRecord struct {
	Raw       []byte
	ID        string
	Timestamp time.Time
}

// FetchLimitError reports that Fetch returned the bounded prefix it collected
// rather than retaining more chunks in memory. Continuation is non-nil when a
// later FetchChunk call can resume after that prefix.
type FetchLimitError struct {
	Dataset      string
	Reason       string
	Records      int
	Bytes        int
	Chunks       int
	Continuation *FetchContinuation
	Cause        error
}

func (e *FetchLimitError) Error() string {
	return fmt.Sprintf("%s fetch stopped at the aggregate %s after %d records, %d bytes, and %d chunks",
		e.Dataset, e.Reason, e.Records, e.Bytes, e.Chunks)
}

func (e *FetchLimitError) Unwrap() error { return e.Cause }

type cachedToken struct {
	Value   string
	Expires time.Time
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// replayBody lets the shared RetryHttpClient resend POST requests after a
// retryable response. net/http closes a request body after each attempt, while
// RetryHttpClient intentionally reuses the request value.
type replayBody struct {
	mu   sync.Mutex
	data []byte
	read *bytes.Reader
}

func newReplayBody(data []byte) *replayBody {
	copyOfData := append([]byte(nil), data...)
	return &replayBody{data: copyOfData, read: bytes.NewReader(copyOfData)}
}

func (b *replayBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.read.Read(p)
}

func (b *replayBody) Close() error {
	b.mu.Lock()
	b.read.Reset(b.data)
	b.mu.Unlock()
	return nil
}

type Client struct {
	conf     *Config
	http     httpDoer
	baseHTTP *http.Client
	limiter  *rate.Limiter

	mu                  sync.Mutex
	tokens              map[string]cachedToken
	managementNotBefore time.Time
	now                 func() time.Time
	fetchDeadline       time.Duration
	retryTimeout        time.Duration
	retryBackoff        time.Duration
	// chunkRetentionBytes and chunkRetentionRecords override how much decoded
	// payload one fetch chunk may retain before it yields a continuation.
	chunkRetentionBytes   int
	chunkRetentionRecords int
	// pageRetentionRecords overrides how many elements one streamed page of an
	// envelope may contribute before the page is rejected with its cursor
	// intact.
	pageRetentionRecords int
	// continuationPageLimit overrides how many source pages one window may read
	// cumulatively, across every pass that resumes the same cursor.
	continuationPageLimit int
	// parentChildPageLimit overrides how many child pages one parent of a
	// parent-child fan-out may supply, cumulatively across every pass that
	// resumes the same cursor.
	parentChildPageLimit int
}

func NewClient(conf *Config, hc *http.Client) (*Client, error) {
	if conf == nil {
		return nil, errors.New("nil Microsoft config")
	}
	if conf.Requests_Per_Minute < 1 {
		return nil, errors.New("Microsoft request rate must be positive")
	}
	client := &Client{
		conf:                  conf,
		limiter:               rate.NewLimiter(rate.Every(time.Minute/time.Duration(conf.Requests_Per_Minute)), conf.Requests_Per_Minute),
		tokens:                make(map[string]cachedToken),
		now:                   time.Now,
		fetchDeadline:         defaultFetchDeadline,
		retryTimeout:          defaultHTTPAttemptTimeout,
		retryBackoff:          defaultRetryBackoff,
		chunkRetentionBytes:   maximumChunkRecordBytes,
		chunkRetentionRecords: maximumChunkRecords,
		pageRetentionRecords:  maximumEnvelopePageRecords,
		continuationPageLimit: maximumContinuationPages,
		parentChildPageLimit:  maximumParentChildPages,
	}
	if hc != nil {
		client.baseHTTP = hc
	}
	return client, nil
}

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if c.http != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			if internalFetchDeadline(ctx) && ctx.Err() == nil {
				return nil, errFetchPacing
			}
			return nil, err
		}
		resp, err := c.http.Do(req)
		return resp, deferredFetchDeadline(ctx, err)
	}
	// RetryHttpClient captures its construction context. Construct it for the
	// bounded fetch context instead of retaining a job-lifetime retry loop.
	base := c.baseHTTP
	if base == nil {
		base = &http.Client{Timeout: c.retryTimeout}
	}
	retry := utils.NewRetryHttpClientWithClient(c.limiter, exactOriginClient(base, req.URL), c.retryBackoff, ctx, nil)
	retry.SetRetryAfterLimit(utils.MaximumRetryAfter)
	resp, err := retry.Do(req)
	if errors.Is(err, utils.ErrRateLimitWait) && internalFetchDeadline(ctx) && ctx.Err() == nil {
		return nil, errFetchPacing
	}
	return resp, deferredFetchDeadline(ctx, err)
}

// exactOriginClient clones base and restricts every redirect to the initial
// request's scheme, host, and effective port. The check runs before net/http
// dispatches the redirected request, so bearer headers and replayable POST
// bodies cannot cross an origin boundary.
func exactOriginClient(base *http.Client, origin *url.URL) *http.Client {
	var client http.Client
	if base != nil {
		client = *base
	}
	prior := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameEffectiveOrigin(origin, req.URL) {
			return http.ErrUseLastResponse
		}
		if prior != nil {
			return prior(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &client
}

func sameEffectiveOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	leftPort, leftOK := effectivePort(left)
	rightPort, rightOK := effectivePort(right)
	return leftOK && rightOK && leftPort == rightPort
}

func effectivePort(u *url.URL) (string, bool) {
	if u == nil {
		return "", false
	}
	if port := u.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", false
		}
		return strconv.FormatUint(number, 10), true
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80", true
	case "https":
		return "443", true
	default:
		return "", true
	}
}

func (c *Client) Fetch(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string) ([]FetchedRecord, error) {
	ctx, cancel := withFetchDeadline(ctx, c.fetchDeadline)
	defer cancel()
	maximumBytes, maximumRecords := c.retentionBudget()
	var result []FetchedRecord
	var continuation *FetchContinuation
	retained := 0
	for chunkNumber := 0; chunkNumber < maximumContinuationPages; chunkNumber++ {
		chunk, err := c.FetchChunk(ctx, dataset, start, end, subscriptionID, continuation)
		if err != nil {
			if len(result) != 0 {
				return result, &FetchLimitError{Dataset: dataset.Name, Reason: "fetch error", Records: len(result), Bytes: retained,
					Chunks: chunkNumber, Continuation: continuation, Cause: err}
			}
			return result, err
		}
		result = append(result, chunk.Records...)
		retained += chunk.retained
		if chunk.PageBudgetExhausted {
			return result, &FetchLimitError{Dataset: dataset.Name, Reason: "page budget", Records: len(result), Bytes: retained, Chunks: chunkNumber + 1}
		}
		if chunk.Complete {
			return result, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, &FetchLimitError{Dataset: dataset.Name, Reason: "deadline", Records: len(result), Bytes: retained,
				Chunks: chunkNumber + 1, Continuation: chunk.Continuation, Cause: ctxErr}
		}
		if chunk.Continuation == nil {
			return result, fmt.Errorf("%s fetch stopped without a continuation", dataset.Name)
		}
		if retained >= maximumBytes || len(result) >= maximumRecords {
			return result, &FetchLimitError{Dataset: dataset.Name, Reason: "retention budget", Records: len(result), Bytes: retained,
				Chunks: chunkNumber + 1, Continuation: chunk.Continuation}
		}
		continuation = chunk.Continuation
	}
	return result, &FetchLimitError{Dataset: dataset.Name, Reason: "chunk safety limit", Records: len(result), Bytes: retained,
		Chunks: maximumContinuationPages, Continuation: continuation}
}

func (c *Client) request(ctx context.Context, method, endpoint, scope string, body []byte) ([]byte, error) {
	if err := c.validateURL(endpoint); err != nil {
		return nil, err
	}
	if err := claimFetchRequest(ctx); err != nil {
		return nil, err
	}
	token, err := c.token(ctx, scope)
	if err != nil {
		return nil, err
	}
	var requestBody io.ReadCloser
	var replayData []byte
	if body != nil {
		replayData = append([]byte(nil), body...)
		requestBody = newReplayBody(replayData)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return newReplayBody(replayData), nil
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gravwell-microsoft/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, safeTransportError(err)
	}
	if err := ctx.Err(); err != nil {
		// The body is drained and closed before the yield so the page this
		// response belongs to cannot hold a connection open past the chunk that
		// abandoned it.
		utils.DrainResponse(resp)
		return nil, safeTransportError(deferredFetchDeadline(ctx, err))
	}
	failed := resp.StatusCode < 200 || resp.StatusCode >= 300
	limit := int64(maximumAPIResponseBody)
	if failed {
		limit = maximumAPIErrorBody
	}
	responseBody, readErr := readCappedBody(resp, limit)
	resp.Body.Close()
	if failed {
		// The body of a failed request is never decoded, so an oversized or
		// truncated one still reports the status the operator needs.
		return nil, fmt.Errorf("Microsoft API request failed with HTTP %d", resp.StatusCode)
	}
	if readErr != nil {
		return nil, readErr
	}
	return responseBody, nil
}

// readCappedBody buffers at most limit bytes of resp and refuses a larger body
// instead of growing the Hosted Runner's resident set to whatever fits in one
// attempt timeout. The declared length is honored first so an oversized
// response is rejected before any of it is read.
func readCappedBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("Microsoft API response exceeds %d bytes", limit)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, safeTransportError(err)
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("Microsoft API response exceeds %d bytes", limit)
	}
	return payload, nil
}

func (c *Client) token(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	if token, ok := c.tokens[scope]; ok && time.Until(token.Expires) > 2*time.Minute {
		c.mu.Unlock()
		return token.Value, nil
	}
	c.mu.Unlock()
	endpoint := strings.TrimRight(c.conf.Auth_Host, "/") + "/" + url.PathEscape(c.conf.Tenant_ID) + "/oauth2/v2.0/token"
	form := url.Values{"client_id": {c.conf.Client_ID}, "client_secret": {c.conf.Client_Secret}, "grant_type": {"client_credentials"}, "scope": {scope}}
	body, err := c.tokenResponse(ctx, endpoint, form.Encode())
	if err != nil {
		return "", err
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", errors.New("decode Microsoft token response")
	}
	if decoded.AccessToken == "" {
		return "", errors.New("Microsoft token response did not contain access_token")
	}
	if decoded.ExpiresIn < 0 || decoded.ExpiresIn > int64((time.Duration(1<<63-1))/time.Second) {
		return "", errors.New("Microsoft token expiry is out of range")
	}
	if decoded.ExpiresIn == 0 {
		decoded.ExpiresIn = 3600
	}
	c.mu.Lock()
	c.tokens[scope] = cachedToken{Value: decoded.AccessToken, Expires: time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second)}
	c.mu.Unlock()
	return decoded.AccessToken, nil
}

func (c *Client) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("Microsoft API URL must be absolute HTTPS")
	}
	allowed := []string{c.conf.Graph_Host, c.conf.ARM_Host, c.conf.Defender_Host, c.conf.Defender_XDR_Host, c.conf.PowerBI_Host, c.conf.Management_Host, c.conf.Auth_Host}
	for _, base := range allowed {
		parsed, parseErr := url.Parse(base)
		if parseErr == nil && sameEffectiveOrigin(parsed, u) {
			return nil
		}
	}
	return errors.New("unexpected Microsoft API origin")
}

// datasetRequest is the only way a data request reaches the wire. It binds the
// endpoint to the configured origin of dataset.Service before anything else
// happens, and then issues the request with that service's token audience.
//
// validateURL alone was not that binding. It accepts any configured Microsoft
// origin, which is correct for what it guards -- the allowlist of hosts this
// plugin may talk to at all -- but it cannot say which of them a given
// dataset's bearer token belongs to. Continuations received during a pass were
// already pinned to the origin that served them by resolveContinuation, so the
// only way a URL on another service's origin could appear was from state:
// NextURL, ParentNextURL and ChildNextURL are restored from the persisted
// cursor and used to dispatch directly. A stale or tampered cursor document
// therefore attached the dataset's token -- a Graph token, say -- to a request
// aimed at the configured ARM, Defender or Power BI origin, and the service
// that received it was handed a bearer credential minted for a different
// resource (CWE-200).
//
// The check runs before c.token, so a refused endpoint costs no token
// acquisition and no dispatch at all.
func (c *Client) datasetRequest(ctx context.Context, method string, dataset Dataset, endpoint string, body []byte) ([]byte, error) {
	if err := c.validateDatasetURL(dataset, endpoint); err != nil {
		return nil, err
	}
	return c.request(ctx, method, endpoint, c.conf.Scope(dataset.Service, dataset.Scope), body)
}

// validateDatasetURL reports whether raw is a URL this dataset may request:
// absolute HTTPS, within the length bound a cursor field is accepted under, and
// on the exact configured origin of dataset.Service.
//
// The expected origin is read from Config.Base rather than from a compiled-in
// host list, so a sovereign or air-gapped deployment that points Graph-Host,
// ARM-Host, Defender-Host, Defender-XDR-Host or PowerBI-Host at its own
// endpoints is bound to those, and the relative and absolute continuation forms
// resolveContinuation accepts keep working unchanged: both resolve against the
// page that served them, which is on this origin by induction from the first
// URL, which Config.Base built.
//
// A mismatch is reported as an incompatible continuation. That is what routes a
// stored cursor pointing at another service into the existing reset path --
// which rewrites the cursor key and restarts the window from the durable
// watermark -- rather than failing identically on every later poll, because
// nothing else in the plugin rewrites that key. Endpoints built from
// Config.Base cannot reach it: Verify has already proved every host is an
// absolute HTTPS base, so only a cursor field can disagree.
func (c *Client) validateDatasetURL(dataset Dataset, raw string) error {
	if len(raw) > maximumContinuationURLLength {
		return fmt.Errorf("%w: %s request URL exceeds %d bytes", errIncompatibleContinuation, dataset.Name, maximumContinuationURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("%w: %s request URL must be absolute HTTPS", errIncompatibleContinuation, dataset.Name)
	}
	base := c.conf.Base(dataset.Service)
	if base == "" {
		return fmt.Errorf("%s has no configured service origin", dataset.Name)
	}
	expected, parseErr := url.Parse(base)
	if parseErr != nil || expected.Host == "" {
		return fmt.Errorf("%s configured service origin is not a URL", dataset.Name)
	}
	if !sameEffectiveOrigin(expected, u) {
		return fmt.Errorf("%w: %s request URL is not on the configured origin of its service", errIncompatibleContinuation, dataset.Name)
	}
	return nil
}

// resolveContinuation accepts the two forms returned by Microsoft services:
// an absolute URL (Graph commonly uses this) or a relative ARM nextLink (Azure
// Policy and some Azure Monitor endpoints use this).  Every resolved URL still
// passes the same allowlisted HTTPS-origin check before it can be requested.
//
// The length bound is measured on both the URL as published and the URL as
// resolved. A relative reference is shorter than what it resolves to and
// re-encoding a query can shorten it as well as lengthen it, so accepting one
// form and persisting the other would admit a continuation just below the bound
// that every request after it refused. Crossing it fails the chunk with the
// cursor untouched: nothing oversized is persisted, and the next pass re-reads
// the same page, so no acknowledged record is skipped.
func (c *Client) resolveContinuation(currentURL, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > maximumContinuationURLLength {
		return "", fmt.Errorf("Microsoft continuation URL exceeds %d bytes", maximumContinuationURLLength)
	}
	base, err := url.Parse(currentURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return "", errors.New("Microsoft continuation base URL must be absolute HTTPS")
	}
	reference, err := url.Parse(trimmed)
	if err != nil {
		return "", errors.New("invalid Microsoft continuation URL")
	}
	resolved := reference
	if !reference.IsAbs() {
		resolved = base.ResolveReference(reference)
	}
	if !sameEffectiveOrigin(base, resolved) {
		return "", errors.New("Microsoft continuation must retain the request origin")
	}
	value := resolved.String()
	if len(value) > maximumContinuationURLLength {
		return "", fmt.Errorf("Microsoft continuation URL exceeds %d bytes", maximumContinuationURLLength)
	}
	if err := c.validateURL(value); err != nil {
		return "", err
	}
	return value, nil
}

func expandPath(path, subscriptionID string) (string, error) {
	if strings.Contains(path, "{subscriptionId}") {
		if subscriptionID == "" {
			return "", errors.New("subscription ID is required")
		}
		path = strings.ReplaceAll(path, "{subscriptionId}", url.PathEscape(subscriptionID))
	}
	if strings.Contains(path, "{") {
		return "", errors.New("Microsoft API path contains an unresolved placeholder")
	}
	return path, nil
}

func decodeRecord(dataset Dataset, raw json.RawMessage) (FetchedRecord, error) {
	return decodeRecordWithIdentityPolicy(dataset, raw, false)
}

func decodeRecordRequiringIdentity(dataset Dataset, raw json.RawMessage) (FetchedRecord, error) {
	return decodeRecordWithIdentityPolicy(dataset, raw, true)
}

func decodeRecordWithIdentityPolicy(dataset Dataset, raw json.RawMessage, requireIdentity bool) (FetchedRecord, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return FetchedRecord{}, fmt.Errorf("compact %s record: %w", dataset.Name, err)
	}
	payload := append([]byte(nil), compact.Bytes()...)
	if bytes.ContainsAny(payload, "\r\n") {
		return FetchedRecord{}, fmt.Errorf("compacted %s record contains a physical line break", dataset.Name)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return FetchedRecord{}, err
	}
	if object == nil {
		return FetchedRecord{}, fmt.Errorf("%s record must be a JSON object", dataset.Name)
	}
	id := stringValueAtPath(object, dataset.IDField)
	if len(dataset.IDFields) != 0 {
		parts := make([]string, 0, len(dataset.IDFields))
		for _, path := range dataset.IDFields {
			value := stringValueAtPath(object, path)
			if value == "" {
				// A partial composite key can collapse distinct records, e.g.
				// multiple alert-evidence entities sharing one device/time.
				parts = nil
				break
			}
			parts = append(parts, value)
		}
		id = strings.Join(parts, "\x1f")
	}
	if id == "" {
		if requireIdentity {
			return FetchedRecord{}, fmt.Errorf("%s record is missing its required source identity", dataset.Name)
		}
		digest := sha256.Sum256(payload)
		id = "sha256:" + hex.EncodeToString(digest[:])
	}
	timestamp := parseTimestamp(stringValueAtPath(object, dataset.TimeField))
	return FetchedRecord{Raw: payload, ID: id, Timestamp: timestamp}, nil
}

func stringValueAtPath(object map[string]any, path string) string {
	if path == "" {
		return ""
	}
	var current any = object
	for _, part := range strings.Split(path, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = mapping[part]
		if !ok {
			return ""
		}
	}
	switch value := current.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func parseTimestamp(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// Preserve cancellation identity without logging request URLs or arbitrary
// transport diagnostics, which may contain sensitive query parameters.
func safeTransportError(err error) error {
	if errors.Is(err, errFetchPacing) {
		return errFetchPacing
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("Microsoft HTTP transport failed")
}

// tokenResponse uses the same shared retry client and pacing policy as data
// requests. It never emits response bodies or request credentials.
func (c *Client) tokenResponse(ctx context.Context, endpoint, form string) ([]byte, error) {
	if err := c.validateURL(endpoint); err != nil {
		return nil, err
	}
	replayData := []byte(form)
	requestBody := newReplayBody(replayData)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return nil, errors.New("invalid Microsoft token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ContentLength = int64(len(form))
	req.GetBody = func() (io.ReadCloser, error) {
		return newReplayBody(replayData), nil
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, safeTransportError(err)
	}
	if err := ctx.Err(); err != nil {
		// A token refresh is part of the fetch whose deadline this is, so it
		// yields the same durable checkpoint a data page does rather than
		// discarding the pages the chunk already holds.
		utils.DrainResponse(resp)
		return nil, safeTransportError(deferredFetchDeadline(ctx, err))
	}
	body, readErr := readCappedBody(resp, maximumTokenResponseBody)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Microsoft token request failed with HTTP %d", resp.StatusCode)
	}
	if readErr != nil {
		return nil, readErr
	}
	return body, nil
}
