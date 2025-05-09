/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrOversizedObject = errors.New("JSON object too large")
	ErrInvalidLimit    = errors.New("limit mus be greater than zero")
	ErrInvalidReader   = errors.New("reader is invalid")
)

type JsonLimitedDecoder struct {
	*json.Decoder
	total   int64
	lr      *io.LimitedReader
	maxSize int64
}

// NewJsonLimitedDecoder will return a new JsonLimitedDecoder ready for use.
// The json.Decoder object is directly exposed so that buffer methods can be used.
// This is a drop in replacement for the json.Decoder but we can return additional errors about oversized objects.
func NewJsonLimitedDecoder(rdr io.Reader, max int64) (jld *JsonLimitedDecoder, err error) {
	if max <= 0 {
		err = ErrInvalidLimit
		return
	} else if rdr == nil {
		err = ErrInvalidReader
		return
	}
	lr := &io.LimitedReader{
		R: rdr,
		N: max,
	}
	jld = &JsonLimitedDecoder{
		lr:      lr,
		Decoder: json.NewDecoder(lr),
		maxSize: max,
	}
	return
}

// Decode an object using a JSON decoder
func (j *JsonLimitedDecoder) Decode(v interface{}) (err error) {
	j.lr.N = j.maxSize
	err = j.Decoder.Decode(v)
	j.total += j.maxSize - j.lr.N //keep a tally
	if err == nil {
		return // all good
	}
	//figure out why
	if j.lr.N == 0 {
		//we wrap the decoder output error so that callers can check if it was due to size or just outright broken
		err = fmt.Errorf("%w exceeded %d bytes - %v", ErrOversizedObject, j.maxSize, err)
	}
	return
}

func (j *JsonLimitedDecoder) TotalRead() int64 {
	return j.total
}
