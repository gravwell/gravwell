/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"errors"
	"io"
	"net/http"
)

const (
	maxDataDrain = 1024 * 1024 * 4
)

var (
	ErrMaxBodyDrained = errors.New("too many response bytes in body, closing")
)

var (
	adminParams = map[string]string{
		`admin`: `true`,
	}
)

// drainResponse will drain up to 4MB of data then close the response Body.
// We do this so that http requests can re-use connects as per docs.
func drainResponse(resp *http.Response) {
	if resp.Body == nil {
		return
	}
	var nw nilWriter
	io.Copy(&nw, resp.Body)
	resp.Body.Close()
}

type nilWriter struct {
	n int
}

func (nw *nilWriter) Write(b []byte) (r int, err error) {
	if nw.n > maxDataDrain {
		r = -1
		err = ErrMaxBodyDrained
		return
	}
	nw.n += len(b)
	r = len(b)
	return
}
