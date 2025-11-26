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
	SampleHeaderFormatSize int = 4
	SampleHeaderLengthSize
	SampleHeaderSize = int(unsafe.Sizeof(SampleHeader{}))
)

type Sample interface {
	GetSampleHeader() (SampleHeader, error)
}

// SampleHeader minimum data all sample types, vendor and official, must provide.
// See https://sflow.org/developers/diagrams/sFlowV5Sample.pdf and https://sflow.org/sflow_version_5.txt, pag 25
// Samples and records share the same header.
type SampleHeader struct {
	// The most significant 20 bits correspond to the SMI Private Enterprise Code of the entity responsible for the structure definition. A value of zero is used to denote standard structures defined by sflow.org.
	//
	// The least significant 12 bits are a structure format number assigned by the enterprise that should uniquely identify the the format of the structure.
	Format uint32
	// Length of the rest of the sample data in bytes
	Length uint32
}
