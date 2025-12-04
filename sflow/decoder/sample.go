/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrSampleMalformedOrIncomplete = errors.New("sample is malformed or incomplete")
	ErrRecordMalformedOrIncomplete = errors.New("record is malformed or incomplete")
)

func decodeSample(r io.Reader) (datagram.Sample, error) {
	var format uint32
	var length uint32
	var err error

	err = binary.Read(r, binary.BigEndian, &format)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.BigEndian, &length)
	if err != nil {
		return nil, err
	}

	var sample datagram.Sample
	switch format {
	// TODO Not yet
	case datagram.CounterSampleFormat:
		sample, err = decodeCounterSampleFormat(r, length)
	case datagram.CounterSampleExtendedFormat:
		sample, err = decodeCounterSampleExpandedFormat(r, length)
	default:
		sample, err = decodeUnknownSample(r, format, length)
	}

	return sample, err
}
