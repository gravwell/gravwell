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
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultTimeout = time.Second * 3
	defaultBackoff = time.Second * 10
)

var (
	defaultRetryCodes = []int{425, 429} // basically just Too Early and too many requests
)

type RetryHttpClient struct {
	rl                 *rate.Limiter
	cli                *http.Client
	ctx                context.Context
	retryResponseCodes []int
	backoff            time.Duration
}

func NewRetryHttpClient(rl *rate.Limiter, timeout, backoff time.Duration, ctx context.Context, retryCodes []int) *RetryHttpClient {
	if retryCodes == nil {
		retryCodes = defaultRetryCodes
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if backoff <= 0 {
		backoff = defaultBackoff
	}

	return &RetryHttpClient{
		rl: rl,
		cli: &http.Client{
			Timeout: timeout,
		},
		ctx:                ctx,
		retryResponseCodes: retryCodes,
		backoff:            backoff,
	}
}

func (rc *RetryHttpClient) Do(req *http.Request) (resp *http.Response, err error) {
	if rc == nil {
		return nil, errors.New("retry client not ready")
	}
	if req == nil {
		return nil, errors.New("nil request")
	}

	for {
		if rc.rl != nil {
			if err = rc.rl.Wait(rc.ctx); err != nil {
				return nil, err
			}
		}
		if resp, err = rc.cli.Do(req.WithContext(rc.ctx)); err != nil {
			//log the error and continue
			if rc.ctx.Err() != nil {
				//context cancelled
				return
			}
			// some sort of error, backoff and then continue
		} else if resp.StatusCode != http.StatusOK {
			//drain the body just in case
			DrainResponse(resp)
			//check if this status code is something we can recover from
			if !rc.isRecoverableStatus(resp.StatusCode) {
				err = fmt.Errorf("non-recoverable status code %s (%d)", resp.Status, resp.StatusCode)
				return
			}
		} else {
			//all good
			break
		}
		if QuitableSleep(rc.ctx, rc.backoff) {
			break
		}
	}
	return
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
}
