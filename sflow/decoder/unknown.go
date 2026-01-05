/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
	"github.com/gravwell/gravwell/v3/sflow/xdr"
)

func decodeUnknownSample(r *io.LimitedReader, format, length uint32) (*datagram.UnknownSample, error) {
	if int64(length) > r.N {
		return nil, ErrSizeTooLarge
	}

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
	if err := xdr.SkipPadding(r, length); err != nil {
		return nil, err
	}

	return &datagram.UnknownSample{
		Format: format,
		Data:   data,
	}, nil
}

func decodeUnknownRecord(r *io.LimitedReader, dataFormat uint32) (*datagram.UnknownRecord, error) {
	record, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return nil, err
	}

	return &datagram.UnknownRecord{
		Format: dataFormat,
		Data:   record,
	}, nil
}
