/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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
	ErrInvalidExtendedTCPInfoRecordSize     = errors.New("extended tcp info record size is invalid")
	ErrInvalidFlowSampledHeaderRecordSize   = errors.New("flow sampled header record size is invalid")
	ErrInvalidSampledEthernetRecordSize     = errors.New("sampled ethernet record size is invalid")
	ErrInvalidSampledIPv4RecordSize         = errors.New("sampled ipv4 record size is invalid")
	ErrInvalidSampledIPv6RecordSize         = errors.New("sampled ipv6 record size is invalid")
	ErrInvalidExtendedSwitchRecordSize      = errors.New("extended switch record size is invalid")
	ErrInvalidExtendedRouterRecordSize      = errors.New("extended router record size is invalid")
	ErrInvalidExtendedGatewayRecordSize     = errors.New("extended gateway record size is invalid")
	ErrInvalidExtendedUserRecordSize        = errors.New("extended user record size is invalid")
	ErrInvalidExtendedNATRecordSize         = errors.New("extended nat record size is invalid")
	ErrInvalidExtendedSocketIPv4RecordSize  = errors.New("extended socket ipv4 record size is invalid")
	ErrInvalidExtendedSocketIPv6RecordSize  = errors.New("extended socket ipv6 record size is invalid")
	ErrInvalidExtendedMPLSRecordSize        = errors.New("extended mpls record size is invalid")
	ErrInvalidExtendedMPLSTunnelRecordSize  = errors.New("extended mpls tunnel record size is invalid")
	ErrInvalidExtendedMPLSVCRecordSize      = errors.New("extended mpls vc record size is invalid")
	ErrInvalidExtendedMPLSFTNRecordSize     = errors.New("extended mpls ftn record size is invalid")
	ErrInvalidExtendedMPLSLDPFECRecordSize  = errors.New("extended mpls ldp fec record size is invalid")
	ErrInvalidExtendedVLANTunnelRecordSize  = errors.New("extended vlan tunnel record size is invalid")
	ErrInvalidExtendedEgressQueueRecordSize = errors.New("extended egress queue record size is invalid")
	ErrInvalidExtendedACLRecordSize         = errors.New("extended acl record size is invalid")
	ErrInvalidExtendedFunctionRecordSize    = errors.New("extended function record size is invalid")
	ErrInvalidExtendedLinuxReasonRecordSize = errors.New("extended linux reason record size is invalid")
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

	beforeN := r.N

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

	if beforeN-r.N != int64(dfsh.Length) {
		return nil, ErrInvalidFlowSampledHeaderRecordSize
	}

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

	beforeN := r.N

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

	if beforeN-r.N != int64(eg.Length) {
		return nil, ErrInvalidExtendedGatewayRecordSize
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

	beforeN := r.N

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

	if beforeN-r.N != int64(eu.Length) {
		return nil, ErrInvalidExtendedUserRecordSize
	}

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
	expectedLength := uint32(8 + len(srcAddr.IP) + len(dstAddr.IP))
	if en.Length != expectedLength {
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

func decodeExtendedMPLS(r *io.LimitedReader) (*datagram.ExtendedMPLS, error) {
	em := datagram.ExtendedMPLS{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedMPLSRecordDataFormatValue,
		},
	}

	var err error
	em.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	nextHop, err := decodeAddress(r)
	if err != nil {
		return nil, err
	}
	em.NextHop = nextHop

	inStack, err := decodeXDRVariableLengthArray(r)
	if err != nil {
		return nil, err
	}
	em.InStack = inStack

	outStack, err := decodeXDRVariableLengthArray(r)
	if err != nil {
		return nil, err
	}
	em.OutStack = outStack

	if beforeN-r.N != int64(em.Length) {
		return nil, ErrInvalidExtendedMPLSRecordSize
	}

	return &em, nil
}

func decodeExtendedMPLSTunnel(r *io.LimitedReader) (*datagram.ExtendedMPLSTunnel, error) {
	emt := datagram.ExtendedMPLSTunnel{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedMPLSTunnelRecordDataFormatValue,
		},
	}

	var err error
	emt.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	tunnelName, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	emt.TunnelName = tunnelName

	if err := binary.Read(r, binary.BigEndian, &emt.TunnelID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &emt.TunnelCOS); err != nil {
		return nil, err
	}

	if beforeN-r.N != int64(emt.Length) {
		return nil, ErrInvalidExtendedMPLSTunnelRecordSize
	}

	return &emt, nil
}

func decodeExtendedMPLSVC(r *io.LimitedReader) (*datagram.ExtendedMPLSVC, error) {
	emv := datagram.ExtendedMPLSVC{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedMPLSVCRecordDataFormatValue,
		},
	}

	var err error
	emv.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	vcInstanceName, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	emv.VCInstanceName = vcInstanceName

	if err := binary.Read(r, binary.BigEndian, &emv.VLLVCID); err != nil {
		return nil, err
	}

	if err := binary.Read(r, binary.BigEndian, &emv.VCLabelCOS); err != nil {
		return nil, err
	}

	if beforeN-r.N != int64(emv.Length) {
		return nil, ErrInvalidExtendedMPLSVCRecordSize
	}

	return &emv, nil
}

func decodeExtendedMPLSFTN(r *io.LimitedReader) (*datagram.ExtendedMPLSFTN, error) {
	emf := datagram.ExtendedMPLSFTN{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedMPLSFTNRecordDataFormatValue,
		},
	}

	var err error
	emf.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	description, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	emf.Description = description

	if err := binary.Read(r, binary.BigEndian, &emf.Mask); err != nil {
		return nil, err
	}

	if beforeN-r.N != int64(emf.Length) {
		return nil, ErrInvalidExtendedMPLSFTNRecordSize
	}

	return &emf, nil
}

func decodeExtendedMPLSLDPFEC(r *io.LimitedReader) (*datagram.ExtendedMPLSLDPFEC, error) {
	emlf := datagram.ExtendedMPLSLDPFEC{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedMPLSLDPFECRecordDataFormatValue,
		},
	}

	var err error
	emlf.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if emlf.Length != uint32(datagram.ExtendedMPLSLDPFECRecordValidLength) {
		return nil, ErrInvalidExtendedMPLSLDPFECRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &emlf.AddrPrefixLength); err != nil {
		return nil, err
	}

	return &emlf, nil
}

func decodeExtendedVLANTunnel(r *io.LimitedReader) (*datagram.ExtendedVLANTunnel, error) {
	evt := datagram.ExtendedVLANTunnel{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedVLANTunnelRecordDataFormatValue,
		},
	}

	var err error
	evt.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	stack, err := decodeXDRVariableLengthArray(r)
	if err != nil {
		return nil, err
	}
	evt.Stack = stack

	if beforeN-r.N != int64(evt.Length) {
		return nil, ErrInvalidExtendedVLANTunnelRecordSize
	}

	return &evt, nil
}

func decodeExtendedEgressQueue(r *io.LimitedReader) (*datagram.ExtendedEgressQueue, error) {
	eeq := datagram.ExtendedEgressQueue{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedEgressQueueRecordDataFormatValue,
		},
	}

	var err error
	eeq.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	if eeq.Length != uint32(datagram.ExtendedEgressQueueRecordValidLength) {
		return nil, ErrInvalidExtendedEgressQueueRecordSize
	}

	if err := binary.Read(r, binary.BigEndian, &eeq.Queue); err != nil {
		return nil, err
	}

	return &eeq, nil
}

func decodeExtendedACL(r *io.LimitedReader) (*datagram.ExtendedACL, error) {
	eacl := datagram.ExtendedACL{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedACLRecordDataFormatValue,
		},
	}

	var err error
	eacl.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	if err := binary.Read(r, binary.BigEndian, &eacl.Number); err != nil {
		return nil, err
	}

	name, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	eacl.Name = name

	if err := binary.Read(r, binary.BigEndian, &eacl.Direction); err != nil {
		return nil, err
	}

	if beforeN-r.N != int64(eacl.Length) {
		return nil, ErrInvalidExtendedACLRecordSize
	}

	return &eacl, nil
}

func decodeExtendedFunction(r *io.LimitedReader) (*datagram.ExtendedFunction, error) {
	ef := datagram.ExtendedFunction{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedFunctionRecordDataFormatValue,
		},
	}

	var err error
	ef.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	symbol, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	ef.Symbol = symbol

	if beforeN-r.N != int64(ef.Length) {
		return nil, ErrInvalidExtendedFunctionRecordSize
	}

	return &ef, nil
}

func decodeExtendedLinuxReason(r *io.LimitedReader) (*datagram.ExtendedLinuxReason, error) {
	elr := datagram.ExtendedLinuxReason{
		RecordHeader: datagram.RecordHeader{
			Format: datagram.ExtendedLinuxReasonRecordDataFormatValue,
		},
	}

	var err error
	elr.Length, err = decodeLength(r, BytesPerLength)
	if err != nil {
		return nil, err
	}

	beforeN := r.N

	reason, err := decodeXDRString(r)
	if err != nil {
		return nil, err
	}
	elr.Reason = reason

	if beforeN-r.N != int64(elr.Length) {
		return nil, ErrInvalidExtendedLinuxReasonRecordSize
	}

	return &elr, nil
}
