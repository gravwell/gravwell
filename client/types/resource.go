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
	"encoding/json"
	"io"
)

type ResourceUpdate struct {
	Metadata Resource
	Data     []byte
	rdr      io.ReadCloser //do not export this, gob can't handle the type
}

// maxResourcePresize bounds how much we will speculatively allocate off of the size
// declared in resource metadata.  A bogus size should not drive a huge allocation,
// anything beyond this cap just grows the buffer the normal way.
const maxResourcePresize = 1024 * 1024 * 1024 // 1GB

// Bytes returns a byte slice no matter what the underlying storage is
// if the ResourceUpdate is using a readCloser then it performs a complete read and
// returns a byte slice.  If the reader points to a large resource this may require significant resources
func (ru *ResourceUpdate) Bytes() (b []byte, err error) {
	if ru.Data != nil {
		b = ru.Data
	} else if ru.rdr != nil {
		// pre-size the buffer using the size declared in the metadata, an unsized buffer
		// walks a doubling ladder and burns about 2x the final size in allocations.
		// the extra bytes.MinRead of headroom keeps ReadFrom from doubling one last
		// time when it asks for room for the read that returns EOF.
		bb := bytes.NewBuffer(make([]byte, 0, presizeHint(ru.Metadata.Size)+bytes.MinRead))
		if _, err = io.Copy(bb, ru.rdr); err != nil {
			return
		}
		b = bb.Bytes()
	}
	return
}

func presizeHint(sz uint64) int {
	if sz > maxResourcePresize {
		return maxResourcePresize
	}
	return int(sz)
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

	Size          uint64
	Hash          string
	ContentType   string // Guessed at update time if possible
	FileExtension string // The extension of the uploaded file, with the dot (ex: ".csv").
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r Resource) MarshalJSON() ([]byte, error) {
	type dummyResource Resource
	r.CommonFields = r.CommonFields.MakeNilSlices()
	return json.Marshal(dummyResource(r))
}

type ResourceListResponse struct {
	BaseListResponse
	Results []Resource
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (r ResourceListResponse) MarshalJSON() ([]byte, error) {
	type dummyResourceListResponse ResourceListResponse
	r.Results = nonNilSlice(r.Results)
	r.BaseListResponse = r.BaseListResponse.MakeNilSlices()
	return json.Marshal(dummyResourceListResponse(r))
}
