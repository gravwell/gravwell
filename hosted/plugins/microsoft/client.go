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

	"golang.org/x/time/rate"
)

const maximumResponseBody = 64 << 20

type FetchedRecord struct {
	Raw       []byte
	ID        string
	Timestamp time.Time
}

type cachedToken struct {
	Value   string
	Expires time.Time
}

type Client struct {
	conf    *Config
	http    *http.Client
	limiter *rate.Limiter

	mu               sync.Mutex
	tokens           map[string]cachedToken
	cooldowns        map[string]retryEligibility
	persistCooldowns func(map[string]retryEligibility) error
}

func NewClient(conf *Config, hc *http.Client) (*Client, error) {
	if conf == nil {
		return nil, errors.New("nil Microsoft config")
	}
	if conf.Requests_Per_Minute < 1 {
		return nil, errors.New("Microsoft request rate must be positive")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	// Each request must pass through our origin and pacing checks. Neither
	// OAuth POST bodies nor bearer-authenticated requests may follow redirects.
	clientCopy := *hc
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	hc = &clientCopy
	return &Client{
		conf: conf, http: hc,
		limiter:   rate.NewLimiter(rate.Every(time.Minute/time.Duration(conf.Requests_Per_Minute)), 1),
		tokens:    make(map[string]cachedToken),
		cooldowns: make(map[string]retryEligibility),
	}, nil
}

func (c *Client) Fetch(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string) ([]FetchedRecord, error) {
	ctx = withFetchBudget(ctx)
	ctx = withRetryWaitDeadline(ctx)
	switch dataset.Kind {
	case KindODataGET:
		return c.fetchOData(ctx, dataset, start, end, subscriptionID)
	case KindObjectGET:
		return c.fetchObject(ctx, dataset, subscriptionID)
	case KindAzureActivity:
		return c.fetchAzureActivity(ctx, dataset, start, end, subscriptionID)
	case KindResourceGraph:
		return c.fetchResourceGraph(ctx, dataset)
	case KindPolicyStates:
		return c.fetchPolicyStates(ctx, dataset, subscriptionID)
	case KindFabricActivity:
		return c.fetchFabricActivity(ctx, dataset, start, end)
	case KindAdvancedHunting:
		return c.fetchAdvancedHunting(ctx, dataset, start, end)
	case KindParentChild:
		return c.fetchParentChild(ctx, dataset, start, end, subscriptionID)
	default:
		return nil, fmt.Errorf("unsupported dataset kind %q", dataset.Kind)
	}
}

func (c *Client) fetchObject(ctx context.Context, dataset Dataset, subscriptionID string) ([]FetchedRecord, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return nil, err
	}
	body, err := c.request(ctx, http.MethodGet, c.conf.Base(dataset.Service)+path, dataset.Scope, nil)
	if err != nil {
		return nil, err
	}
	record, err := decodeRecord(dataset, json.RawMessage(body))
	if err != nil {
		return nil, err
	}
	return []FetchedRecord{record}, nil
}

func (c *Client) fetchOData(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string) ([]FetchedRecord, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.conf.Base(dataset.Service) + path)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	if dataset.Service == ServiceGraph && !dataset.ServerPageSize && query.Get("$top") == "" {
		query.Set("$top", strconv.Itoa(c.conf.Page_Size))
	}
	if dataset.Service == ServiceGraph && dataset.TimeField != "" && dataset.Mode != ModeSnapshot {
		query.Set("$filter", fmt.Sprintf("%s ge %s and %s lt %s", dataset.TimeField, start.UTC().Format(time.RFC3339Nano), dataset.TimeField, end.UTC().Format(time.RFC3339Nano)))
	} else if dataset.Filter != "" {
		query.Set("$filter", dataset.Filter)
	}
	u.RawQuery = query.Encode()
	return c.fetchEnvelopePages(ctx, dataset, http.MethodGet, u.String(), nil)
}

func (c *Client) fetchAzureActivity(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string) ([]FetchedRecord, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.conf.Base(dataset.Service) + path)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	filter := fmt.Sprintf("eventTimestamp ge '%s' and eventTimestamp le '%s'", start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if dataset.Filter != "" {
		filter += " and " + dataset.Filter
	}
	query.Set("$filter", filter)
	u.RawQuery = query.Encode()
	return c.fetchEnvelopePages(ctx, dataset, http.MethodGet, u.String(), nil)
}

func (c *Client) fetchAdvancedHunting(ctx context.Context, dataset Dataset, start, end time.Time) ([]FetchedRecord, error) {
	// Hunting has no OData continuation. Request one extra row to detect an
	// incomplete window instead of truncating data and advancing its watermark.
	// Stay below Microsoft's 100,000-row service ceiling including the probe.
	limit := c.conf.Page_Size * c.conf.Max_Pages
	if limit < 1 {
		return nil, fmt.Errorf("advanced hunting requires a positive Page-Size and Max-Pages budget")
	}
	if limit > 99999 {
		limit = 99999
	}
	request := map[string]string{"Query": fmt.Sprintf("%s | take %d", dataset.Query, limit+1)}
	if dataset.Mode != ModeSnapshot {
		request["Timespan"] = start.UTC().Format(time.RFC3339Nano) + "/" + end.UTC().Format(time.RFC3339Nano)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	response, err := c.request(ctx, http.MethodPost, c.conf.Base(dataset.Service)+dataset.Path, dataset.Scope, body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Results *[]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return nil, fmt.Errorf("decode %s hunting results: %w", dataset.Name, err)
	}
	if envelope.Results == nil {
		return nil, fmt.Errorf("%s hunting response is missing the results array", dataset.Name)
	}
	if len(*envelope.Results) > limit {
		return nil, fmt.Errorf("%s hunting result exceeds the %d-record budget; checkpoint retained; reduce Lookback or increase Page-Size/Max-Pages", dataset.Name, limit)
	}
	result := make([]FetchedRecord, 0, len(*envelope.Results))
	for _, raw := range *envelope.Results {
		record, err := decodeRecord(dataset, raw)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

func (c *Client) fetchParentChild(ctx context.Context, dataset Dataset, start, end time.Time, subscriptionID string) ([]FetchedRecord, error) {
	parent := dataset
	parent.Kind = KindODataGET
	parent.Path = dataset.ParentPath
	parent.Mode = ModeSnapshot
	parent.TimeField = ""
	parent.IDField = "id"
	parent.IDFields = nil
	parents, err := c.fetchOData(ctx, parent, start, end, subscriptionID)
	if err != nil {
		return nil, err
	}
	var result []FetchedRecord
	for _, item := range parents {
		if item.ID == "" {
			continue
		}
		child := dataset
		child.Kind = KindODataGET
		child.Path = strings.ReplaceAll(dataset.ChildPath, "{parentId}", url.PathEscape(item.ID))
		children, err := c.fetchOData(ctx, child, start, end, subscriptionID)
		if err != nil {
			return nil, err
		}
		// Child identifiers are scoped to the parent resource. Encode the pair
		// without delimiter ambiguity; keep the vendor payload byte-identical.
		for i := range children {
			identity, err := json.Marshal([]string{item.ID, children[i].ID})
			if err != nil {
				return nil, err
			}
			children[i].ID = string(identity)
		}
		result = append(result, children...)
	}
	return result, nil
}

func (c *Client) fetchPolicyStates(ctx context.Context, dataset Dataset, subscriptionID string) ([]FetchedRecord, error) {
	path, err := expandPath(dataset.Path, subscriptionID)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.conf.Base(dataset.Service) + path)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Set("$top", strconv.Itoa(c.conf.Page_Size))
	u.RawQuery = query.Encode()
	return c.fetchEnvelopePages(ctx, dataset, http.MethodPost, u.String(), []byte("{}"))
}

func (c *Client) fetchResourceGraph(ctx context.Context, dataset Dataset) ([]FetchedRecord, error) {
	endpoint := c.conf.Base(dataset.Service) + dataset.Path
	pageToken := ""
	seen := make(map[string]struct{})
	var result []FetchedRecord
	for page := 1; page <= c.conf.Max_Pages; page++ {
		options := map[string]any{"$top": c.conf.Page_Size, "$resultFormat": "objectArray"}
		if pageToken != "" {
			options["$skipToken"] = pageToken
		}
		body, err := json.Marshal(map[string]any{"subscriptions": c.conf.Subscription_ID, "query": dataset.Query, "options": options})
		if err != nil {
			return nil, err
		}
		response, err := c.request(ctx, http.MethodPost, endpoint, dataset.Scope, body)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Data            []json.RawMessage `json:"data"`
			SkipToken       string            `json:"skipToken"`
			DollarSkipToken string            `json:"$skipToken"`
			ResultTruncated json.RawMessage   `json:"resultTruncated"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil {
			return nil, fmt.Errorf("decode Resource Graph page: %w", err)
		}
		if envelope.Data == nil {
			return nil, errors.New("Resource Graph response is missing the data array")
		}
		for _, raw := range envelope.Data {
			record, err := decodeRecord(dataset, raw)
			if err != nil {
				return nil, err
			}
			result = append(result, record)
		}
		if envelope.SkipToken == "" {
			envelope.SkipToken = envelope.DollarSkipToken
		}
		switch string(bytes.TrimSpace(envelope.ResultTruncated)) {
		case "", "false", `"false"`:
		case "true", `"true"`:
			if envelope.SkipToken == "" {
				return nil, errors.New("Resource Graph result is truncated without continuation; checkpoint retained")
			}
		default:
			return nil, errors.New("Resource Graph returned an invalid truncation indicator")
		}
		if envelope.SkipToken == "" {
			return result, nil
		}
		if _, exists := seen[envelope.SkipToken]; exists {
			return nil, errors.New("Resource Graph returned a repeated skip token")
		}
		seen[envelope.SkipToken] = struct{}{}
		pageToken = envelope.SkipToken
	}
	return nil, fmt.Errorf("Resource Graph exceeded Max-Pages=%d", c.conf.Max_Pages)
}

func (c *Client) fetchFabricActivity(ctx context.Context, dataset Dataset, start, end time.Time) ([]FetchedRecord, error) {
	if end.Sub(start) > 28*24*time.Hour {
		return nil, errors.New("Fabric activity window exceeds 28 days")
	}
	// Use inclusive millisecond windows, rounding outward so a checkpoint near
	// midnight cannot create start > end or omit the fractional boundary.
	start = start.UTC().Truncate(time.Millisecond)
	end = end.UTC().Add(time.Millisecond - time.Nanosecond).Truncate(time.Millisecond)
	var result []FetchedRecord
	for cursor := start.UTC(); cursor.Before(end.UTC()); {
		dayEnd := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 23, 59, 59, 999000000, time.UTC)
		if dayEnd.After(end) {
			dayEnd = end.UTC()
		}
		u, err := url.Parse(c.conf.Base(dataset.Service) + dataset.Path)
		if err != nil {
			return nil, err
		}
		query := u.Query()
		query.Set("startDateTime", "'"+cursor.Format(time.RFC3339Nano)+"'")
		query.Set("endDateTime", "'"+dayEnd.Format(time.RFC3339Nano)+"'")
		u.RawQuery = query.Encode()
		next := u.String()
		seen := make(map[string]struct{})
		for page := 1; next != "" && page <= c.conf.Max_Pages; page++ {
			if _, exists := seen[next]; exists {
				return nil, errors.New("Fabric returned a repeated continuation URI")
			}
			seen[next] = struct{}{}
			body, err := c.request(ctx, http.MethodGet, next, dataset.Scope, nil)
			if err != nil {
				return nil, err
			}
			var envelope struct {
				Events          []json.RawMessage `json:"activityEventEntities"`
				ContinuationURI string            `json:"continuationUri"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil {
				return nil, fmt.Errorf("decode Fabric activity page: %w", err)
			}
			if envelope.Events == nil {
				return nil, errors.New("Fabric response is missing the activityEventEntities array")
			}
			for _, raw := range envelope.Events {
				record, err := decodeRecord(dataset, raw)
				if err != nil {
					return nil, err
				}
				result = append(result, record)
			}
			if envelope.ContinuationURI == "" {
				next = ""
			} else {
				resolved, err := c.resolveContinuation(next, envelope.ContinuationURI)
				if err != nil {
					return nil, fmt.Errorf("Fabric continuation URI: %w", err)
				}
				next = resolved
			}
			if page == c.conf.Max_Pages && next != "" {
				return nil, fmt.Errorf("Fabric activity exceeded Max-Pages=%d", c.conf.Max_Pages)
			}
		}
		cursor = dayEnd.Add(time.Millisecond)
	}
	return result, nil
}

func (c *Client) fetchEnvelopePages(ctx context.Context, dataset Dataset, method, firstURL string, requestBody []byte) ([]FetchedRecord, error) {
	next := firstURL
	seen := make(map[string]struct{})
	var result []FetchedRecord
	for page := 1; next != "" && page <= c.conf.Max_Pages; page++ {
		if _, exists := seen[next]; exists {
			return nil, fmt.Errorf("%s returned a repeated continuation URL", dataset.Name)
		}
		seen[next] = struct{}{}
		body, err := c.request(ctx, method, next, dataset.Scope, requestBody)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Value         []json.RawMessage `json:"value"`
			NextLink      string            `json:"nextLink"`
			ODataNextLink string            `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode %s page: %w", dataset.Name, err)
		}
		if envelope.Value == nil {
			return nil, fmt.Errorf("%s response is missing the value array", dataset.Name)
		}
		for _, raw := range envelope.Value {
			record, err := decodeRecord(dataset, raw)
			if err != nil {
				return nil, err
			}
			result = append(result, record)
		}
		rawNext := envelope.NextLink
		if rawNext == "" {
			rawNext = envelope.ODataNextLink
		}
		if rawNext == "" {
			next = ""
		} else {
			resolved, err := c.resolveContinuation(next, rawNext)
			if err != nil {
				return nil, fmt.Errorf("%s continuation URL: %w", dataset.Name, err)
			}
			next = resolved
		}
		if page == c.conf.Max_Pages && next != "" {
			return nil, fmt.Errorf("%s exceeded Max-Pages=%d", dataset.Name, c.conf.Max_Pages)
		}
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method, endpoint, scope string, body []byte) ([]byte, error) {
	if err := c.validateURL(endpoint); err != nil {
		return nil, err
	}
	for attempt := 0; attempt <= c.conf.Max_Retries; attempt++ {
		if err := claimFetchRequest(ctx); err != nil {
			return nil, err
		}
		if err := c.waitCooldown(ctx, endpoint); err != nil {
			return nil, err
		}
		token, err := c.token(ctx, scope)
		if err != nil {
			return nil, err
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "gravwell-microsoft/1.0")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt == c.conf.Max_Retries {
				return nil, safeTransportError(err)
			}
			if err := sleepContext(ctx, retryDelay(attempt, "")); err != nil {
				return nil, err
			}
			continue
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		// Eligibility is authoritative once the response headers arrive. A
		// truncated or oversized error body must not enable an early retry.
		if retryable {
			if err := c.retainCooldown(endpoint, retryEligibilityFor(attempt, resp.Header.Get("Retry-After"), time.Now())); err != nil {
				resp.Body.Close()
				return nil, err
			}
		}
		limit := responseLimit(ctx)
		limited := io.LimitReader(resp.Body, limit+1)
		responseBody, readErr := io.ReadAll(limited)
		resp.Body.Close()
		if readErr != nil {
			return nil, safeTransportError(readErr)
		}
		if int64(len(responseBody)) > limit {
			return nil, errors.New("Microsoft API response or total poll exceeded its byte budget; checkpoint retained")
		}
		chargeResponse(ctx, int64(len(responseBody)))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return responseBody, nil
		}
		if retryable {
			if attempt == c.conf.Max_Retries {
				return nil, fmt.Errorf("Microsoft API request failed with HTTP %d", resp.StatusCode)
			}
			if err := c.waitCooldown(ctx, endpoint); err != nil {
				return nil, err
			}
			continue
		}
		return nil, fmt.Errorf("Microsoft API request failed with HTTP %d", resp.StatusCode)
	}
	return nil, errors.New("Microsoft API retry loop exhausted")
}

func (c *Client) token(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	if token, ok := c.tokens[scope]; ok && time.Until(token.Expires) > 2*time.Minute {
		c.mu.Unlock()
		return token.Value, nil
	}
	c.mu.Unlock()
	secret, err := readSecret(c.conf.Client_Secret_File)
	if err != nil {
		return "", fmt.Errorf("reload Client-Secret-File: %w", err)
	}
	endpoint := strings.TrimRight(c.conf.Auth_Host, "/") + "/" + url.PathEscape(c.conf.Tenant_ID) + "/oauth2/v2.0/token"
	form := url.Values{"client_id": {c.conf.Client_ID}, "client_secret": {secret}, "grant_type": {"client_credentials"}, "scope": {scope}}
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
	allowed := []string{c.conf.Graph_Host, c.conf.ARM_Host, c.conf.Defender_Host, c.conf.PowerBI_Host, c.conf.Auth_Host}
	for _, base := range allowed {
		parsed, parseErr := url.Parse(base)
		if parseErr == nil && strings.EqualFold(parsed.Host, u.Host) {
			return nil
		}
	}
	return errors.New("unexpected Microsoft API origin")
}

// resolveContinuation accepts the two forms returned by Microsoft services:
// an absolute URL (Graph commonly uses this) or a relative ARM nextLink (Azure
// Policy and some Azure Monitor endpoints use this).  Every resolved URL still
// passes the same allowlisted HTTPS-origin check before it can be requested.
func (c *Client) resolveContinuation(currentURL, raw string) (string, error) {
	base, err := url.Parse(currentURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return "", errors.New("Microsoft continuation base URL must be absolute HTTPS")
	}
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("invalid Microsoft continuation URL")
	}
	resolved := reference
	if !reference.IsAbs() {
		resolved = base.ResolveReference(reference)
	}
	if !strings.EqualFold(base.Host, resolved.Host) || resolved.Scheme != base.Scheme {
		return "", errors.New("Microsoft continuation must retain the request origin")
	}
	value := resolved.String()
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

func retryDelay(attempt int, retryAfter string) time.Duration {
	raw := strings.TrimSpace(retryAfter)
	if raw != "" && strings.Trim(raw, "0123456789") == "" {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || seconds > uint64((time.Duration(1<<63-1))/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		return max(time.Until(when), 0)
	}
	return min(time.Second<<min(attempt, 6), time.Minute)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay == time.Duration(1<<63-1) {
		return errors.New("Microsoft retry eligibility exceeds the supported duration")
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Preserve cancellation identity without logging request URLs or arbitrary
// transport diagnostics, which may contain sensitive query parameters.
func safeTransportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("Microsoft HTTP transport failed")
}

// tokenResponse applies the same bounded retry and pacing policy to OAuth as
// to data requests. It never emits response bodies or request credentials.
func (c *Client) tokenResponse(ctx context.Context, endpoint, form string) ([]byte, error) {
	ctx = withRetryWaitDeadline(ctx)
	if err := c.validateURL(endpoint); err != nil {
		return nil, err
	}
	for attempt := 0; attempt <= c.conf.Max_Retries; attempt++ {
		if err := c.waitCooldown(ctx, endpoint); err != nil {
			return nil, err
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form))
		if err != nil {
			return nil, errors.New("invalid Microsoft token request")
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt == c.conf.Max_Retries {
				return nil, safeTransportError(err)
			}
			if err := sleepContext(ctx, retryDelay(attempt, "")); err != nil {
				return nil, err
			}
			continue
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if retryable {
			if err := c.retainCooldown(endpoint, retryEligibilityFor(attempt, resp.Header.Get("Retry-After"), time.Now())); err != nil {
				resp.Body.Close()
				return nil, err
			}
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, safeTransportError(readErr)
		}
		if len(body) > 1<<20 {
			return nil, errors.New("Microsoft token response exceeded the safety limit")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}
		if retryable {
			if attempt == c.conf.Max_Retries {
				return nil, fmt.Errorf("Microsoft token request failed with HTTP %d", resp.StatusCode)
			}
			if err := c.waitCooldown(ctx, endpoint); err != nil {
				return nil, err
			}
			continue
		}
		return nil, fmt.Errorf("Microsoft token request failed with HTTP %d", resp.StatusCode)
	}
	return nil, errors.New("Microsoft token retry loop exhausted")
}
