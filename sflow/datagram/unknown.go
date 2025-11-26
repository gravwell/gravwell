/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

// UnknownSample refers to a vendor specific sample or a sample we don't know the structure of.
type UnknownSample struct {
	Format uint32
	Data   XDRVariableLengthOpaque
}

func (us *UnknownSample) GetHeader() SampleHeader {
	return SampleHeader{
		Format: us.Format,
		Length: uint32(us.Data.Len()),
	}
}

func (us *UnknownSample) GetFullLength() int {
	return us.Data.FullLen()
}

// UnknownRecord refers to a vendor specific record or a record we don't know the structure of.
type UnknownRecord = UnknownSample
