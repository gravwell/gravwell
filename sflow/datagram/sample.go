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

	"github.com/gravwell/gravwell/v3/sflow/xdr"
)

const (
	SampleHeaderFormatSize int = 4
	SampleHeaderLengthSize
	SampleHeaderSize = int(unsafe.Sizeof(SampleHeader{}))
)

type Sample interface {
	GetHeader() SampleHeader
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

func (sa SampleHeader) Pad() int {
	return int(xdr.CalculatePad(sa.Length))
}

func (sa SampleHeader) DataFullLength() int {
	length := int(sa.Length)

	return length + sa.Pad()
}

// SFlowDataSource see https://sflow.org/sflow_version_5.txt, pag 30, `sflow_data_source`
type SFlowDataSource = uint32

// SFlowDataSourceExpanded see https://sflow.org/sflow_version_5.txt, pag 30, `sflow_data_source_expanded`
type SFlowDataSourceExpanded struct {
	SourceIDType  uint32
	SourceIDIndex uint32
}

// Interface see https://sflow.org/sflow_version_5.txt, pag 28, `interface`
type Interface = uint32

// InterfaceExpanded see https://sflow.org/sflow_version_5.txt, pag 30, `interface_expanded`
type InterfaceExpanded struct {
	Format uint32
	Value  uint32
}
