/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"io"
)

type ResourceContentType struct {
	ContentType string
	Body        []byte
}

type ResourceUpdate struct {
	Metadata Resource
	Data     []byte
	rdr      io.ReadCloser //do not export this, gob can't handle the type
}

// Bytes returns a byte slice no matter what the underlying storage is
// if the ResourceUpdate is using a readCloser then it performs a complete read and
// returns a byte slice.  If the reader points to a large resource this may require significant resources
func (ru *ResourceUpdate) Bytes() (b []byte) {
	if ru.Data != nil {
		b = ru.Data
	} else {
		bb := bytes.NewBuffer(nil)
		io.Copy(bb, ru.rdr)
		b = bb.Bytes()
	}
	return
}

// Stream generates a io.Reader from either the underlying reader or the Data byte slice
func (ru *ResourceUpdate) Stream() io.Reader {
	if ru.rdr != nil {
		return ru.rdr
	}
	return bytes.NewBuffer(ru.Data)
}

// SetStream will set the resource update to use a read closer instead of static bytes
// we do not export the ReadCloser because gob can't handle it
func (ru *ResourceUpdate) SetStream(rc io.ReadCloser) {
	if ru != nil {
		ru.Data = nil
		ru.rdr = rc
	}
}

// Close is a safe method to make sure that ReadClosers and Byte Buffers are wiped out
func (ru *ResourceUpdate) Close() {
	if ru != nil {
		if ru.rdr != nil {
			ru.rdr.Close()
		}
		if ru.Data != nil {
			ru.Data = nil
		}
	}
}

// Resource contains metadata about the resource but not the actual
// bytes, because those may be quite large.
type Resource struct {
	CommonFields

	Size        uint64
	Hash        string
	ContentType string // Guessed at update time if possible
}

type ResourceListResponse struct {
	BaseListResponse
	Results []Resource `json:"results"`
}
