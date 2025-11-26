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
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func decodeUnknownSample(r io.Reader, format, length uint32) (*datagram.UnknownSample, error) {
	rest := make([]byte, length)
	n, err := r.Read(rest)
	if err != nil {
		return nil, err
	}
	if n != int(length) {
		return nil, ErrSampleMalformedOrIncomplete
	}

	res := datagram.UnknownSample(make([]byte, uint32(datagram.SampleHeaderSize)+length))

	if _, err := binary.Encode(res[:datagram.SampleHeaderFormatSize], binary.BigEndian, &format); err != nil {
		return nil, err
	}
	if _, err := binary.Encode(res[datagram.SampleHeaderFormatSize:datagram.SampleHeaderSize], binary.BigEndian, &length); err != nil {
		return nil, err
	}

	copy(res[datagram.SampleHeaderSize:], rest)

	return &res, nil
}

func decodeUnknownRecord(r io.Reader, format uint32) (*datagram.UnknownRecord, error) {
	var dataFormat uint32
	var length uint32

	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	res := datagram.UnknownRecord(make([]byte, uint32(datagram.RecordHeaderSize)+length))

	if _, err := binary.Encode(res[:datagram.RecordHeaderFormatSize], binary.BigEndian, &dataFormat); err != nil {
		return nil, err
	}
	if _, err := binary.Encode(res[datagram.RecordHeaderFormatSize:datagram.RecordHeaderSize], binary.BigEndian, &length); err != nil {
		return nil, err
	}

	rest := make([]byte, length)
	n, err := r.Read(rest)
	if err != nil {
		return nil, err
	}
	if n != int(length) {
		return nil, ErrRecordMalformedOrIncomplete
	}

	copy(res[datagram.RecordHeaderSize:], rest)

	return &res, nil
}
