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

func decodeExtendedTCPInfo(r io.Reader) (*datagram.ExtendedTCPInfo, error) {
	eti := datagram.ExtendedTCPInfo{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.FlowSampledHeaderRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Length); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Dir); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.SndMss); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.RcvMss); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Unacked); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Lost); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Retrans); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Pmtu); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Rtt); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Rttvar); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.SndCwnd); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.Reordering); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eti.MinRtt); err != nil {
		return nil, err
	}

	return &eti, nil
}

func decodeFlowSampledHeader(r io.Reader) (*datagram.FlowSampledHeader, error) {
	dfsh := datagram.FlowSampledHeader{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.FlowSampledHeaderRecordDataFormatValue,
		},
	}

	if err := binary.Read(r, binary.BigEndian, &dfsh.Length); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &dfsh.HeaderProtocol); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &dfsh.FrameLength); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &dfsh.Stripped); err != nil {
		return nil, err
	}

	b, err := decodeXDRVariableLengthOpaque(r)
	if err != nil {
		return nil, err
	}

	dfsh.HeaderBytes = b

	return &dfsh, nil
}
