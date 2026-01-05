/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

import (
	"unsafe"
)

const (
	RecordHeaderFormatSize int = 4
	RecordHeaderLengthSize
	RecordHeaderSize = unsafe.Sizeof(RecordHeader{})
)

type Record interface {
	GetHeader() RecordHeader
}

type RecordHeader = SampleHeader
