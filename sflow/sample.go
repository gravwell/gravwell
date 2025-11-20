/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sflow

import (
	"encoding/binary"
	"errors"
)

const (
	SampleHeaderFormatSize int = 4
	SampleHeaderLengthSize
	MinSampleHeaderSize int = SampleHeaderLengthSize + SampleHeaderFormatSize
)

var (
	ErrSampleHeaderTooShort = errors.New("sample header too small")
)

type Sample interface {
	GetHeader() (*SampleHeader, error)
}

// SampleHeader minimum data all sample types, vendor and official, must provide.
// See https://sflow.org/developers/diagrams/sFlowV5Sample.pdf
type SampleHeader struct {
	// See https://sflow.org/sflow_version_5.txt, pag  25
	//
	// The most significant 20 bits correspond to the SMI Private Enterprise Code of the entity responsible for the structure definition. A value of zero is used to denote standard structures defined by sflow.org.
	//
	// The least significant 12 bits are a structure format number assigned by the enterprise that should uniquely identify the the format of the structure.
	Format uint32
	// Length of the rest of the sample data in bytes
	Length uint32
}


// Official Samples

// TODO  For now, we are going to pretend they are all "unknown", verify we can parse the datagram header correctly and all samples, see them go through to gravwell, then we revisit this.


// Vendor specific

// UnknownSample refers to a vendor specific sample.
type UnknownSample []byte

func (us *UnknownSample) GetHeader() (*SampleHeader, error) {
	raw := *us
	if len(raw) < MinSampleHeaderSize {
		return nil, ErrSampleHeaderTooShort
	}

	return &SampleHeader{
		Format: binary.BigEndian.Uint32(raw),
		Length: binary.BigEndian.Uint32(raw[SampleHeaderFormatSize:SampleHeaderLengthSize]),
	}, nil
}
