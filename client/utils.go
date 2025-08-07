/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
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

type jwtState struct {
	UID     int       `json:"uid"`
	Expires time.Time `json:"expires"`
}

// simple wrapper that decodes the JWT expire timestamp
// a zero time is returned on any decode failure
func decodeJWTExpires(jwt string) (r time.Time) {
	var st jwtState
	if bits := strings.Split(jwt, "."); len(bits) == 3 {
		if stateBts, err := hex.DecodeString(bits[1]); err == nil {
			if err = json.Unmarshal(stateBts, &st); err == nil {
				r = st.Expires
			}
		}
	}
	return
}
