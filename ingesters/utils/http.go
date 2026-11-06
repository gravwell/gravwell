/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultTimeout = time.Second * 3
	defaultBackoff = time.Second * 10
	// DefaultRetryDrainLimit bounds how much of a retryable response body the
	// retry loop reads before the attempt that follows it. A 425, 429 or 5xx
	// body is only ever discarded, so reading all of it buys nothing and costs
	// an unbounded number of bytes and an unbounded amount of CPU once per
	// attempt: the declared length is whatever the origin claims, a chunked
	// body declares nothing at all, and a body net/http decompresses
	// transparently expands by whatever ratio the sender chose. 64 KiB is a
	// conservative ceiling on the diagnostic JSON these statuses carry.
	DefaultRetryDrainLimit = 64 << 10
	// MaximumRetryAfter bounds Retry-After parsing for callers that explicitly
	// opt into a server-directed delay beyond their configured backoff.
	MaximumRetryAfter = 24 * time.Hour
)

var (
	defaultRetryCodes = []int{425, 429} // basically just Too Early and too many requests
	// ErrRateLimitWait identifies a request that was not dispatched because the
	// caller's context could not accommodate the rate limiter's required wait.
	ErrRateLimitWait = errors.New("retry client rate limiter wait")
	// ErrOversizedResponse identifies a retryable response whose body crossed
	// the retry-drain bound. It is deliberately generic: nothing past the bound
	// is read, so there is nothing about the response to report, and the retry
	// loop stops rather than paying the same unbounded cost again.
	ErrOversizedResponse = errors.New("retry client response body exceeds the retry drain bound")
)

type RetryHttpClient struct {
	rl                 *rate.Limiter
	cli                *http.Client
	ctx                context.Context
	retryResponseCodes []int
	backoff            time.Duration
	// retryAfterLimit bounds how much a server can extend backoff. Constructors
	// default it to backoff, preserving the shared client's historical retry
	// cadence for callers with a plugin-lifetime context.
	retryAfterLimit time.Duration
	// drainLimit bounds the retry drain. It is never zero: both constructors
	// install DefaultRetryDrainLimit.
	drainLimit int64
}

func NewRetryHttpClient(rl *rate.Limiter, timeout, backoff time.Duration, ctx context.Context, retryCodes []int) *RetryHttpClient {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return NewRetryHttpClientWithClient(rl, &http.Client{Timeout: timeout}, backoff, ctx, retryCodes)
}

// NewRetryHttpClientWithClient constructs a retrying client around cli. This
// preserves the caller's transport, timeout, cookie jar, and redirect policy,
// allowing credential-bearing callers to reject redirects before net/http can
// forward request headers. A nil cli uses the same default timeout as
// NewRetryHttpClient.
func NewRetryHttpClientWithClient(rl *rate.Limiter, cli *http.Client, backoff time.Duration, ctx context.Context, retryCodes []int) *RetryHttpClient {
	if retryCodes == nil {
		retryCodes = defaultRetryCodes
	}
	if cli == nil {
		cli = &http.Client{Timeout: defaultTimeout}
	}
	if backoff <= 0 {
		backoff = defaultBackoff
	}

	return &RetryHttpClient{
		rl:                 rl,
		cli:                cli,
		ctx:                ctx,
		retryResponseCodes: retryCodes,
		backoff:            backoff,
		retryAfterLimit:    backoff,
		drainLimit:         DefaultRetryDrainLimit,
	}
}

// SetRetryAfterLimit lets a caller honor Retry-After beyond its configured
// backoff. Call it before Do; values below backoff retain the backoff floor.
func (rc *RetryHttpClient) SetRetryAfterLimit(limit time.Duration) {
	if rc == nil {
		return
	}
	if limit < rc.backoff {
		limit = rc.backoff
	}
	rc.retryAfterLimit = limit
}

func (rc *RetryHttpClient) Do(req *http.Request) (resp *http.Response, err error) {
	if rc == nil {
		return nil, errors.New("retry client not ready")
	}
	if req == nil {
		return nil, errors.New("nil request")
	}

	for {
		delay := rc.backoff
		if rc.rl != nil {
			if err = rc.rl.Wait(rc.ctx); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrRateLimitWait, err)
			}
		}
		if resp, err = rc.cli.Do(req.WithContext(rc.ctx)); err != nil {
			//log the error and continue
			if rc.ctx.Err() != nil {
				//context canceled, or timed out
				return
			}
			// some sort of error, backoff and then continue
		} else if rc.isRecoverableStatus(resp.StatusCode) {
			if resp.StatusCode == http.StatusTooManyRequests {
				retryAfter := boundedRetryAfter(resp.Header.Get("Retry-After"), time.Now())
				if retryAfter > rc.retryAfterLimit {
					retryAfter = rc.retryAfterLimit
				}
				if retryAfter > delay {
					delay = retryAfter
				}
			}
			//drain the body, as we will be retrying.  The body is discarded
			//either way, so it is drained under a bound: an oversized one stops
			//the retry loop instead of being read once per attempt.
			if err = DrainResponseBounded(resp, rc.drainLimit); err != nil {
				return nil, err
			}
		} else {
			//all good
			return
		}
		if QuitableSleep(rc.ctx, delay) {
			return nil, rc.ctx.Err()
		}
	}
}

func boundedRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseUint(raw, 10, 64); err == nil {
		if seconds > uint64(MaximumRetryAfter/time.Second) {
			return MaximumRetryAfter
		}
		return time.Duration(seconds) * time.Second
	} else if strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		return MaximumRetryAfter
	}
	when, err := http.ParseTime(raw)
	if err != nil || !when.After(now) {
		return 0
	}
	delay := when.Sub(now)
	if delay > MaximumRetryAfter {
		return MaximumRetryAfter
	}
	return delay
}

func (rc *RetryHttpClient) isRecoverableStatus(status int) bool {
	if status >= 500 {
		// it is server side, so yes
		return true
	}
	for _, v := range rc.retryResponseCodes {
		if v == status {
			return true
		}
	}
	return false
}

func DrainResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// DrainResponseBounded is the shared bounded drain for a response whose body is
// going to be discarded. It reads at most limit bytes as resp.Body presents
// them -- which is after any transparent decompression net/http performed, so
// an expanding encoding is bounded by what it expands to rather than by what
// arrived -- and always closes the body.
//
// A declared length past the bound is refused without reading any of it. Beyond
// that, closing a body that still has unread bytes is deliberate: net/http only
// returns a connection to the idle pool once its response body has been read to
// completion, so an oversized response costs one connection rather than
// becoming the connection the next request inherits.
//
// Only an oversized body is reported. A read error on a body that is being
// discarded is not an error the caller can act on, which is why DrainResponse
// has always ignored it, and reporting it here would turn a truncated retryable
// response into a failure instead of another attempt.
func DrainResponseBounded(resp *http.Response, limit int64) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	if limit < 0 {
		limit = 0
	}
	if resp.ContentLength > limit {
		resp.Body.Close()
		return ErrOversizedResponse
	}
	read, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, limit+1))
	resp.Body.Close()
	if read > limit {
		return ErrOversizedResponse
	}
	return nil
}
