/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package decoder decodes sflow packets
package decoder

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrUnknownSflowVersion = errors.New("unknown sflow version")
	ErrUnknownIPVersion    = errors.New("unknown ip version")
)

type DatagramDecoder struct {
	r io.Reader
}

func NewDatagramDecoder(r io.Reader) DatagramDecoder {
	return DatagramDecoder{r: r}
}

func (dd *DatagramDecoder) Decode() (*datagram.Datagram, error) {
	// Decode headers first
	dgram := &datagram.Datagram{}
	var err error

	err = binary.Read(dd.r, binary.BigEndian, &dgram.Version)
	if err != nil {
		return nil, err
	}

	// We only support sflow 5
	if dgram.Version != 5 {
		return nil, ErrUnknownSflowVersion
	}

	err = binary.Read(dd.r, binary.BigEndian, &dgram.IPVersion)
	if err != nil {
		return nil, err
	}

	// See https://sflow.org/sflow_version_5.txt, pag 24
	if dgram.IPVersion < 1 || dgram.IPVersion > 2 {
		return nil, ErrUnknownIPVersion
	}

	// IPVersion = 1 -> IP V4
	// IPVersion = 2 -> IP V6
	ipLen := 4
	if dgram.IPVersion == 2 {
		ipLen = 16
	}

	ipBuf := make([]byte, ipLen)
	_, err = dd.r.Read(ipBuf)
	if err != nil {
		return nil, err
	}
	dgram.AgentIP = ipBuf

	err = binary.Read(dd.r, binary.BigEndian, &dgram.SubAgentID)
	if err != nil {
		return nil, err
	}

	err = binary.Read(dd.r, binary.BigEndian, &dgram.SequenceNumber)
	if err != nil {
		return nil, err
	}

	err = binary.Read(dd.r, binary.BigEndian, &dgram.Uptime)
	if err != nil {
		return nil, err
	}

	err = binary.Read(dd.r, binary.BigEndian, &dgram.SamplesCount)
	if err != nil {
		return nil, err
	}

	for i := dgram.SamplesCount; i > 0; i-- {
		sample, err := decodeSample(dd.r)
		if err != nil {
			return nil, err
		}

		dgram.Samples = append(dgram.Samples, sample)
	}

	return dgram, nil
}
