/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func decodeUnknownSample(r io.Reader, format, length uint32) (*datagram.UnknownSample, error) {
	// Per XDR spec, length is the actual data size. Padding is wire overhead
	// that can be recomputed for re-encoding via data.Pad().
	data := make(datagram.XDRVariableLengthOpaque, length)
	n, err := r.Read(data)
	if err != nil {
		return nil, err
	}
	if n != int(length) {
		return nil, ErrSampleMalformedOrIncomplete
	}

	// Discard padding bytes from the stream
	if data.Pad() > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(data.Pad())); err != nil {
			return nil, err
		}
	}

	return &datagram.UnknownSample{
		Format: format,
		Data:   data,
	}, nil
}

func decodeUnknownRecord(r io.Reader, dataFormat uint32) (*datagram.UnknownRecord, error) {
	record, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return nil, err
	}

	return &datagram.UnknownRecord{
		Format: dataFormat,
		Data:   record,
	}, nil
}
