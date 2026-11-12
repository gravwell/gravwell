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

	"golang.org/x/time/rate"
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
)

type client struct {
	baseURL     *url.URL
	credential  string
	http        *http.Client
	limiter     requestLimiter
	pageSize    int
	maxPages    int
	maxRecords  int
	recordLimit int64
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
	Records []collectedRecord
	Count   int
}

type collectedRecord struct {
	identity    [sha256.Size]byte
	fingerprint [sha256.Size]byte
	page        int
	timestamp   time.Time
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
	}, nil
}

func (c *client) collect(ctx context.Context, definition datasetDefinition) (collection, error) {
	return c.collectStreaming(ctx, definition, nil)
}

func (c *client) collectStreaming(ctx context.Context, definition datasetDefinition, onRecord func(collectedRecord, *normalizedRecord) error) (result collection, err error) {
	normalizer := newRecordNormalizer("", c.recordLimit)
	defer func() {
		err = errors.Join(err, normalizer.cleanup())
		if err != nil {
			result = collection{}
		}
	}()
	query := make(url.Values)
	if definition.RecordsField != "" {
		query.Set("limit", strconv.Itoa(c.pageSize))
	}
	if definition.Name == "executions" {
		query.Set("includeData", "false")
	}
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
		requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		response, err := c.request(requestCtx, definition, requestURL, query)
		if err != nil {
			requestErr := requestCtx.Err()
			cancel()
			if parentErr := ctx.Err(); parentErr != nil {
				return collection{}, parentErr
			}
			if requestErr != nil {
				return collection{}, fmt.Errorf("request Dataset %s page %d within 90-second request context: %w", definition.Name, page, requestErr)
			}
			return collection{}, fmt.Errorf("request Dataset %s page %d: %w", definition.Name, page, err)
		}
		cursor, err := c.consumeResponse(requestCtx, response, definition, page, c.maxRecords-result.Count, &result, onRecord, normalizer)
		requestErr := requestCtx.Err()
		cancel()
		if err != nil {
			if parentErr := ctx.Err(); parentErr != nil {
				return collection{}, parentErr
			}
			if requestErr != nil {
				return collection{}, fmt.Errorf("consume Dataset %s page %d response within 90-second request context: %w", definition.Name, page, requestErr)
			}
			return collection{}, fmt.Errorf("decode Dataset %s page %d: %w", definition.Name, page, err)
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

func (c *client) consumeResponse(ctx context.Context, resp *http.Response, definition datasetDefinition, page, remaining int, result *collection, onRecord func(collectedRecord, *normalizedRecord) error, normalizer *recordNormalizer) (string, error) {
	defer resp.Body.Close()
	scanner := newResponseScanner(resp.Body)
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
			raw, first, err := scanner.value(64*1024, true)
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
				cursor = strings.TrimSpace(cursor)
				if cursor == "" {
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
	record := collectedRecord{identity: body.identity, fingerprint: body.fingerprint, page: page, timestamp: body.timestamp}
	result.Count++
	if onRecord != nil {
		return onRecord(record, body)
	}
	result.Records = append(result.Records, record)
	return nil
}

// responseScanner validates JSON incrementally. Captured values are bounded by
// Gravwell's actual maximum entry size before any byte slice is grown; ignored
// response fields are validated and discarded without accumulating the body.
type responseScanner struct{ reader *bufio.Reader }

func newResponseScanner(reader io.Reader) *responseScanner {
	return &responseScanner{reader: bufio.NewReader(reader)}
}

func (s *responseScanner) space() error {
	for {
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
	for {
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
	return s.valueWithObjectMembers(limit, capture, nil, 0)
}

// valueWithObjectMembers applies the aggregate object-member bound while a
// scrubbed value is validated and discarded without retaining secret bytes.
func (s *responseScanner) valueWithObjectMembers(limit int64, capture bool, memberCount *int, memberLimit int) ([]byte, byte, error) {
	if err := s.space(); err != nil {
		return nil, 0, err
	}
	var output bytes.Buffer
	var out *bytes.Buffer
	if capture {
		out = &output
	}
	var consumed int64
	first, err := scanJSONValue(s.reader, out, &consumed, limit, 0, memberCount, memberLimit)
	if err != nil {
		return nil, first, err
	}
	return output.Bytes(), first, nil
}

func scanJSONValue(reader *bufio.Reader, output *bytes.Buffer, consumed *int64, limit int64, depth int, memberCount *int, memberLimit int) (byte, error) {
	if depth > 10_000 {
		return 0, errors.New("JSON nesting limit exceeded")
	}
	read := func() (byte, error) {
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
		if level > 10_000 {
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
					if memberCount != nil {
						if *memberCount >= memberLimit {
							return first, fmt.Errorf("n8n record exceeds the %d object member limit", memberLimit)
						}
						*memberCount++
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
