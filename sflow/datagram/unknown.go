/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import "encoding/binary"

// UnknownSample refers to a vendor specific sample.
type UnknownSample []byte

func (us *UnknownSample) GetHeader() (SampleHeader, error) {
	raw := *us
	if len(raw) < MinSampleHeaderSize {
		return SampleHeader{}, ErrSampleHeaderTooShort
	}

	return SampleHeader{
		Format: binary.BigEndian.Uint32(raw),
		Length: binary.BigEndian.Uint32(raw[SampleHeaderFormatSize:SampleHeaderLengthSize]),
	}, nil
}
