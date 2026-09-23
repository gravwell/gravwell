package servicenow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes int64 = 32 << 20

var errRetryAfterOverflow = errors.New("ServiceNow Retry-After exceeds the supported duration")

type AuthSecret struct{ Mode, ClientID, ClientSecret, Username, Password, BearerToken string }
type Record struct {
	Raw       json.RawMessage
	ID        string
	Timestamp time.Time
}
type Page struct {
	Records []Record
	HasNext bool
	NextURL string
}
type StatusError struct {
	Code              int
	Source            string
	Endpoint          bool
	Unavailable       bool
	IllegalParameters bool
}

func (e *StatusError) Error() string {
	if e.Endpoint {
		return fmt.Sprintf("ServiceNow REST API returned HTTP %d for endpoint profile %s", e.Code, e.Source)
	}
	return fmt.Sprintf("ServiceNow Table API returned HTTP %d for table %s", e.Code, e.Source)
}
func IsUnavailable(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && (status.Unavailable || status.Code == 403 || status.Code == 404)
}

// IsIllegalParameters reports the precise ServiceNow HTTP 400 response used by
// collection endpoints when a persisted terminal offset is no longer valid.
// Fetcher implementations use this classification to reset only that offset;
// other bad requests remain hard failures.
func IsIllegalParameters(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code == http.StatusBadRequest && status.IllegalParameters
}

type Client struct {
	instance, secretPath string
	http                 *http.Client
	retries              int
	mu                   sync.Mutex
	token                string
	expires              time.Time
	secretSum            [32]byte
	rateMu               sync.Mutex
	nextRequest          time.Time
	requestEvery         time.Duration
}

func NewClient(instance, secretPath string, timeout time.Duration, retries, requestsPerMinute int, transport http.RoundTripper) *Client {
	if transport == nil {
		transport = http.DefaultTransport
	}
	if requestsPerMinute < 1 {
		requestsPerMinute = 6
	}
	return &Client{
		instance:   strings.TrimRight(instance, "/"),
		secretPath: secretPath,
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(next *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if !strings.EqualFold(origin.Scheme, next.URL.Scheme) || !strings.EqualFold(origin.Host, next.URL.Host) || origin.Path != next.URL.Path {
					return http.ErrUseLastResponse
				}
				if len(via) >= 10 {
					return errors.New("ServiceNow redirect limit exceeded")
				}
				return nil
			},
		},
		retries:      retries,
		requestEvery: time.Minute / time.Duration(requestsPerMinute),
	}
}

func loadSecret(path string) (AuthSecret, [32]byte, error) {
	var disk struct {
		Mode         string `json:"mode"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		BearerToken  string `json:"bearer_token"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AuthSecret{}, [32]byte{}, fmt.Errorf("read ServiceNow secret file: %w", err)
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return AuthSecret{}, [32]byte{}, fmt.Errorf("decode ServiceNow secret file: %w", err)
	}
	s := AuthSecret{disk.Mode, disk.ClientID, disk.ClientSecret, disk.Username, disk.Password, disk.BearerToken}
	s.Mode = strings.ToLower(strings.TrimSpace(s.Mode))
	if s.Mode == "" {
		s.Mode = "oauth-client-credentials"
	}
	switch s.Mode {
	case "oauth-client-credentials":
		if s.ClientID == "" || s.ClientSecret == "" {
			return s, [32]byte{}, errors.New("OAuth client ID and secret are required")
		}
		if s.Username != "" || s.Password != "" || s.BearerToken != "" {
			return s, [32]byte{}, errors.New("OAuth mode cannot include basic or bearer credentials")
		}
	case "basic":
		if s.Username == "" || s.Password == "" {
			return s, [32]byte{}, errors.New("basic username and password are required")
		}
		if s.ClientID != "" || s.ClientSecret != "" || s.BearerToken != "" {
			return s, [32]byte{}, errors.New("basic mode cannot include OAuth or bearer credentials")
		}
	case "bearer":
		if s.BearerToken == "" {
			return s, [32]byte{}, errors.New("bearer token is required")
		}
		if s.ClientID != "" || s.ClientSecret != "" || s.Username != "" || s.Password != "" {
			return s, [32]byte{}, errors.New("bearer mode cannot include OAuth or basic credentials")
		}
	default:
		return s, [32]byte{}, fmt.Errorf("unsupported ServiceNow auth mode %q", s.Mode)
	}
	return s, sha256.Sum256(data), nil
}

func (c *Client) accessToken(ctx context.Context, s AuthSecret, sum [32]byte) (string, error) {
	if s.Mode == "bearer" {
		return s.BearerToken, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.secretSum == sum && time.Now().Before(c.expires.Add(-30*time.Second)) {
		return c.token, nil
	}
	values := url.Values{"grant_type": {"client_credentials"}, "client_id": {s.ClientID}, "client_secret": {s.ClientSecret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.instance+"/oauth_token.do", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ServiceNow token endpoint returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", errors.New("token response omitted access_token")
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 1800
	}
	c.token, c.expires, c.secretSum = result.AccessToken, time.Now().Add(time.Duration(result.ExpiresIn)*time.Second), sum
	return c.token, nil
}

func (c *Client) FetchPage(ctx context.Context, d Dataset, query string, limit, offset int) (Page, error) {
	return c.FetchPageAfter(ctx, d, query, limit, offset, "")
}

// FetchPageAfter follows a vendor-provided same-origin continuation URL for
// endpoints that do not expose an offset parameter. Table and offset-based REST
// datasets always pass an empty continuation.
func (c *Client) FetchPageAfter(ctx context.Context, d Dataset, query string, limit, offset int, continuation string) (Page, error) {
	u, err := c.continuationURL(d, query, limit, offset, continuation)
	if err != nil {
		return Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("Accept", "application/json")
	s, sum, err := loadSecret(c.secretPath)
	if err != nil {
		return Page{}, err
	}
	if s.Mode == "basic" {
		req.SetBasicAuth(s.Username, s.Password)
	} else {
		token, err := c.accessToken(ctx, s, sum)
		if err != nil {
			return Page{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var serviceNowError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &serviceNowError)
		message := strings.ToLower(strings.TrimSpace(serviceNowError.Error.Message))
		invalidTable := resp.StatusCode == http.StatusBadRequest && strings.HasPrefix(message, "invalid table")
		illegalParameters := resp.StatusCode == http.StatusBadRequest && message == "illegal query parameters"
		source := d.Table
		if d.REST != nil {
			source = d.Name
		}
		return Page{}, &StatusError{Code: resp.StatusCode, Source: source, Endpoint: d.REST != nil, Unavailable: invalidTable, IllegalParameters: illegalParameters}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Page{}, err
	}
	if int64(len(body)) > maxResponseBytes {
		return Page{}, fmt.Errorf("ServiceNow dataset %s response exceeds %d bytes", d.Name, maxResponseBytes)
	}
	resultPath := "result"
	idField := "sys_id"
	if d.REST != nil {
		resultPath = d.REST.ResultPath
		idField = d.REST.IDField
	}
	records, err := resultRecords(body, resultPath)
	if err != nil {
		return Page{}, fmt.Errorf("decode ServiceNow dataset %s: %w", d.Name, err)
	}
	page := Page{Records: make([]Record, 0, len(records))}
	for _, raw := range records {
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return Page{}, fmt.Errorf("compact ServiceNow dataset %s record: %w", d.Name, err)
		}
		b := append(json.RawMessage(nil), compact.Bytes()...)
		id := recordIDField(b, idField)
		if d.REST != nil && d.REST.StaticID != "" {
			if len(records) != 1 {
				return Page{}, fmt.Errorf("decode ServiceNow dataset %s: Static_ID requires a singleton result", d.Name)
			}
			id = d.REST.StaticID
		}
		page.Records = append(page.Records, Record{Raw: b, ID: id, Timestamp: recordTimestamp(b, d.Timestamp)})
	}
	if d.REST != nil && d.REST.OffsetParameter == "" {
		page.NextURL = nextLink(resp.Header.Get("Link"))
		page.HasNext = page.NextURL != ""
	} else {
		// Offset-paginated ServiceNow APIs can return a stale next link on the
		// final short page. The documented terminal condition is fewer records
		// than the requested limit; following the stale link produces HTTP 400.
		page.HasNext = len(page.Records) == limit
	}
	return page, nil
}

func (c *Client) continuationURL(d Dataset, query string, limit, offset int, continuation string) (*url.URL, error) {
	if strings.TrimSpace(continuation) == "" {
		return c.datasetURL(d, query, limit, offset)
	}
	if d.REST == nil || d.REST.OffsetParameter != "" {
		return nil, errors.New("ServiceNow continuation URL is only valid for no-offset REST endpoints")
	}
	base, err := c.datasetURL(d, query, limit, offset)
	if err != nil {
		return nil, err
	}
	next, err := url.Parse(strings.TrimSpace(continuation))
	if err != nil || !next.IsAbs() || !strings.EqualFold(base.Scheme, next.Scheme) || !strings.EqualFold(base.Host, next.Host) || next.Path != base.Path || next.User != nil || next.Fragment != "" {
		return nil, errors.New("ServiceNow continuation URL escaped the selected endpoint")
	}
	return next, nil
}

func nextLink(header string) string {
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(item, ";")
		if len(parts) < 2 {
			continue
		}
		relNext := false
		for _, parameter := range parts[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(name, "rel") && strings.EqualFold(strings.Trim(strings.TrimSpace(value), `"`), "next") {
				relNext = true
				break
			}
		}
		candidate := strings.TrimSpace(parts[0])
		if relNext && strings.HasPrefix(candidate, "<") && strings.HasSuffix(candidate, ">") {
			return strings.TrimSuffix(strings.TrimPrefix(candidate, "<"), ">")
		}
	}
	return ""
}

func (c *Client) datasetURL(d Dataset, query string, limit, offset int) (*url.URL, error) {
	path := "/api/now/table/" + url.PathEscape(d.Table)
	if d.REST != nil {
		path = d.REST.Path
	}
	u, err := url.Parse(c.instance + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if d.REST == nil {
		// Preserve stable native values for extraction, comparison, and joins.
		// Display-value expansion can turn a scalar into a per-field object and
		// makes one query contract behave differently across tenants. Operators
		// can still enrich reference values after ingestion when display labels
		// are required.
		q.Set("sysparm_display_value", "false")
		q.Set("sysparm_exclude_reference_link", "true")
		q.Set("sysparm_no_count", "true")
		q.Set("sysparm_limit", strconv.Itoa(limit))
		q.Set("sysparm_offset", strconv.Itoa(offset))
		if d.Fields != "" {
			q.Set("sysparm_fields", d.Fields)
		}
		if query != "" {
			q.Set("sysparm_query", query)
		}
	} else {
		for key, value := range d.REST.Parameters {
			q.Set(key, value)
		}
		if d.REST.LimitParameter != "" {
			q.Set(d.REST.LimitParameter, strconv.Itoa(limit))
		}
		if d.REST.OffsetParameter != "" {
			q.Set(d.REST.OffsetParameter, strconv.Itoa(offset))
		}
	}
	u.RawQuery = q.Encode()
	return u, nil
}

func resultRecords(body []byte, resultPath string) ([]json.RawMessage, error) {
	value := json.RawMessage(append([]byte(nil), body...))
	if !json.Valid(value) {
		return nil, errors.New("invalid JSON response")
	}
	for _, part := range strings.Split(resultPath, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, fmt.Errorf("result path %q crossed a non-object at %q", resultPath, part)
		}
		next, ok := object[part]
		if !ok {
			return nil, fmt.Errorf("result path %q is absent", resultPath)
		}
		value = next
	}
	trimmed := bytes.TrimSpace(value)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) == 0 {
		return nil, errors.New("empty result value")
	}
	if trimmed[0] != '[' {
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		out = append(out, append(json.RawMessage(nil), bytes.TrimSpace(value)...))
	}
	return out, nil
}

func recordID(raw []byte) string {
	return recordIDField(raw, "sys_id")
}

func recordIDField(raw []byte, field string) string {
	var o map[string]any
	if json.Unmarshal(raw, &o) == nil {
		value := nestedValue(o, field)
		if v, ok := value.(string); ok && v != "" {
			return v
		}
		if nested, ok := value.(map[string]any); ok {
			if v, ok := nested["value"].(string); ok && v != "" {
				return v
			}
		}
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:])
}
func recordTimestamp(raw []byte, field string) time.Time {
	var o map[string]any
	if json.Unmarshal(raw, &o) == nil {
		v := nestedValue(o, field)
		if m, ok := v.(map[string]any); ok {
			v = m["value"]
		}
		if s, ok := v.(string); ok {
			for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
				if t, e := time.ParseInLocation(layout, s, time.UTC); e == nil {
					return t.UTC()
				}
			}
		}
	}
	return time.Time{}
}

func nestedValue(object map[string]any, path string) any {
	var value any = object
	for _, part := range strings.Split(path, ".") {
		current, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = current[part]
	}
	return value
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	// Max-Retries is the number of retries after the initial request.
	attempts := c.retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for n := 0; n < attempts; n++ {
		if err := c.waitRate(req.Context()); err != nil {
			return nil, err
		}
		clone := req.Clone(req.Context())
		if req.Body != nil {
			body, e := io.ReadAll(req.Body)
			if e != nil {
				return nil, e
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			clone.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, e := c.http.Do(clone)
		if e == nil && resp.StatusCode != 429 && resp.StatusCode < 500 {
			return resp, nil
		}
		if e != nil {
			last = e
		} else {
			last = fmt.Errorf("retryable HTTP status %d", resp.StatusCode)
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
		}
		if n == attempts-1 {
			break
		}
		wait := time.Duration(1<<n) * time.Second
		if e == nil {
			if retryWait, present, retryErr := retryAfterDelay(resp.Header.Get("Retry-After"), time.Now()); retryErr != nil {
				return nil, retryErr
			} else if present {
				wait = retryWait
			}
		}
		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return nil, last
}

func retryAfterDelay(value string, now time.Time) (time.Duration, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		maximumSeconds := uint64((1<<63 - 1) / int64(time.Second))
		if seconds > maximumSeconds {
			return 0, true, errRetryAfterOverflow
		}
		return time.Duration(seconds) * time.Second, true, nil
	}
	if at, err := http.ParseTime(value); err == nil {
		if !at.After(now) {
			return 0, true, nil
		}
		delay := at.Sub(now)
		if delay == time.Duration(1<<63-1) && at.After(now.Add(delay)) {
			return 0, true, errRetryAfterOverflow
		}
		return delay, true, nil
	}
	return 0, false, nil
}

func (c *Client) waitRate(ctx context.Context) error {
	c.rateMu.Lock()
	now := time.Now()
	reserved := now
	if c.nextRequest.After(now) {
		reserved = c.nextRequest
	}
	c.nextRequest = reserved.Add(c.requestEvery)
	c.rateMu.Unlock()

	wait := time.Until(reserved)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
