package restpoller

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
	maximumResponseBody = 64 << 20
	maximumRetries      = 4
)

type Client struct {
	baseURL    *url.URL
	credential string
	http       *http.Client
	limiter    *rate.Limiter
	pageSize   int
	maxPages   int
}

type Collection struct {
	Records       []json.RawMessage
	PersistCursor string
}

func NewClient(baseURL, credential string, requestsPerMinute, pageSize, maxPages int, hc *http.Client) (*Client, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		return nil, errors.New("invalid base URL")
	}
	if requestsPerMinute < 1 || pageSize < 1 || maxPages < 1 {
		return nil, errors.New("rate and pagination limits must be positive")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	copyHTTP := *hc
	copyHTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	hc = &copyHTTP
	return &Client{
		baseURL: base, credential: credential, http: hc,
		limiter:  rate.NewLimiter(rate.Every(time.Minute/time.Duration(requestsPerMinute)), requestsPerMinute),
		pageSize: pageSize, maxPages: maxPages,
	}, nil
}

func (c *Client) Collect(ctx context.Context, definition Definition, query url.Values, initialCursor, suiteQL string) (Collection, error) {
	if query == nil {
		query = make(url.Values)
	}
	if definition.PageSizeQuery != "" && query.Get(definition.PageSizeQuery) == "" {
		query.Set(definition.PageSizeQuery, strconv.Itoa(c.pageSize))
	}
	requestURL, err := c.endpoint(definition.Path)
	if err != nil {
		return Collection{}, err
	}
	if definition.PersistCursor && initialCursor != "" {
		if definition.Pagination == paginationLink {
			requestURL, err = c.validContinuation(initialCursor)
			if err != nil {
				return Collection{}, err
			}
		} else if definition.CursorQuery != "" {
			query.Set(definition.CursorQuery, initialCursor)
		}
	}
	offset := 0
	if definition.OffsetQuery != "" {
		query.Set(definition.OffsetQuery, "0")
	}
	var collection Collection
	var collectedBytes int
	seen := make(map[string]bool)
	for page := 1; page <= c.maxPages; page++ {
		requestKey := requestURL.String() + "?" + query.Encode()
		if seen[requestKey] {
			return Collection{}, errors.New("repeated pagination cursor")
		}
		seen[requestKey] = true
		response, err := c.request(ctx, definition, requestURL, query, suiteQL)
		if err != nil {
			return Collection{}, err
		}
		records, err := extractRecords(response.Body, definition.RecordsField)
		if err != nil {
			return Collection{}, fmt.Errorf("decode %s/%s page %d: %w", definition.Product, definition.Name, page, err)
		}
		if len(collection.Records)+len(records) > 100000 {
			return Collection{}, errors.New("collection exceeds 100000 records; narrow its query window")
		}
		for _, record := range records {
			if len(record) > 8<<20 {
				return Collection{}, errors.New("logical record exceeds 8 MiB")
			}
			collectedBytes += len(record)
			if collectedBytes > 64<<20 {
				return Collection{}, errors.New("collection exceeds 64 MiB; narrow its query window")
			}
		}
		collection.Records = append(collection.Records, records...)
		switch definition.Pagination {
		case paginationNone:
			return collection, nil
		case paginationLink:
			next := parseNextLink(response.Header.Get("Link"))
			if definition.PersistCursor && next != "" {
				collection.PersistCursor = next
			}
			if next == "" {
				return collection, nil
			}
			requestURL, err = c.validContinuation(next)
			if err != nil {
				return Collection{}, err
			}
		case paginationCursor:
			cursor := stringAt(response.Body, definition.CursorField)
			if definition.PersistCursor && cursor != "" {
				collection.PersistCursor = cursor
			}
			if cursor == "" {
				return collection, nil
			}
			query.Set(definition.CursorQuery, cursor)
		case paginationOffset:
			if !boolAt(response.Body, definition.HasMoreField) {
				return collection, nil
			}
			if len(records) == 0 {
				return Collection{}, errors.New("empty page declares more results")
			}
			offset += c.pageSize
			query.Set(definition.OffsetQuery, strconv.Itoa(offset))
		default:
			return Collection{}, fmt.Errorf("unsupported pagination kind %q", definition.Pagination)
		}
	}
	return Collection{}, fmt.Errorf("%s/%s exceeded Max-Pages=%d", definition.Product, definition.Name, c.maxPages)
}

type responsePage struct {
	Body   []byte
	Header http.Header
}

func (c *Client) request(ctx context.Context, definition Definition, requestURL *url.URL, query url.Values, suiteQL string) (responsePage, error) {
	for attempt := 0; attempt <= maximumRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return responsePage{}, err
		}
		u := *requestURL
		effectiveQuery := u.Query()
		for key, values := range query {
			effectiveQuery[key] = append([]string(nil), values...)
		}
		u.RawQuery = effectiveQuery.Encode()
		var body io.Reader
		switch definition.RequestBody {
		case "suiteql-file":
			encoded, err := json.Marshal(map[string]string{"q": suiteQL})
			if err != nil {
				return responsePage{}, err
			}
			body = bytes.NewReader(encoded)
		case "":
		case "{}":
			body = strings.NewReader("{}")
		default:
			return responsePage{}, errors.New("unsupported request body contract")
		}
		req, err := http.NewRequestWithContext(ctx, definition.Method, u.String(), body)
		if err != nil {
			return responsePage{}, err
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if definition.Product == "claude" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		if definition.Product == "netsuite" {
			req.Header.Set("Prefer", "transient")
		}
		c.authorize(req, definition)
		resp, err := c.http.Do(req)
		if err != nil {
			return responsePage{}, err
		}
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, maximumResponseBody+1))
		resp.Body.Close()
		if readErr != nil {
			return responsePage{}, readErr
		}
		if len(payload) > maximumResponseBody {
			return responsePage{}, errors.New("response exceeds 64 MiB limit")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if !json.Valid(payload) {
				return responsePage{}, errors.New("successful response is not valid JSON")
			}
			if raw := rawAt(payload, "error"); len(raw) > 0 && string(raw) != "null" {
				return responsePage{}, errors.New("vendor returned application error")
			}
			return responsePage{Body: payload, Header: resp.Header.Clone()}, nil
		}
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return responsePage{}, fmt.Errorf("%s %s failed with HTTP %d", definition.Method, safeURL(u.String()), resp.StatusCode)
		}
		if attempt == maximumRetries {
			break
		}
		delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return responsePage{}, ctx.Err()
		case <-timer.C:
		}
	}
	return responsePage{}, fmt.Errorf("%s %s exhausted retries", definition.Method, safeURL(requestURL.String()))
}

func (c *Client) authorize(req *http.Request, definition Definition) {
	switch definition.Auth {
	case authNone:
	case authBearer:
		req.Header.Set("Authorization", "Bearer "+c.credential)
	case authBasicUser:
		req.SetBasicAuth(c.credential, "X")
	case authHeader:
		req.Header.Set(definition.AuthHeader, definition.AuthPrefix+c.credential)
	}
}

func (c *Client) endpoint(path string) (*url.URL, error) {
	result, err := url.Parse(strings.TrimRight(c.baseURL.String(), "/") + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, err
	}
	return c.validContinuation(result.String())
}

func (c *Client) validContinuation(raw string) (*url.URL, error) {
	result, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if !result.IsAbs() {
		result = c.baseURL.ResolveReference(result)
	}
	if result.Scheme != c.baseURL.Scheme || !strings.EqualFold(result.Host, c.baseURL.Host) || result.User != nil {
		return nil, errors.New("pagination continuation escaped the configured API origin")
	}
	return result, nil
}

func extractRecords(body []byte, field string) ([]json.RawMessage, error) {
	if field == "" {
		var array []json.RawMessage
		if err := json.Unmarshal(body, &array); err == nil {
			return array, nil
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(body, &object); err != nil {
			return nil, err
		}
		return []json.RawMessage{append(json.RawMessage(nil), body...)}, nil
	}
	raw := rawAt(body, field)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("required response array %s is missing", field)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("field %s is not an array", field)
	}
	return records, nil
}

func rawAt(body []byte, path string) json.RawMessage {
	var value json.RawMessage = body
	for _, part := range strings.Split(path, ".") {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil
		}
		value = object[part]
		if len(value) == 0 {
			return nil
		}
	}
	return value
}

func stringAt(body []byte, path string) string {
	raw := rawAt(body, path)
	var value string
	_ = json.Unmarshal(raw, &value)
	return strings.TrimSpace(value)
}

func boolAt(body []byte, path string) bool {
	raw := rawAt(body, path)
	var value bool
	_ = json.Unmarshal(raw, &value)
	return value
}

func parseNextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 || !strings.Contains(strings.ToLower(strings.Join(segments[1:], ";")), `rel="next"`) {
			continue
		}
		candidate := strings.TrimSpace(segments[0])
		return strings.TrimSuffix(strings.TrimPrefix(candidate, "<"), ">")
	}
	return ""
}

func retryDelay(raw string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	query := u.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "key") {
			query.Set(key, "REDACTED")
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}
