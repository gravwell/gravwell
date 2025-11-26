/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"encoding/binary"
	"errors"
)

var (
	ErrSampleHeaderTooShort = errors.New("sample header too small")
	ErrRecordHeaderTooShort = errors.New("record header too small")
)

// UnknownSample refers to a vendor specific sample or a sample we don't know the structure of.
type UnknownSample []byte

func (us *UnknownSample) GetSampleHeader() (SampleHeader, error) {
	raw := *us
	if len(raw) < SampleHeaderSize {
		return SampleHeader{}, ErrSampleHeaderTooShort
	}

	return SampleHeader{
		Format: binary.BigEndian.Uint32(raw),
		Length: binary.BigEndian.Uint32(raw[SampleHeaderFormatSize:SampleHeaderLengthSize]),
	}, nil
}

// UnknownRecord refers to a vendor specific record or a record we don't know the structure of.
type UnknownRecord []byte

func (us *UnknownRecord) GetRecordHeader() (RecordHeader, error) {
	raw := *us
	if len(raw) < int(RecordHeaderSize) {
		return RecordHeader{}, ErrRecordHeaderTooShort
	}

	return RecordHeader{
		Format: binary.BigEndian.Uint32(raw),
		Length: binary.BigEndian.Uint32(raw[RecordHeaderFormatSize:RecordHeaderLengthSize]),
	}, nil
}
