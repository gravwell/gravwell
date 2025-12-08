package mimecast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	MTATimeFormat   = "2006-01-02"
	AuditTimeFormat = "2006-01-02T15:04:05-0700"
	siemEndpoint    = "/siem/v1/batch/events/cg"
	AuditEndpoint   = "/api/audit/get-audit-events"
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	host   string
	id     string
	secret string
	c      doer
	mtx    sync.RWMutex
	token  AuthToken
}

type AuthToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpireIn    int64     `json:"expires_in"`
	Scope       string    `json:"scope"`
	ExpireAt    time.Time `json:"-"`
}

func NewClient(host, id, secret string, c doer) *Client {
	return &Client{host: host, id: id, secret: secret, c: c}
}

func (c *Client) authenticate(ctx context.Context) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.token.ExpireAt.After(time.Now()) {
		// token isn't expired yet
		return nil
	}

	data := url.Values{}
	data.Set("client_id", c.id)
	data.Set("client_secret", c.secret)
	data.Set("grant_type", "client_credentials")
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/oauth/token", c.host), strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := c.c.Do(r)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("auth request got bad status code: %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("auth request could not read response body")
	}

	var token AuthToken
	err = json.Unmarshal(body, &token)
	if err != nil {
		return fmt.Errorf("failed to read auth token: %w", err)
	}
	// expire 'early' so we don't risk a race
	c.token.ExpireAt = time.Now().Add(time.Duration(c.token.ExpireIn)*time.Second - time.Second*5)
	c.token = token
	return nil
}

func (c *Client) Do(r *http.Request) (*http.Response, error) {
	ctx, can := context.WithTimeout(r.Context(), time.Second*5)
	err := c.authenticate(ctx)
	can()
	if err != nil {
		return nil, err
	}
	c.mtx.RLock()
	r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token.AccessToken))
	c.mtx.RUnlock()
	r.Header.Set("Accept", "application/json")
	return c.c.Do(r)
}

func (c *Client) GetSIEMEventBatch(ctx context.Context, et EventType, start, end time.Time, cursor *string) (*SIEMBatchEventResponse, error) {
	if start.After(end) {
		return nil, fmt.Errorf("start time is after end time")
	}

	params := url.Values{}
	params.Set("type", string(et))
	params.Set("dateRangeStartsAt", start.Format(MTATimeFormat))
	params.Set("dateRangeEndsAt", end.Format(MTATimeFormat))
	params.Set("pageSize", "10") // limit risk of dropping events
	if cursor != nil {
		params.Set("nextPage", *cursor)
	}
	endpoint := fmt.Sprintf("%s%s?%s",
		c.host,
		siemEndpoint,
		params.Encode(),
	)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error building siem event batch request: %w", err)
	}

	resp, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error making siem event batch request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	b, err := parse[SIEMBatchEventResponse](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing siem event batch response: %w", err)
	}

	return b, nil
}

func (c *Client) GetRawAuditEvents(ctx context.Context, start, end time.Time, cursor *string) (*Response, error) {
	if start.After(end) {
		return nil, fmt.Errorf("start time is after end time")
	}

	payload := Request{}
	payload.Meta.Pagination.PageSize = 10
	if cursor != nil {
		payload.Meta.Pagination.PageToken = *cursor
	} else {
		payload.Data = []RequestData{
			{
				StartDateTime: start.Format(AuditTimeFormat),
				EndDateTime:   end.Format(AuditTimeFormat),
			},
		}
	}

	pBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error making audit api payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s%s", c.host, AuditEndpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(pBody))
	if err != nil {
		return nil, fmt.Errorf("error making audit api request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling audit api: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	b, err := parse[Response](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing audit api response: %w", err)
	}

	return b, nil
}

func parse[T any](rc io.ReadCloser) (*T, error) {
	t := new(T)

	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, t); err != nil {
		return nil, err
	}

	return t, nil
}
