/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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

type retryClient struct {
	rl                 *rate.Limiter
	cli                *http.Client
	ctx                context.Context
	retryResponseCodes []int
	backoff            time.Duration
}

func newRetryClient(rl *rate.Limiter, timeout, backoff time.Duration, ctx context.Context, retryCodes []int) *retryClient {
	if retryCodes == nil {
		retryCodes = defaultRetryCodes
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if backoff <= 0 {
		backoff = defaultBackoff
	}

	return &retryClient{
		rl: rl,
		cli: &http.Client{
			Timeout: timeout,
		},
		ctx:                ctx,
		retryResponseCodes: retryCodes,
		backoff:            backoff,
	}
}

func (rc *retryClient) Do(req *http.Request) (resp *http.Response, err error) {
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
			log.Printf("Retrying due to error requesting %v %v\n", req, err)
		} else if resp.StatusCode != http.StatusOK {
			//drain the body just in case
			drainResponse(resp)
			//check if this status code is something we can recover from
			if rc.isRecoverableStatus(resp.StatusCode) == false {
				log.Printf("Aborting Retry due to response code %s (%d)\n", resp.Status, resp.StatusCode)
				err = fmt.Errorf("non-recoverable status code %s (%d)", resp.Status, resp.StatusCode)
				return
			}
			log.Printf("Retrying due to response code %s (%d)\n", resp.Status, resp.StatusCode)
		} else {
			//all good
			break
		}
		if quitableSleep(rc.ctx, rc.backoff) {
			break
		}
	}
	return
}

func (rc *retryClient) isRecoverableStatus(status int) bool {
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

func drainResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	return
}

func quitableSleep(ctx context.Context, to time.Duration) (quit bool) {
	tmr := time.NewTimer(to)
	defer tmr.Stop()
	select {
	case <-tmr.C:
	case <-ctx.Done():
		quit = true
	}
	return
}
