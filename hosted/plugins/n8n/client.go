/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/time/rate"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	requestTimeout = 90 * time.Second
	// maxRetryAfter keeps an untrusted server from suspending the shared
	// per-plugin request gate for an operationally unbounded interval. The wait
	// remains cancellation-aware and happens before the per-request timeout.
	maxRetryAfter = 15 * time.Minute
	// defaultMaxRawRecords independently bounds tiny records and pagination
	// overlap, which can otherwise amplify a small payload into large slices.
	defaultMaxRawRecords = 500_000
	// maxCursorBytes bounds one encoded nextCursor value read from a response.
	maxCursorBytes = 64 * 1024
	// maxExecutionIDLength is the longest execution ID accepted from n8n or state.
	maxExecutionIDLength = 128
	// maxJSONNestingDepth bounds container nesting in any JSON value read from n8n.
	maxJSONNestingDepth = 10_000
)

type client struct {
	baseURL                *url.URL
	credential             string
	http                   *http.Client
	limiter                requestLimiter
	pageSize               int
	maxPages               int
	maxRecords             int
	recordLimit            int64
	includeExecutionData   bool
	executionStartedAfter  time.Time
	executionStartedBefore time.Time
	responseTime           time.Time
	exchangeTimeout        time.Duration
}

type requestLimiter interface {
	Wait(context.Context) error
}

type retryDeferrer interface {
	DeferUntil(time.Time)
}

type requestGate struct {
	limiter *rate.Limiter
	mu      sync.Mutex
	retryAt time.Time
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
}

func newRequestLimiter(requestsPerMinute int) requestLimiter {
	interval := time.Minute / time.Duration(requestsPerMinute)
	return &requestGate{
		limiter: rate.NewLimiter(rate.Every(interval), 1),
		now:     time.Now,
		wait:    waitForContext,
	}
}

func (g *requestGate) Wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		now, retryAt := g.now(), g.retryAt
		g.mu.Unlock()
		if retryAt.After(now) {
			if err := g.wait(ctx, retryAt.Sub(now)); err != nil {
				return err
			}
			continue
		}
		return g.limiter.Wait(ctx)
	}
}

func (g *requestGate) DeferUntil(deadline time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if deadline.After(g.retryAt) {
		g.retryAt = deadline
	}
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 1 || seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return time.Time{}, false
		}
		deadline := now.Add(time.Duration(seconds) * time.Second)
		return clampRetryAfter(now, deadline), true
	}
	deadline, err := http.ParseTime(value)
	if err != nil || !deadline.After(now) {
		return time.Time{}, false
	}
	return clampRetryAfter(now, deadline), true
}

func clampRetryAfter(now, deadline time.Time) time.Time {
	ceiling := now.Add(maxRetryAfter)
	if deadline.After(ceiling) {
		return ceiling
	}
	return deadline
}

type collection struct {
	Count int
}

type collectedRecord struct {
	identity     [sha256.Size]byte
	identityText string
	statusText   string
	fingerprint  [sha256.Size]byte
	page         int
	timestamp    time.Time
}

type datasetUnavailableError struct {
	Dataset string
	Status  int
}

func (e *datasetUnavailableError) Error() string {
	return fmt.Sprintf("Dataset %s is unavailable with HTTP %d", e.Dataset, e.Status)
}

func newClient(baseURL, credential string, requestsPerMinute, pageSize, maxPages int, hc *http.Client) (*client, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, errors.New("invalid Base-URL")
	}
	if credential == "" {
		return nil, errors.New("empty n8n API credential")
	}
	if requestsPerMinute < 1 || pageSize < 1 || maxPages < 1 {
		return nil, errors.New("rate and pagination limits must be positive")
	}
	if hc == nil {
		hc = &http.Client{Timeout: requestTimeout}
	}
	copyHTTP := *hc
	// Keep the API key from following a redirect without modifying a caller's
	// client or any shared Hosted Runner HTTP behavior.
	copyHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{
		baseURL: base, credential: credential, http: &copyHTTP, limiter: newRequestLimiter(requestsPerMinute),
		pageSize: pageSize, maxPages: maxPages,
		maxRecords: defaultMaxRawRecords, recordLimit: maxNormalizedRecordSize,
		exchangeTimeout: requestTimeout,
	}, nil
}

func (c *client) objectMemberLimit(definition datasetDefinition) int {
	if definition.Name == "executions" && c.includeExecutionData {
		// Expanded execution records are already bounded by the 64 MiB record
		// limit and 90-second request context. Do not reject otherwise valid n8n
		// data at an arbitrary object-member count.
		return 0
	}
	return maxRecordMembers
}

func (c *client) collectStreaming(ctx context.Context, definition datasetDefinition, onRecord func(collectedRecord, *normalizedRecord) error) (result collection, err error) {
	query := make(url.Values)
	if definition.RecordsField != "" {
		query.Set("limit", strconv.Itoa(c.pageSize))
	}
	if definition.Name == "executions" {
		query.Set("includeData", strconv.FormatBool(c.includeExecutionData))
		if c.includeExecutionData {
			if !c.executionStartedAfter.IsZero() {
				query.Set("startedAfter", c.executionStartedAfter.UTC().Format(time.RFC3339Nano))
			}
			if !c.executionStartedBefore.IsZero() {
				query.Set("startedBefore", c.executionStartedBefore.UTC().Format(time.RFC3339Nano))
			}
		}
	}
	return c.collectStreamingQuery(ctx, definition, query, c.objectMemberLimit(definition), onRecord)
}

func (c *client) collectStreamingQuery(ctx context.Context, definition datasetDefinition, query url.Values, memberLimit int, onRecord func(collectedRecord, *normalizedRecord) error) (result collection, err error) {
	normalizer := newRecordNormalizer("", c.recordLimit)
	normalizer.memberLimit = memberLimit
	normalizer.allowOversized = definition.Name == "executions" && memberLimit == 0
	defer func() {
		err = errors.Join(err, normalizer.cleanup())
		if err != nil {
			result = collection{}
		}
	}()
	requestURL, err := c.endpoint(definition.Path)
	if err != nil {
		return collection{}, err
	}

	seen := make(map[[sha256.Size]byte]struct{})
	for page := 1; page <= c.maxPages; page++ {
		requestKey := sha256.Sum256([]byte(requestURL.String() + "?" + query.Encode()))
		if _, ok := seen[requestKey]; ok {
			return collection{}, errors.New("n8n returned a repeated pagination cursor")
		}
		seen[requestKey] = struct{}{}

		// Rate-limit and server-requested Retry-After waits are part of the poll,
		// not the HTTP exchange. Waiting before the request context preserves the
		// full requestTimeout for connecting to n8n and consuming the response.
		if err := c.limiter.Wait(ctx); err != nil {
			if parentErr := ctx.Err(); parentErr != nil {
				return collection{}, parentErr
			}
			return collection{}, fmt.Errorf("wait to request Dataset %s page %d: %w", definition.Name, page, err)
		}
		requestCtx, cancel := context.WithTimeout(ctx, c.exchangeTimeout)
		response, err := c.request(requestCtx, definition, requestURL, query)
		if err != nil {
			requestErr, parentErr := requestCtx.Err(), ctx.Err()
			cancel()
			if parentErr != nil {
				return collection{}, errors.Join(err, parentErr)
			}
			if requestErr != nil {
				return collection{}, fmt.Errorf("request Dataset %s page %d within 90-second request context: %w", definition.Name, page, errors.Join(err, requestErr))
			}
			return collection{}, fmt.Errorf("request Dataset %s page %d: %w", definition.Name, page, err)
		}
		cursor, err := c.consumeResponse(requestCtx, response, definition, page, c.maxRecords-result.Count, &result, onRecord, normalizer)
		requestErr := requestCtx.Err()
		cancel()
		// Capture request failure before our own cancel, even after a successful
		// parse. Preserve independent processing errors alongside cancellation.
		err = errors.Join(err, requestErr, ctx.Err())
		if err != nil {
			return collection{}, fmt.Errorf("consume Dataset %s page %d response: %w", definition.Name, page, err)
		}
		if definition.CursorField == "" {
			return result, nil
		}
		if cursor == "" {
			return result, nil
		}
		query.Set("cursor", cursor)
	}
	return collection{}, fmt.Errorf("Dataset %s exceeded Max-Pages=%d; increase Max-Pages", definition.Name, c.maxPages)
}

// activeExecutionIDs reads only lightweight metadata. Expanded execution
// payloads are emitted after completion; retaining nonterminal IDs prevents a
// long-running, queued, waiting, or unknown execution from falling behind the
// incremental startedAfter boundary.
func (c *client) activeExecutionIDs(ctx context.Context, definition datasetDefinition, limit int) (map[string]struct{}, error) {
	ids := make(map[string]struct{})
	for _, status := range []string{"new", "running", "unknown", "waiting"} {
		query := make(url.Values)
		query.Set("limit", strconv.Itoa(c.pageSize))
		query.Set("includeData", "false")
		query.Set("status", status)
		_, err := c.collectStreamingQuery(ctx, definition, query, maxRecordMembers, func(source collectedRecord, _ *normalizedRecord) error {
			if !validExecutionID(source.identityText) {
				return errors.New("active execution returned an invalid id")
			}
			if _, duplicate := ids[source.identityText]; !duplicate && len(ids) >= limit {
				return fmt.Errorf("active execution limit of %d exceeded", limit)
			}
			ids[source.identityText] = struct{}{}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// collectExecution fetches one completed execution with full data. A 404 means
// retention removed it after the running probe; that is a source-side race,
// not an unavailable executions dataset. Any other refusal of this request is
// an ordinary failure: the execution is still tracked and cannot be read, so
// the poll must stop without advancing state instead of reporting the whole
// dataset as unavailable.
func (c *client) collectExecution(ctx context.Context, id string, page int, onRecord func(collectedRecord, *normalizedRecord) error) (count int, found bool, err error) {
	if !validExecutionID(id) {
		return 0, false, errors.New("invalid execution id")
	}
	definition := datasetCatalog["executions"]
	definition.Path += "/" + id
	definition.RecordsField, definition.CursorField = "", ""
	query := make(url.Values)
	query.Set("includeData", "true")
	result, err := c.collectStreamingQuery(ctx, definition, query, c.objectMemberLimit(definition), func(source collectedRecord, body *normalizedRecord) error {
		if source.identityText != id {
			return errors.New("execution detail returned a different id")
		}
		source.page = page
		return onRecord(source, body)
	})
	if err != nil {
		// A nil classification is a 404 that was the sole cause of the
		// failure: the execution is counted as dropped.
		return 0, false, executionDetailFailure(id, err)
	}
	return result.Count, true, nil
}

// executionDetailFailure classifies a failed single-execution request. Only a
// 404 that is the sole cause of the failure means retention removed the
// execution; it returns nil, and the execution is counted as dropped. Any
// other refusal becomes a new error rather than a wrapped one: Handle isolates
// the unavailable type as a dataset warning and would then report a poll that
// left tracked executions behind as a success. A failure joined to the
// refusal, such as scratch cleanup, the request deadline, or parent
// cancellation, is kept rather than discarded with the 404.
func executionDetailFailure(id string, err error) error {
	var unavailable *datasetUnavailableError
	if !errors.As(err, &unavailable) {
		return err
	}
	var refusal error
	if unavailable.Status != http.StatusNotFound {
		refusal = fmt.Errorf("request for the detail of execution %s was refused with HTTP %d; the n8n API key must be able to read single executions", id, unavailable.Status)
	}
	companions := unavailableCompanions(err)
	if len(companions) == 0 {
		return refusal
	}
	return errors.Join(append([]error{refusal}, companions...)...)
}

// unavailableCompanions returns every failure joined to the dataset-unavailable
// error in err, without that error or the wrappers that only describe it.
func unavailableCompanions(err error) []error {
	var unavailable *datasetUnavailableError
	if !errors.As(err, &unavailable) {
		return []error{err}
	}
	switch e := err.(type) {
	case *datasetUnavailableError:
		return nil
	case interface{ Unwrap() []error }:
		var companions []error
		for _, member := range e.Unwrap() {
			companions = append(companions, unavailableCompanions(member)...)
		}
		return companions
	case interface{ Unwrap() error }:
		return unavailableCompanions(e.Unwrap())
	default:
		return nil
	}
}

func validExecutionID(id string) bool {
	if id == "" || len(id) > maxExecutionIDLength {
		return false
	}
	for i := range len(id) {
		b := id[i]
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') && b != '-' && b != '_' {
			return false
		}
	}
	return true
}

func (c *client) request(ctx context.Context, definition datasetDefinition, requestURL *url.URL, query url.Values) (*http.Response, error) {
	u := *requestURL
	effectiveQuery := u.Query()
	for key, values := range query {
		effectiveQuery[key] = append([]string(nil), values...)
	}
	u.RawQuery = effectiveQuery.Encode()
	var body io.Reader
	if definition.Method == http.MethodPost {
		body = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequestWithContext(ctx, definition.Method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-N8N-API-KEY", c.credential)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%s %s request failed", definition.Method, safeURL(u.String()))
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if deadline, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			if deferrer, ok := c.limiter.(retryDeferrer); ok {
				deferrer.DeferUntil(deadline)
			}
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if responseTime, parseErr := http.ParseTime(resp.Header.Get("Date")); parseErr == nil {
			c.responseTime = responseTime.UTC()
		}
		return resp, nil
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusGone ||
		resp.StatusCode == http.StatusNotImplemented {
		return nil, &datasetUnavailableError{Dataset: definition.Name, Status: resp.StatusCode}
	}
	return nil, fmt.Errorf("%s %s failed with HTTP %d", definition.Method, safeURL(u.String()), resp.StatusCode)
}

func (c *client) consumeResponse(ctx context.Context, resp *http.Response, definition datasetDefinition, page, remaining int, result *collection, onRecord func(collectedRecord, *normalizedRecord) error, normalizer *recordNormalizer) (cursorResult string, err error) {
	defer resp.Body.Close()
	scanner := newResponseScanner(resp.Body)
	scanner.ctx = ctx
	defer func() {
		// Delimiter helpers deliberately redact parser/reader details. Recover
		// cancellation observed by the scanner without hiding callback failures.
		if scanner.cancelErr != nil {
			err = scanner.cancelErr
		}
		if err == nil {
			err = ctx.Err()
		}
	}()
	if definition.RecordsField == "" {
		if err := deliverNormalizedRecord(ctx, scanner, false, page, result, onRecord, true, normalizer); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := scanner.expect('{'); err != nil {
		return "", errors.New("expected a JSON object")
	}
	seenRecords, seenCursor, seenError := false, false, false
	var cursor string
	for !scanner.nextIs('}') {
		key, known, err := scanner.objectKey(8*max(len(definition.RecordsField), len(definition.CursorField), len("error")) + 2)
		if err != nil || scanner.expect(':') != nil {
			return "", errors.New("invalid JSON response")
		}
		switch {
		case known && key == definition.RecordsField:
			if seenRecords {
				return "", errors.New("duplicate records field")
			}
			seenRecords = true
			if err := scanner.expect('['); err != nil {
				return "", errors.New("response records field is not an array")
			}
			for !scanner.nextIs(']') {
				if remaining <= 0 {
					return "", fmt.Errorf("raw record limit of %d exceeded", c.maxRecords)
				}
				if err := deliverNormalizedRecord(ctx, scanner, definition.Name == "credentials", page, result, onRecord, false, normalizer); err != nil {
					return "", err
				}
				remaining--
				if !scanner.nextIs(']') {
					if err := scanner.expect(','); err != nil {
						return "", errors.New("invalid JSON response records array")
					}
					if scanner.nextIs(']') {
						return "", errors.New("invalid JSON response records array")
					}
				}
			}
			if err := scanner.expect(']'); err != nil {
				return "", errors.New("invalid JSON response records array")
			}
		case known && key == definition.CursorField:
			if seenCursor {
				return "", fmt.Errorf("%s must appear only once", definition.CursorField)
			}
			seenCursor = true
			raw, first, err := scanner.value(maxCursorBytes, true)
			if err != nil {
				return "", errors.New("nextCursor is invalid or exceeds the 64 KiB cursor limit")
			}
			if first == 'n' {
				if !json.Valid(raw) || string(raw) != "null" {
					return "", fmt.Errorf("%s must be a string or null", definition.CursorField)
				}
			} else if first == '"' {
				if err := json.Unmarshal(raw, &cursor); err != nil {
					return "", fmt.Errorf("%s must be a string or null", definition.CursorField)
				}
				if strings.TrimSpace(cursor) == "" {
					return "", fmt.Errorf("%s must be null or a non-empty string", definition.CursorField)
				}
			} else {
				return "", fmt.Errorf("%s must be a string or null", definition.CursorField)
			}
		case known && key == "error":
			if seenError {
				return "", errors.New("duplicate error field")
			}
			seenError = true
			_, first, err := scanner.value(0, false)
			if err != nil {
				return "", errors.New("invalid application error field")
			}
			if first != 'n' {
				return "", errors.New("n8n returned an application error")
			}
		default:
			if _, _, err := scanner.value(0, false); err != nil {
				return "", errors.New("invalid JSON response")
			}
		}
		if !scanner.nextIs('}') {
			if err := scanner.expect(','); err != nil {
				return "", errors.New("invalid JSON response")
			}
			if scanner.nextIs('}') {
				return "", errors.New("invalid JSON response")
			}
		}
	}
	if err := scanner.expect('}'); err != nil {
		return "", errors.New("invalid JSON response")
	}
	if !seenRecords {
		return "", fmt.Errorf("required response array %s is missing", definition.RecordsField)
	}
	if err := scanner.eof(); err != nil {
		return "", errors.New("response contains trailing or invalid JSON")
	}
	return cursor, nil
}

func deliverNormalizedRecord(ctx context.Context, scanner *responseScanner, scrub bool, page int, result *collection, onRecord func(collectedRecord, *normalizedRecord) error, requireEOF bool, normalizer *recordNormalizer) error {
	body, err := normalizer.normalize(ctx, scanner, scrub)
	if err != nil {
		return err
	}
	if requireEOF {
		if err := scanner.eof(); err != nil {
			return errors.New("response contains trailing or invalid JSON")
		}
	}
	record := collectedRecord{identity: body.identity, identityText: body.identityText, statusText: body.statusText, fingerprint: body.fingerprint, page: page, timestamp: body.timestamp}
	if err := ctx.Err(); err != nil {
		return err
	}
	result.Count++
	return errors.Join(onRecord(record, body), ctx.Err())
}

// responseScanner validates JSON incrementally. Captured values are bounded by
// Gravwell's actual maximum entry size before any byte slice is grown; ignored
// response fields are validated and discarded without accumulating the body.
type responseScanner struct {
	reader    *bufio.Reader
	ctx       context.Context
	cancelErr error
}

func (s *responseScanner) cancelled() error {
	if s.ctx != nil {
		if err := s.ctx.Err(); err != nil {
			s.cancelErr = err
			return err
		}
	}
	return nil
}

func newResponseScanner(reader io.Reader) *responseScanner {
	s := &responseScanner{}
	// Reader failures can contain URLs or response data. Keep them redacted at
	// the source, before parser helpers or normalization can propagate them.
	s.reader = bufio.NewReader(responseReader{s: s, r: reader})
	return s
}

type responseReader struct {
	s *responseScanner
	r io.Reader
}

func (r responseReader) Read(p []byte) (int, error) {
	if err := r.s.cancelled(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	// A canceled read cannot feed buffered bytes into later processing. Drop
	// any accompanying bytes so a callback or local error cannot occur after
	// the scanner has recorded this cancellation.
	if errors.Is(err, context.Canceled) {
		err = context.Canceled
		r.s.cancelErr = err
		return 0, err
	} else if errors.Is(err, context.DeadlineExceeded) {
		err = context.DeadlineExceeded
		r.s.cancelErr = err
		return 0, err
	} else if err != nil && !errors.Is(err, io.EOF) {
		err = errors.New("read n8n response failed")
	}
	return n, err
}

func (s *responseScanner) space() error {
	for scanned := 0; ; scanned++ {
		if scanned&1023 == 0 {
			if err := s.cancelled(); err != nil {
				return err
			}
		}
		b, err := s.reader.Peek(1)
		if err != nil {
			return err
		}
		if b[0] != ' ' && b[0] != '\t' && b[0] != '\r' && b[0] != '\n' {
			return nil
		}
		_, _ = s.reader.ReadByte()
	}
}

func (s *responseScanner) nextIs(want byte) bool {
	_ = s.space()
	b, err := s.reader.Peek(1)
	return err == nil && b[0] == want
}

func (s *responseScanner) expect(want byte) error {
	if err := s.space(); err != nil {
		return err
	}
	b, err := s.reader.ReadByte()
	if err != nil {
		return err
	}
	if b != want {
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func (s *responseScanner) objectKey(maxCapture int) (string, bool, error) {
	if err := s.space(); err != nil {
		return "", false, err
	}
	first, err := s.reader.ReadByte()
	if err != nil || first != '"' {
		return "", false, errors.New("expected JSON object key")
	}
	raw := []byte{'"'}
	complete := true
	for scanned := 0; ; scanned++ {
		if scanned&1023 == 0 {
			if err := s.cancelled(); err != nil {
				return "", false, err
			}
		}
		b, err := s.reader.ReadByte()
		if err != nil {
			return "", false, err
		}
		if len(raw) <= maxCapture {
			raw = append(raw, b)
		} else {
			complete = false
		}
		if b < 0x20 {
			return "", false, errors.New("invalid JSON object key")
		}
		if b == '\\' {
			if err := s.readStringEscape(&raw, maxCapture, &complete); err != nil {
				return "", false, err
			}
		} else if b == '"' {
			break
		}
	}
	if !complete {
		return "", false, nil
	}
	var key string
	if err := json.Unmarshal(raw, &key); err != nil {
		return "", false, err
	}
	return key, true, nil
}

func (s *responseScanner) readStringEscape(raw *[]byte, maxCapture int, complete *bool) error {
	b, err := s.reader.ReadByte()
	if err != nil {
		return err
	}
	if len(*raw) <= maxCapture {
		*raw = append(*raw, b)
	} else {
		*complete = false
	}
	switch b {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return nil
	case 'u':
		for range 4 {
			digit, err := s.reader.ReadByte()
			if err != nil {
				return err
			}
			if len(*raw) <= maxCapture {
				*raw = append(*raw, digit)
			} else {
				*complete = false
			}
			if !isHex(digit) {
				return errors.New("invalid JSON unicode escape")
			}
		}
		return nil
	default:
		return errors.New("invalid JSON escape")
	}
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func (s *responseScanner) value(limit int64, capture bool) ([]byte, byte, error) {
	return s.valueWithObjectLimit(limit, capture, 0)
}

// valueWithObjectLimit applies the per-object member bound while a scrubbed
// value is validated and discarded without retaining secret bytes.
func (s *responseScanner) valueWithObjectLimit(limit int64, capture bool, memberLimit int) ([]byte, byte, error) {
	if err := s.space(); err != nil {
		return nil, 0, err
	}
	var output bytes.Buffer
	var out *bytes.Buffer
	if capture {
		out = &output
	}
	var consumed int64
	first, err := scanJSONValue(s.reader, out, &consumed, limit, 0, memberLimit, s.cancelled)
	if err != nil {
		return nil, first, err
	}
	return output.Bytes(), first, nil
}

func scanJSONValue(reader *bufio.Reader, output *bytes.Buffer, consumed *int64, limit int64, depth int, memberLimit int, cancelled func() error) (byte, error) {
	if depth > maxJSONNestingDepth {
		return 0, errors.New("JSON nesting limit exceeded")
	}
	read := func() (byte, error) {
		if *consumed&1023 == 0 {
			if err := cancelled(); err != nil {
				return 0, err
			}
		}
		b, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		*consumed++
		if limit > 0 && *consumed > limit {
			return 0, errors.New("JSON value exceeds Gravwell's maximum entry size")
		}
		if output != nil {
			_ = output.WriteByte(b)
		}
		return b, nil
	}
	space := func() error {
		for {
			peek, err := reader.Peek(1)
			if err != nil {
				return err
			}
			if peek[0] != ' ' && peek[0] != '\t' && peek[0] != '\r' && peek[0] != '\n' {
				return nil
			}
			if _, err := read(); err != nil {
				return err
			}
		}
	}
	var parse func(int) (byte, error)
	parse = func(level int) (byte, error) {
		objectMemberCount := 0
		if level > maxJSONNestingDepth {
			return 0, errors.New("JSON nesting limit exceeded")
		}
		if err := space(); err != nil {
			return 0, err
		}
		peek, err := reader.Peek(1)
		if err != nil {
			return 0, err
		}
		first := peek[0]
		switch first {
		case '{', '[':
			opening, _ := read()
			closing := byte('}')
			object := opening == '{'
			if !object {
				closing = ']'
			}
			if err := space(); err != nil {
				return first, err
			}
			if s, _ := reader.Peek(1); len(s) > 0 && s[0] == closing {
				_, err := read()
				return first, err
			}
			for {
				if object {
					if s, _ := reader.Peek(1); len(s) == 0 || s[0] != '"' {
						return first, errors.New("expected JSON object key")
					}
					if _, err := scanJSONString(reader, read); err != nil {
						return first, err
					}
					if err := space(); err != nil {
						return first, err
					}
					colon, err := read()
					if err != nil || colon != ':' {
						return first, errors.New("expected JSON object colon")
					}
					if memberLimit > 0 {
						if objectMemberCount >= memberLimit {
							return first, fmt.Errorf("n8n object exceeds the %d object member limit", memberLimit)
						}
						objectMemberCount++
					}
				}
				if _, err := parse(level + 1); err != nil {
					return first, err
				}
				if err := space(); err != nil {
					return first, err
				}
				b, err := read()
				if err != nil {
					return first, err
				}
				if b == closing {
					return first, nil
				}
				if b != ',' {
					return first, errors.New("invalid JSON container separator")
				}
				if err := space(); err != nil {
					return first, err
				}
			}
		case '"':
			if _, err := scanJSONString(reader, read); err != nil {
				return first, err
			}
			return first, nil
		case 't':
			return first, scanLiteral("true", read)
		case 'f':
			return first, scanLiteral("false", read)
		case 'n':
			return first, scanLiteral("null", read)
		default:
			return first, scanJSONNumber(reader, read)
		}
	}
	return parse(depth)
}

func scanJSONString(reader *bufio.Reader, read func() (byte, error)) (string, error) {
	opening, err := read()
	if err != nil || opening != '"' {
		return "", errors.New("expected JSON string")
	}
	for {
		b, err := read()
		if err != nil {
			return "", err
		}
		if b < 0x20 {
			return "", errors.New("invalid control byte in JSON string")
		}
		if b == '"' {
			return "", nil
		}
		if b != '\\' {
			continue
		}
		escape, err := read()
		if err != nil {
			return "", err
		}
		switch escape {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			for range 4 {
				digit, err := read()
				if err != nil || !isHex(digit) {
					return "", errors.New("invalid JSON unicode escape")
				}
			}
		default:
			return "", errors.New("invalid JSON escape")
		}
	}
}

func scanLiteral(literal string, read func() (byte, error)) error {
	for i := range literal {
		b, err := read()
		if err != nil || b != literal[i] {
			return errors.New("invalid JSON literal")
		}
	}
	return nil
}

func scanJSONNumber(reader *bufio.Reader, read func() (byte, error)) error {
	consume := func() (byte, error) { return read() }
	peek, err := reader.Peek(1)
	if err != nil {
		return err
	}
	if peek[0] == '-' {
		if _, err := consume(); err != nil {
			return err
		}
		peek, err = reader.Peek(1)
		if err != nil {
			return err
		}
	}
	if peek[0] == '0' {
		if _, err := consume(); err != nil {
			return err
		}
		if next, err := reader.Peek(1); err == nil && next[0] >= '0' && next[0] <= '9' {
			return errors.New("leading zero in JSON number")
		}
	} else if peek[0] >= '1' && peek[0] <= '9' {
		for {
			peek, err = reader.Peek(1)
			if err != nil || peek[0] < '0' || peek[0] > '9' {
				break
			}
			if _, err := consume(); err != nil {
				return err
			}
		}
	} else {
		return errors.New("invalid JSON number")
	}
	if peek, err = reader.Peek(1); err == nil && peek[0] == '.' {
		_, _ = consume()
		peek, err = reader.Peek(1)
		if err != nil || peek[0] < '0' || peek[0] > '9' {
			return errors.New("invalid JSON fraction")
		}
		for {
			peek, err = reader.Peek(1)
			if err != nil || peek[0] < '0' || peek[0] > '9' {
				break
			}
			if _, err := consume(); err != nil {
				return err
			}
		}
	}
	if peek, err = reader.Peek(1); err == nil && (peek[0] == 'e' || peek[0] == 'E') {
		_, _ = consume()
		peek, err = reader.Peek(1)
		if err == nil && (peek[0] == '+' || peek[0] == '-') {
			_, _ = consume()
			peek, err = reader.Peek(1)
		}
		if err != nil || peek[0] < '0' || peek[0] > '9' {
			return errors.New("invalid JSON exponent")
		}
		for {
			peek, err = reader.Peek(1)
			if err != nil || peek[0] < '0' || peek[0] > '9' {
				break
			}
			if _, err := consume(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *responseScanner) eof() error {
	if err := s.space(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if _, err := s.reader.Peek(1); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

// endpoint builds a dataset URL structurally from the configured origin. Joining the
// strings and re-parsing the result lets any query, fragment, or escaped separator that
// survived in the base URL re-interpret the dataset path, which silently retargets every
// request at the bare base path, so the path is set on a copy of the parsed base instead.
func (c *client) endpoint(path string) (*url.URL, error) {
	result := *c.baseURL
	result.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	result.RawPath = ""
	result.RawQuery = ""
	result.ForceQuery = false
	result.Fragment = ""
	result.RawFragment = ""
	if result.Scheme != c.baseURL.Scheme || !strings.EqualFold(result.Host, c.baseURL.Host) || result.User != nil {
		return nil, errors.New("n8n endpoint escaped the configured API origin")
	}
	return &result, nil
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	query := u.Query()
	for key := range query {
		query.Set(key, "REDACTED")
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// Only fixed-width object-member hashes are sorted. This shared budget covers
// every open object, including ancestors of a deeply nested value. Wider
// objects spill sorted runs rather than expanding a generic JSON object graph.
const (
	memberHashBudget        = 4096
	maxNormalizedRecordSize = int64(64 * 1024 * 1024)
	maxRecordMembers        = 250_000
)

var identityFields = []string{"id", "uuid", "eventId", "executionId", "workflowId", "email", "name"}
var timestampFields = []string{"timestamp", "updatedAt", "createdAt", "startedAt", "stoppedAt", "updated_at", "created_at"}

type normalizedRecord struct {
	file                  *os.File
	size, limit           int64
	oversized             bool
	identity, fingerprint [sha256.Size]byte
	identityText          string
	statusText            string
	timestamp             time.Time
}

func (r *normalizedRecord) payload() ([]byte, error) {
	if r.oversized || r.size < 0 || r.limit <= 0 || r.size > r.limit || r.size > int64(^uint(0)>>1) {
		return nil, errors.New("invalid normalized n8n entry size")
	}
	data := make([]byte, int(r.size))
	if _, err := io.ReadFull(io.NewSectionReader(r.file, 0, r.size), data); err != nil {
		return nil, errors.New("read normalized n8n record failed")
	}
	return data, nil
}

type recordNormalizer struct {
	ctx            context.Context
	scanner        *responseScanner
	output         *os.File
	writer         *bufio.Writer
	size, limit    int64
	checkBytes     int
	scrub          bool
	allowOversized bool
	oversized      bool
	tempDir        string
	paths          map[string]struct{}
	scratchCreated int
	sorters        []*memberSorter
	members        int
	memberLimit    int
	identity       [sha256.Size]byte
	identityText   string
	statusText     string
	identityRank   int
	timestamp      time.Time
	timestampRank  int
}

func newRecordNormalizer(tempDir string, limit int64) *recordNormalizer {
	if limit > maxNormalizedRecordSize {
		limit = maxNormalizedRecordSize
	}
	return &recordNormalizer{tempDir: tempDir, limit: limit, memberLimit: maxRecordMembers, paths: make(map[string]struct{})}
}

func (n *recordNormalizer) normalize(ctx context.Context, scanner *responseScanner, scrub bool) (*normalizedRecord, error) {
	n.ctx, n.scanner, n.scrub = ctx, scanner, scrub
	scanner.ctx = ctx
	n.checkBytes = 0
	n.oversized = false
	n.scratchCreated = 0
	if err := n.cancelled(); err != nil {
		return nil, err
	}
	n.identity, n.identityText, n.statusText, n.timestamp = [sha256.Size]byte{}, "", "", time.Time{}
	n.identityRank, n.timestampRank = len(identityFields), len(timestampFields)
	clear(n.sorters[:cap(n.sorters)])
	n.sorters, n.members = n.sorters[:0], 0
	if n.output == nil {
		file, err := n.scratch()
		if err != nil {
			return nil, err
		}
		n.output, n.writer = file, bufio.NewWriterSize(file, 64*1024)
	} else if err := n.rewind(0); err != nil {
		return nil, err
	}
	if !scanner.nextIs('{') {
		return nil, errors.New("expected a JSON object")
	}
	fingerprint, err := n.value(0, nil)
	if err != nil {
		return nil, err
	}
	if err := n.cancelled(); err != nil {
		return nil, err
	}
	if err := n.writer.Flush(); err != nil {
		return nil, errors.New("write normalized n8n record failed")
	}
	if err := n.cancelled(); err != nil {
		return nil, err
	}
	if n.identityRank == len(identityFields) {
		text := "sha256:" + hex.EncodeToString(fingerprint[:])
		n.identity = sha256.Sum256([]byte(text))
	}
	if n.timestampRank == len(timestampFields) {
		n.timestamp = time.Now().UTC()
	}
	result := &normalizedRecord{file: n.output, size: n.size, limit: n.limit, oversized: n.oversized,
		identity: n.identity, identityText: n.identityText, statusText: n.statusText, fingerprint: fingerprint, timestamp: n.timestamp}
	return result, nil
}

func (n *recordNormalizer) scratch() (*os.File, error) {
	f, err := os.CreateTemp(n.tempDir, "gravwell-n8n-record-*")
	if err != nil {
		return nil, errors.New("create temporary n8n record failed")
	}
	n.paths[f.Name()] = struct{}{}
	n.scratchCreated++
	return f, nil
}

func (n *recordNormalizer) enterOversized() error {
	n.oversized = true
	n.members = 0
	for _, sorter := range n.sorters {
		sorter.memory = nil
		for level, path := range sorter.runs {
			if path == "" {
				continue
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.New("remove temporary n8n member hashes failed")
			}
			delete(n.paths, path)
			sorter.runs[level] = ""
		}
	}
	return nil
}

func (n *recordNormalizer) cleanup() error {
	var cleanupErr error
	if n.output != nil {
		if err := n.output.Close(); err != nil {
			cleanupErr = errors.New("close temporary n8n record failed")
		}
	}
	for path := range n.paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.New("remove temporary n8n record failed")
		}
	}
	return cleanupErr
}

func (n *recordNormalizer) cancelled() error {
	if n.ctx == nil {
		return nil
	}
	return n.ctx.Err()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return r.r.Read(p)
}

func (n *recordNormalizer) write(data []byte) error {
	// Retained strings arrive in buffered chunks, while numbers and escapes
	// may arrive byte by byte. Amortize cancellation checks across both forms.
	if n.checkBytes <= 0 {
		if err := n.cancelled(); err != nil {
			return err
		}
		n.checkBytes = 1024
	}
	n.checkBytes -= len(data)
	if n.oversized {
		return nil
	}
	if int64(len(data)) > n.limit-n.size {
		if n.allowOversized {
			return n.enterOversized()
		}
		return fmt.Errorf("n8n record exceeds the per-record safety limit of %d bytes", n.limit)
	}
	if _, err := n.writer.Write(data); err != nil {
		return errors.New("write normalized n8n record failed")
	}
	n.size += int64(len(data))
	return nil
}

func (n *recordNormalizer) rewind(offset int64) error {
	if err := n.writer.Flush(); err != nil {
		return errors.New("write normalized n8n record failed")
	}
	if err := n.output.Truncate(offset); err != nil {
		return errors.New("truncate normalized n8n record failed")
	}
	if _, err := n.output.Seek(offset, io.SeekStart); err != nil {
		return errors.New("seek normalized n8n record failed")
	}
	n.writer.Reset(n.output)
	n.size = offset
	return nil
}

func sumHash(h hash.Hash) (result [sha256.Size]byte) { h.Sum(result[:0]); return }

func (n *recordNormalizer) value(depth int, capture *scalarCapture) ([sha256.Size]byte, error) {
	if depth > maxJSONNestingDepth {
		return [sha256.Size]byte{}, errors.New("JSON nesting limit exceeded")
	}
	if err := n.scanner.space(); err != nil {
		return [sha256.Size]byte{}, err
	}
	peek, err := n.scanner.reader.Peek(1)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	switch peek[0] {
	case '{':
		return n.object(depth)
	case '[':
		return n.array(depth)
	case '"':
		if capture != nil {
			capture.kind = '"'
		}
		return n.stringValue(capture, nil)
	default:
		h := sha256.New()
		_, _ = h.Write([]byte("scalar\x00"))
		var single [1]byte
		read := func() (byte, error) {
			b, err := n.scanner.reader.ReadByte()
			if err != nil {
				return 0, err
			}
			single[0] = b
			if err := n.write(single[:]); err != nil {
				return 0, err
			}
			_, _ = h.Write(single[:])
			if capture != nil {
				capture.feed(single[:])
			}
			return b, nil
		}
		var parseErr error
		switch peek[0] {
		case 't':
			parseErr = scanLiteral("true", read)
		case 'f':
			parseErr = scanLiteral("false", read)
		case 'n':
			parseErr = scanLiteral("null", read)
		default:
			if capture != nil {
				capture.kind = '0'
			}
			parseErr = scanJSONNumber(n.scanner.reader, read)
		}
		return sumHash(h), parseErr
	}
}

func (n *recordNormalizer) object(depth int) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('{'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'{'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	sorter := &memberSorter{owner: n}
	n.sorters = append(n.sorters, sorter)
	defer func() { n.members -= len(sorter.memory); n.sorters = n.sorters[:len(n.sorters)-1] }()
	written := 0
	memberCount := 0
	for !n.scanner.nextIs('}') {
		start := n.size
		if written > 0 {
			if err := n.write([]byte{','}); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		key := &keyCapture{}
		keyDigest, err := n.stringValue(nil, key)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		if err := n.scanner.expect(':'); err != nil {
			return [sha256.Size]byte{}, err
		}
		if err := n.write([]byte{':'}); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.memberLimit > 0 && memberCount >= n.memberLimit {
			return [sha256.Size]byte{}, fmt.Errorf("n8n object exceeds the %d object member limit", n.memberLimit)
		}
		memberCount++
		if n.scrub && key.secret.secret() {
			if err := n.rewind(start); err != nil {
				return [sha256.Size]byte{}, err
			}
			if _, _, err := n.scanner.valueWithObjectLimit(0, false, n.memberLimit); err != nil {
				return [sha256.Size]byte{}, err
			}
		} else {
			var capture *scalarCapture
			name := key.name()
			if depth == 0 {
				capture = newScalarCapture(name)
			}
			child, err := n.value(depth+1, capture)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			if err := sorter.add(memberDigest{key: keyDigest, value: child}); err != nil {
				return [sha256.Size]byte{}, err
			}
			if capture != nil {
				n.takeCapture(name, capture)
			}
			written++
		}
		if n.scanner.nextIs('}') {
			break
		}
		if err := n.scanner.expect(','); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.scanner.nextIs('}') {
			return [sha256.Size]byte{}, errors.New("invalid JSON object separator")
		}
	}
	if err := n.scanner.expect('}'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'}'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sorter.digest()
}

func (n *recordNormalizer) array(depth int) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('['); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{'['}); err != nil {
		return [sha256.Size]byte{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("array\x00"))
	count := uint64(0)
	for !n.scanner.nextIs(']') {
		if count > 0 {
			if err := n.write([]byte{','}); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		child, err := n.value(depth+1, nil)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		_, _ = h.Write(child[:])
		count++
		if n.scanner.nextIs(']') {
			break
		}
		if err := n.scanner.expect(','); err != nil {
			return [sha256.Size]byte{}, err
		}
		if n.scanner.nextIs(']') {
			return [sha256.Size]byte{}, errors.New("invalid JSON array separator")
		}
	}
	if err := n.scanner.expect(']'); err != nil {
		return [sha256.Size]byte{}, err
	}
	if err := n.write([]byte{']'}); err != nil {
		return [sha256.Size]byte{}, err
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], count)
	_, _ = h.Write(size[:])
	return sumHash(h), nil
}

// Strings are decoded in chunks. Neither a giant string nor a giant property
// name is materialized as a Go string; only the emitted file and hashes grow.
//
// A \uXXXX escape of a surrogate code unit that is not half of a valid
// high-low pair has no UTF-8 encoding. It is written and hashed as its own
// six-byte escape with lowercase hex digits, so keys or values that differ
// only in such escapes stay distinct. Captures and the secret-key matcher see
// U+FFFD for it.
func (n *recordNormalizer) stringValue(capture *scalarCapture, key *keyCapture) ([sha256.Size]byte, error) {
	if err := n.scanner.expect('"'); err != nil {
		return [sha256.Size]byte{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("string\x00"))
	emit := func(encoded, decoded []byte) error {
		if err := n.write(encoded); err != nil {
			return err
		}
		_, _ = h.Write(encoded)
		if capture != nil {
			capture.feed(decoded)
		}
		if key != nil {
			key.feed(decoded)
		}
		return nil
	}
	if err := emit([]byte{'"'}, nil); err != nil {
		return [sha256.Size]byte{}, err
	}
	var decoded [utf8.UTFMax]byte
	var encoded [6]byte
	for {
		if n.checkBytes <= 0 {
			if err := n.cancelled(); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		if _, err := n.scanner.reader.Peek(1); err != nil {
			return [sha256.Size]byte{}, err
		}
		chunk, _ := n.scanner.reader.Peek(n.scanner.reader.Buffered())
		length := 0
		for length < len(chunk) && chunk[length] >= 0x20 && chunk[length] < utf8.RuneSelf && chunk[length] != '"' && chunk[length] != '\\' {
			length++
		}
		if length > 0 {
			if err := emit(chunk[:length], chunk[:length]); err != nil {
				return [sha256.Size]byte{}, err
			}
			_, _ = n.scanner.reader.Discard(length)
			continue
		}
		peek, _ := n.scanner.reader.Peek(1)
		// lone is the code unit of an unpaired surrogate escape, otherwise zero.
		var r, lone rune
		if peek[0] == '"' {
			_, _ = n.scanner.reader.ReadByte()
			if err := emit([]byte{'"'}, nil); err != nil {
				return [sha256.Size]byte{}, err
			}
			return sumHash(h), nil
		} else if peek[0] == '\\' {
			_, _ = n.scanner.reader.ReadByte()
			escape, err := n.scanner.reader.ReadByte()
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			switch escape {
			case '"', '\\', '/':
				r = rune(escape)
			case 'b':
				r = '\b'
			case 'f':
				r = '\f'
			case 'n':
				r = '\n'
			case 'r':
				r = '\r'
			case 't':
				r = '\t'
			case 'u':
				r, err = readHexRune(n.scanner.reader)
				if err != nil {
					return [sha256.Size]byte{}, err
				}
				if utf16.IsSurrogate(r) {
					second, err := n.scanner.reader.Peek(6)
					if err == nil && r >= 0xd800 && r <= 0xdbff && second[0] == '\\' && second[1] == 'u' {
						low, parseErr := parseHexRune(second[2:])
						if parseErr == nil && low >= 0xdc00 && low <= 0xdfff {
							_, _ = n.scanner.reader.Discard(6)
							r = utf16.DecodeRune(r, low)
						} else {
							lone, r = r, utf8.RuneError
						}
					} else {
						lone, r = r, utf8.RuneError
					}
				}
			default:
				return [sha256.Size]byte{}, errors.New("invalid JSON escape")
			}
		} else {
			var err error
			r, _, err = n.scanner.reader.ReadRune()
			if err != nil {
				return [sha256.Size]byte{}, err
			}
			if r < 0x20 {
				return [sha256.Size]byte{}, errors.New("invalid control byte in JSON string")
			}
		}
		decodedSize := utf8.EncodeRune(decoded[:], r)
		var output []byte
		switch r {
		case '"', '\\':
			encoded[0], encoded[1] = '\\', byte(r)
			output = encoded[:2]
		case '\b':
			output = []byte(`\b`)
		case '\f':
			output = []byte(`\f`)
		case '\n':
			output = []byte(`\n`)
		case '\r':
			output = []byte(`\r`)
		case '\t':
			output = []byte(`\t`)
		default:
			unit, escaped := r, r < 0x20 || r == 0x2028 || r == 0x2029
			if lone != 0 {
				unit, escaped = lone, true
			}
			if escaped {
				const digits = "0123456789abcdef"
				encoded = [6]byte{'\\', 'u', digits[(unit>>12)&15], digits[(unit>>8)&15], digits[(unit>>4)&15], digits[unit&15]}
				output = encoded[:]
			} else {
				output = decoded[:decodedSize]
			}
		}
		if err := emit(output, decoded[:decodedSize]); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
}

func readHexRune(reader *bufio.Reader) (rune, error) {
	var text [4]byte
	if _, err := io.ReadFull(reader, text[:]); err != nil {
		return 0, err
	}
	return parseHexRune(text[:])
}

func parseHexRune(text []byte) (rune, error) {
	var result rune
	for _, b := range text {
		result <<= 4
		switch {
		case b >= '0' && b <= '9':
			result |= rune(b - '0')
		case b >= 'a' && b <= 'f':
			result |= rune(b - 'a' + 10)
		case b >= 'A' && b <= 'F':
			result |= rune(b - 'A' + 10)
		default:
			return 0, errors.New("invalid JSON unicode escape")
		}
	}
	return result, nil
}

type scalarCapture struct {
	identity hash.Hash
	preview  [128]byte
	size     int
	overflow bool
	kind     byte
	nonempty bool
}

func newScalarCapture(name string) *scalarCapture {
	i, t := slices.Index(identityFields, name), slices.Index(timestampFields, name)
	if i < 0 && t < 0 && name != "status" {
		return nil
	}
	c := &scalarCapture{}
	if i >= 0 {
		c.identity = sha256.New()
		_, _ = c.identity.Write([]byte(name + ":"))
	}
	return c
}

func (c *scalarCapture) feed(data []byte) {
	if len(data) > 0 {
		c.nonempty = true
	}
	if c.identity != nil {
		_, _ = c.identity.Write(data)
	}
	if len(data) > len(c.preview)-c.size {
		c.overflow = true
		return
	}
	copy(c.preview[c.size:], data)
	c.size += len(data)
}

func (n *recordNormalizer) takeCapture(name string, c *scalarCapture) {
	if name == "status" && c.kind == '"' && c.nonempty && !c.overflow {
		n.statusText = string(c.preview[:c.size])
	}
	if i := slices.Index(identityFields, name); i >= 0 && i < n.identityRank && c.identity != nil && (c.kind == '0' || c.kind == '"' && c.nonempty) {
		// Domain-separate decoded strings from numeric scalar spellings.
		_, _ = c.identity.Write([]byte{0, c.kind})
		n.identity, n.identityRank = sumHash(c.identity), i
		if name == "id" && !c.overflow {
			n.identityText = string(c.preview[:c.size])
		}
	}
	if i := slices.Index(timestampFields, name); i >= 0 && i < n.timestampRank && !c.overflow {
		text := string(c.preview[:c.size])
		var parsed time.Time
		valid := false
		if c.kind == '"' {
			var err error
			parsed, err = time.Parse(time.RFC3339Nano, text)
			valid = err == nil
		}
		if c.kind == '0' {
			if seconds, err := json.Number(text).Int64(); err == nil {
				parsed = time.Unix(seconds, 0)
				valid = true
			}
		}
		if valid {
			parsed = parsed.UTC()
			valid = !parsed.Before(time.Unix(0, 0).UTC()) && !parsed.After(time.Now().UTC().Add(24*time.Hour))
		}
		if valid {
			n.timestamp, n.timestampRank = parsed, i
		}
	}
}

type keyCapture struct {
	text     [32]byte
	size     int
	overflow bool
	secret   secretKeyTracker
}

func (k *keyCapture) feed(data []byte) {
	if len(data) > len(k.text)-k.size {
		k.overflow = true
	} else if !k.overflow {
		copy(k.text[k.size:], data)
		k.size += len(data)
	}
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		k.secret.feed(r)
		data = data[size:]
	}
}
func (k *keyCapture) name() string {
	if k.overflow {
		return ""
	}
	return string(k.text[:k.size])
}

// Matches credentialSecretKey without retaining an arbitrarily long key.
type secretKeyTracker struct {
	started, pendingSpace, matched bool
	suffix                         [14]rune
	size                           int
	exact                          [15]rune
	exactSize                      int
}

func (k *secretKeyTracker) normalized(r rune) {
	// Fold full-width ASCII and remove format controls so ordinary secret field
	// names cannot be disguised without adding a normalization dependency.
	if r >= '\uFF01' && r <= '\uFF5E' {
		r -= '\uFEE0'
	}
	if unicode.In(r, unicode.Cf) {
		return
	}
	if r == '_' || r == '-' || r == '.' {
		return
	}
	r = unicode.ToLower(r)
	if k.exactSize < len(k.exact) {
		k.exact[k.exactSize] = r
		k.exactSize++
	}
	if k.size == len(k.suffix) {
		copy(k.suffix[:], k.suffix[1:])
		k.size--
	}
	k.suffix[k.size] = r
	k.size++
	for _, word := range []string{
		"secret", "password", "token", "key", "pass", "pwd", "auth",
		"bearer", "cookie", "session", "signature", "certificate",
		"credential", "private",
	} {
		if len(word) > k.size {
			continue
		}
		match := true
		for i := range word {
			if k.suffix[k.size-len(word)+i] != rune(word[i]) {
				match = false
				break
			}
		}
		if match {
			k.matched = true
		}
	}
}
func (k *secretKeyTracker) feed(r rune) {
	if unicode.IsSpace(r) {
		if k.started {
			k.pendingSpace = true
		}
		return
	}
	if k.pendingSpace {
		k.normalized(' ')
		k.pendingSpace = false
	}
	k.started = true
	k.normalized(r)
}
func (k *secretKeyTracker) secret() bool {
	text := string(k.exact[:k.exactSize])
	return k.matched || text == "data" || text == "credentialdata"
}

type memberDigest struct{ key, value [sha256.Size]byte }
type memberSorter struct {
	owner         *recordNormalizer
	memory        []memberDigest
	runs          [64]string
	overflowCount uint64
	overflowSum   [sha256.Size]byte
	overflowXOR   [sha256.Size]byte
}

func (s *memberSorter) add(member memberDigest) error {
	if s.owner.allowOversized {
		s.addOverflowMember(member)
	}
	if s.owner.oversized {
		return nil
	}
	// One digest per open object is unavoidable until that object closes. Keep
	// the shared spill budget for additional digests so deep nesting cannot
	// force a singleton spill for nearly every parsed member.
	if s.owner.members >= memberHashBudget+len(s.owner.sorters) {
		var largest *memberSorter
		for _, candidate := range s.owner.sorters {
			if largest == nil || len(candidate.memory) > len(largest.memory) {
				largest = candidate
			}
		}
		if largest != nil {
			if err := largest.spill(); err != nil {
				return err
			}
			if s.owner.oversized {
				return nil
			}
		}
	}
	s.memory = append(s.memory, member)
	s.owner.members++
	return nil
}

func (s *memberSorter) addOverflowMember(member memberDigest) {
	h := sha256.New()
	_, _ = h.Write([]byte("member\x00"))
	_, _ = h.Write(member.key[:])
	_, _ = h.Write(member.value[:])
	digest := sumHash(h)
	s.overflowCount++
	carry := uint16(0)
	for i := len(digest) - 1; i >= 0; i-- {
		total := uint16(s.overflowSum[i]) + uint16(digest[i]) + carry
		s.overflowSum[i] = byte(total)
		carry = total >> 8
		s.overflowXOR[i] ^= digest[i]
	}
}

func (s *memberSorter) overflowDigest() [sha256.Size]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("object-overflow\x00"))
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], s.overflowCount)
	_, _ = h.Write(count[:])
	_, _ = h.Write(s.overflowSum[:])
	_, _ = h.Write(s.overflowXOR[:])
	return sumHash(h)
}

func sortMembers(members []memberDigest) error {
	slices.SortFunc(members, func(a, b memberDigest) int { return bytes.Compare(a.key[:], b.key[:]) })
	for i := 1; i < len(members); i++ {
		if members[i-1].key == members[i].key {
			return errors.New("duplicate JSON object key")
		}
	}
	return nil
}

func (s *memberSorter) spill() error {
	if len(s.memory) == 0 {
		return nil
	}
	if err := s.owner.cancelled(); err != nil {
		return err
	}
	if err := sortMembers(s.memory); err != nil {
		return err
	}
	f, err := s.owner.scratch()
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, m := range s.memory {
		if _, err = w.Write(m.key[:]); err != nil {
			break
		}
		if _, err = w.Write(m.value[:]); err != nil {
			break
		}
	}
	flushErr, closeErr := w.Flush(), f.Close()
	if err != nil || flushErr != nil || closeErr != nil {
		return errors.New("write temporary n8n member hashes failed")
	}
	s.owner.members -= len(s.memory)
	s.memory = nil
	path := f.Name()
	for level := range s.runs {
		if s.runs[level] == "" {
			s.runs[level] = path
			return nil
		}
		path, err = s.merge(s.runs[level], path)
		if err != nil {
			return err
		}
		s.runs[level] = ""
	}
	return errors.New("n8n member hash sort overflow")
}

func readMember(r io.Reader) (m memberDigest, err error) {
	var raw [64]byte
	if _, err = io.ReadFull(r, raw[:]); err != nil {
		return
	}
	copy(m.key[:], raw[:32])
	copy(m.value[:], raw[32:])
	return
}

func (s *memberSorter) merge(leftPath, rightPath string) (string, error) {
	if err := s.owner.cancelled(); err != nil {
		return "", err
	}
	left, err := os.Open(leftPath)
	if err != nil {
		return "", errors.New("open temporary n8n member hashes failed")
	}
	defer left.Close()
	right, err := os.Open(rightPath)
	if err != nil {
		return "", errors.New("open temporary n8n member hashes failed")
	}
	defer right.Close()
	out, err := s.owner.scratch()
	if err != nil {
		return "", err
	}
	w := bufio.NewWriter(out)
	lr, rr := bufio.NewReader(left), bufio.NewReader(right)
	a, ae := readMember(lr)
	b, be := readMember(rr)
	merged := 0
	for ae == nil || be == nil {
		if merged&1023 == 0 {
			if err := s.owner.cancelled(); err != nil {
				_ = out.Close()
				return "", err
			}
		}
		merged++
		if ae != nil && !errors.Is(ae, io.EOF) || be != nil && !errors.Is(be, io.EOF) {
			_ = out.Close()
			return "", errors.New("read temporary n8n member hashes failed")
		}
		var chosen memberDigest
		if ae == nil && be == nil && a.key == b.key {
			_ = out.Close()
			return "", errors.New("duplicate JSON object key")
		}
		if be != nil || ae == nil && bytes.Compare(a.key[:], b.key[:]) < 0 {
			chosen = a
			a, ae = readMember(lr)
		} else {
			chosen = b
			b, be = readMember(rr)
		}
		if _, err = w.Write(chosen.key[:]); err == nil {
			_, err = w.Write(chosen.value[:])
		}
		if err != nil {
			_ = out.Close()
			return "", errors.New("write temporary n8n member hashes failed")
		}
	}
	flushErr, closeErr := w.Flush(), out.Close()
	if flushErr != nil || closeErr != nil || !errors.Is(ae, io.EOF) || !errors.Is(be, io.EOF) {
		return "", errors.New("merge temporary n8n member hashes failed")
	}
	if left.Close() != nil || right.Close() != nil {
		return "", errors.New("close temporary n8n member hashes failed")
	}
	if err := os.Remove(leftPath); err != nil {
		return "", errors.New("remove temporary n8n member hashes failed")
	}
	delete(s.owner.paths, leftPath)
	if err := os.Remove(rightPath); err != nil {
		return "", errors.New("remove temporary n8n member hashes failed")
	}
	delete(s.owner.paths, rightPath)
	return out.Name(), nil
}

func (s *memberSorter) digest() ([sha256.Size]byte, error) {
	if err := s.owner.cancelled(); err != nil {
		return [sha256.Size]byte{}, err
	}
	if s.owner.allowOversized {
		digest := s.overflowDigest()
		if !s.owner.oversized {
			if _, err := s.sortedDigest(); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		return digest, nil
	}
	return s.sortedDigest()
}

func (s *memberSorter) sortedDigest() ([sha256.Size]byte, error) {
	h := sha256.New()
	_, _ = h.Write([]byte("object\x00"))
	hasRuns := false
	for _, path := range s.runs {
		hasRuns = hasRuns || path != ""
	}
	if !hasRuns {
		if err := sortMembers(s.memory); err != nil {
			return [sha256.Size]byte{}, err
		}
		for _, member := range s.memory {
			_, _ = h.Write(member.key[:])
			_, _ = h.Write(member.value[:])
		}
		return sumHash(h), nil
	}
	if err := s.spill(); err != nil {
		return [sha256.Size]byte{}, err
	}
	var path string
	for i, run := range s.runs {
		if run == "" {
			continue
		}
		if path == "" {
			path = run
		} else {
			var err error
			path, err = s.merge(path, run)
			if err != nil {
				return [sha256.Size]byte{}, err
			}
		}
		s.runs[i] = ""
	}
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, errors.New("open temporary n8n member hashes failed")
	}
	_, err = io.Copy(h, contextReader{ctx: s.owner.ctx, r: f})
	closeErr := f.Close()
	removeErr := os.Remove(path)
	if removeErr == nil {
		delete(s.owner.paths, path)
	}
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("consume temporary n8n member hashes failed: %w", err)
	}
	if closeErr != nil || removeErr != nil {
		return [sha256.Size]byte{}, errors.New("consume temporary n8n member hashes failed")
	}
	return sumHash(h), nil
}
