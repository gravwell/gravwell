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
	"errors"
	"io"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

var (
	ErrInvalidExtendedTCPInfoRecordSize    = errors.New("extended tcp info record size is invalid")
	ErrInvalidSampledEthernetRecordSize    = errors.New("sampled ethernet record size is invalid")
	ErrInvalidSampledIPv4RecordSize        = errors.New("sampled ipv4 record size is invalid")
	ErrInvalidSampledIPv6RecordSize        = errors.New("sampled ipv6 record size is invalid")
	ErrInvalidExtendedSwitchRecordSize     = errors.New("extended switch record size is invalid")
	ErrInvalidExtendedRouterRecordSize     = errors.New("extended router record size is invalid")
	ErrInvalidExtendedNATRecordSize        = errors.New("extended nat record size is invalid")
	ErrInvalidExtendedSocketIPv4RecordSize = errors.New("extended socket ipv4 record size is invalid")
	ErrInvalidExtendedSocketIPv6RecordSize = errors.New("extended socket ipv6 record size is invalid")
)

func decodeExtendedTCPInfo(r *io.LimitedReader) (*datagram.ExtendedTCPInfo, error) {
	eti := datagram.ExtendedTCPInfo{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedTCPInfoRecordDataFormatValue,
		},
	}

	var err error
	eti.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if eti.Length != uint32(datagram.ExtendedTCPInfoRecordValidLength) {
		return nil, ErrInvalidExtendedTCPInfoRecordSize
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

func decodeFlowSampledHeader(r *io.LimitedReader) (*datagram.FlowSampledHeader, error) {
	dfsh := datagram.FlowSampledHeader{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.FlowSampledHeaderRecordDataFormatValue,
		},
	}

	var err error
	dfsh.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
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

func decodeSampledEthernet(r *io.LimitedReader) (*datagram.SampledEthernet, error) {
	se := datagram.SampledEthernet{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.SampledEthernetRecordDataFormatValue,
		},
	}

	var err error
	se.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if se.Length != uint32(datagram.SampledEthernetRecordValidLength) {
		return nil, ErrInvalidSampledEthernetRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &se.FrameLength); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &se.SrcMAC); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &se.DstMAC); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &se.Type); err != nil {
		return nil, err
	}

	return &se, nil
}

func decodeSampledIPv4(r *io.LimitedReader) (*datagram.SampledIPv4, error) {
	si := datagram.SampledIPv4{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.SampledIPv4RecordDataFormatValue,
		},
	}

	var err error
	si.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if si.Length != uint32(datagram.SampledIPv4RecordValidLength) {
		return nil, ErrInvalidSampledIPv4RecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &si.FrameLength); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.Protocol); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.SrcIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.DstIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.SrcPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.DstPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.TCPFlags); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.TOS); err != nil {
		return nil, err
	}

	return &si, nil
}

func decodeSampledIPv6(r *io.LimitedReader) (*datagram.SampledIPv6, error) {
	si := datagram.SampledIPv6{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.SampledIPv6RecordDataFormatValue,
		},
	}

	var err error
	si.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if si.Length != uint32(datagram.SampledIPv6RecordValidLength) {
		return nil, ErrInvalidSampledIPv6RecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &si.FrameLength); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.Protocol); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.SrcIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.DstIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.SrcPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.DstPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.TCPFlags); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &si.Priority); err != nil {
		return nil, err
	}

	return &si, nil
}

func decodeExtendedSwitch(r *io.LimitedReader) (*datagram.ExtendedSwitch, error) {
	es := datagram.ExtendedSwitch{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedSwitchRecordDataFormatValue,
		},
	}

	var err error
	es.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if es.Length != uint32(datagram.ExtendedSwitchRecordValidLength) {
		return nil, ErrInvalidExtendedSwitchRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &es.SrcVLAN); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &es.SrcPriority); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &es.DstVLAN); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &es.DstPriority); err != nil {
		return nil, err
	}

	return &es, nil
}

func decodeExtendedRouter(r *io.LimitedReader) (*datagram.ExtendedRouter, error) {
	er := datagram.ExtendedRouter{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedRouterRecordDataFormatValue,
		},
	}

	var err error
	er.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	// The way I am justifying with the spec is good, but I've been betrayed by it before
	nextHop, err := decodeAddress(r)
	if err != nil {
		return nil, err
	}
	er.NextHop = nextHop

	var expectedLength uint32
	switch nextHop.Type {
	case datagram.AddressTypeUnknown:
		expectedLength = datagram.ExtendedRouterRecordUnknownLength
	case datagram.AddressTypeIPv4:
		expectedLength = datagram.ExtendedRouterRecordIPv4Length
	case datagram.AddressTypeIPv6:
		expectedLength = datagram.ExtendedRouterRecordIPv6Length
	default:
		// This should never get hit, but just in case
		return nil, ErrUnknownAddressIPVersion
	}

	if er.Length != expectedLength {
		return nil, ErrInvalidExtendedRouterRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &er.SrcMaskLen); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &er.DstMaskLen); err != nil {
		return nil, err
	}

	return &er, nil
}

func decodeExtendedGateway(r *io.LimitedReader) (*datagram.ExtendedGateway, error) {
	eg := datagram.ExtendedGateway{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedGatewayRecordDataFormatValue,
		},
	}

	var err error
	eg.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	nextHop, err := decodeAddress(r)
	if err != nil {
		return nil, err
	}
	eg.NextHop = nextHop

	if err := binary.Read(r, binary.BigEndian, &eg.AS); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eg.SrcAS); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &eg.SrcPeerAS); err != nil {
		return nil, err
	}

	var segmentCount uint32
	segmentCount, err = decodeLength(r, MinBytesPerItem)
	if err != nil {
		return nil, err
	}

	eg.DstASPath = make([]datagram.ASPathSegment, 0, segmentCount)
	for range segmentCount {
		var asps datagram.ASPathSegment
		if err := binary.Read(r, binary.BigEndian, &asps.Type); err != nil {
			return nil, err
		}

		asns, err := decodeXDRVariableLengthArray(r)
		if err != nil {
			return nil, err
		}
		asps.ASNs = asns

		eg.DstASPath = append(eg.DstASPath, asps)
	}

	communities, err := decodeXDRVariableLengthArray(r)
	if err != nil {
		return nil, err
	}
	eg.Communities = communities

	if err := binary.Read(r, binary.BigEndian, &eg.LocalPref); err != nil {
		return nil, err
	}

	return &eg, nil
}

func decodeExtendedUser(r *io.LimitedReader) (*datagram.ExtendedUser, error) {
	eu := datagram.ExtendedUser{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedUserRecordDataFormatValue,
		},
	}

	var err error
	eu.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	// NOTE: ExtendedUser is variable length (contains XDR strings), so no way to validate length

	if err := binary.Read(r, binary.BigEndian, &eu.SrcCharset); err != nil {
		return nil, err
	}

	srcUser, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	eu.SrcUser = srcUser

	if err := binary.Read(r, binary.BigEndian, &eu.DstCharset); err != nil {
		return nil, err
	}

	dstUser, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	eu.DstUser = dstUser

	return &eu, nil
}

func decodeExtendedNAT(r *io.LimitedReader) (*datagram.ExtendedNAT, error) {
	en := datagram.ExtendedNAT{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedNATRecordDataFormatValue,
		},
	}

	var err error
	en.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	srcAddr, err := decodeAddress(r)
	if err != nil {
		return nil, err
	}

	dstAddr, err := decodeAddress(r)
	if err != nil {
		return nil, err
	}

	// 2 x uint32 type discriminant + the length of the IPs bytes
	expectedLength := 8 + len(srcAddr.IP) + len(dstAddr.IP)
	if en.Length != uint32(expectedLength) {
		return nil, ErrInvalidExtendedNATRecordSize
	}

	en.SrcAddress = srcAddr
	en.DstAddress = dstAddr

	return &en, nil
}

func decodeExtendedSocketIPv4(r *io.LimitedReader) (*datagram.ExtendedSocketIPv4, error) {
	esi := datagram.ExtendedSocketIPv4{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedSocketIPv4RecordDataFormatValue,
		},
	}

	var err error
	esi.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if esi.Length != uint32(datagram.ExtendedSocketIPv4RecordValidLength) {
		return nil, ErrInvalidExtendedSocketIPv4RecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &esi.Protocol); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.LocalIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.RemoteIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.LocalPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.RemotePort); err != nil {
		return nil, err
	}

	return &esi, nil
}

func decodeExtendedSocketIPv6(r *io.LimitedReader) (*datagram.ExtendedSocketIPv6, error) {
	esi := datagram.ExtendedSocketIPv6{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedSocketIPv6RecordDataFormatValue,
		},
	}

	var err error
	esi.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if esi.Length != uint32(datagram.ExtendedSocketIPv6RecordValidLength) {
		return nil, ErrInvalidExtendedSocketIPv6RecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &esi.Protocol); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.LocalIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.RemoteIP); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.LocalPort); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &esi.RemotePort); err != nil {
		return nil, err
	}

	return &esi, nil
}
