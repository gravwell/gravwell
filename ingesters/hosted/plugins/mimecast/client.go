package mimecast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	SIEMBatchTimeFormat = "2006-01-02"
	AuditTimeFormat     = "2006-01-02T15:04:05-0700"
	SIEMTimeFormat      = "2006-01-02T15:04:05.000Z"
	siemBatchEndpoint   = "/siem/v1/batch/events/cg"
	siemEndpoint        = "/siem/v1/events/cg"
	AuditEndpoint       = "/api/audit/get-audit-events"
)

var (
	ErrAuthenticationFailure = errors.New("authentication failure")
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
		return fmt.Errorf("%w, request failed: %w", ErrAuthenticationFailure, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w, got bad status code: %d", ErrAuthenticationFailure, res.StatusCode)
	}

	token, err := parse[AuthToken](res.Body)
	if err != nil {
		return fmt.Errorf("%w, failed to parse auth response: %w", ErrAuthenticationFailure, err)
	}
	// expire 'early' so we don't risk a race
	token.ExpireAt = time.Now().Add(time.Duration(token.ExpireIn)*time.Second - time.Second*5)
	c.token = *token
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
	response, err := c.c.Do(r)
	if err != nil {
		return response, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		fail, err := parse[AuthFailureResponse](response.Body)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w, failed to parse response: %w", ErrAuthenticationFailure, err)
		}
		if len(fail.Fail) < 1 {
			return nil, fmt.Errorf("%w, no provided reason", ErrAuthenticationFailure)
		}
		return nil, fmt.Errorf("%w: %s, %s", ErrAuthenticationFailure, fail.Fail[0].Code, fail.Fail[0].Message)
	}
	return response, nil
}

func (c *Client) GetSIEMEventBatch(ctx context.Context, et EventType, start, end time.Time, cursor string) (*SIEMBatchEventResponse, error) {
	if start.After(end) {
		return nil, fmt.Errorf("start time is after end time")
	}

	params := url.Values{}
	params.Set("type", string(et))
	params.Set("pageSize", "100")
	if cursor != "" {
		params.Set("nextPage", cursor)
	} else {
		params.Set("dateRangeStartsAt", start.Format(SIEMBatchTimeFormat))
		params.Set("dateRangeEndsAt", end.Format(SIEMBatchTimeFormat))
	}
	endpoint := fmt.Sprintf("%s%s?%s",
		c.host,
		siemBatchEndpoint,
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
		statusErr := fmt.Errorf("invalid status code %d", resp.StatusCode)
		b, err := parse[SIEMErrorResponse](resp.Body)
		if err != nil {
			return nil, fmt.Errorf("%w, failed to parse error response: %w", statusErr, err)
		}
		return nil, fmt.Errorf("%w, error: %s - %s", statusErr, b.Error.Code, b.Error.Message)
	}

	b, err := parse[SIEMBatchEventResponse](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing siem event batch response: %w", err)
	}

	return b, nil
}

// GetRawAuditEvents returns a partially parsed response from a single page of the API. More pages can be read by providing the cursor.
// The intent is to allow only the caller to control when/if the actual audit data is decoded given the variance in structures of the API.
func (c *Client) GetRawAuditEvents(ctx context.Context, start, end time.Time, cursor string) (*Response, error) {
	if start.After(end) {
		return nil, fmt.Errorf("start time is after end time")
	}

	payload := Request{}
	payload.Meta.Pagination.PageSize = 100
	if cursor != "" {
		payload.Meta.Pagination.PageToken = cursor
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

func (c *Client) GetRawSIEMEvents(ctx context.Context, event EventType, start, end time.Time, cursor string) (*SIEMEventResponse, error) {
	if err := validTime(start, end); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("types", string(event))
	params.Set("pageSize", "100")
	if cursor != "" {
		params.Set("nextPage", cursor)
	} else {
		params.Set("dateRangeStartsAt", start.Format(SIEMTimeFormat))
		params.Set("dateRangeEndsAt", end.Format(SIEMTimeFormat))
	}
	endpoint := fmt.Sprintf("%s%s?%s",
		c.host,
		siemEndpoint,
		params.Encode(),
	)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error building siem events request: %w", err)
	}

	r.Header.Set("Accept", "application/json")
	resp, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("error making siem event request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("invalid status code %d", resp.StatusCode)
		b, err := parse[SIEMErrorResponse](resp.Body)
		if err != nil {
			return nil, fmt.Errorf("%w, failed to parse error response: %w", statusErr, err)
		}
		return nil, fmt.Errorf("%w, error: %s - %s", statusErr, b.Error.Code, b.Error.Message)
	}

	b, err := parse[SIEMEventResponse](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing siem events response: %w", err)
	}

	return b, nil
}

func validTime(start, end time.Time) error {
	if start.After(end) {
		return fmt.Errorf("start time is after end time")
	}
	if start.IsZero() {
		return fmt.Errorf("start time is zero")
	}
	if end.IsZero() {
		return fmt.Errorf("end time is zero")
	}
	if start.Equal(end) {
		return fmt.Errorf("start and end time are equal")
	}
	return nil
}

// parse will read and marshal bytes into a type T.
// intended for use with http.Response bodies
func parse[T any](rc io.Reader) (*T, error) {
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
