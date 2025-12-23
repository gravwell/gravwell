/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package datagram

const (
	FlowSampleFormat         = 1
	FlowSampleExpandedFormat = 3
)

// FlowSample see https://sflow.org/sflow_version_5.txt, pag 28, `flow_sample`
type FlowSample struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSource
	SamplingRate uint32
	SamplePool   uint32
	Drops        uint32
	Input        Interface
	Output       Interface
	Records      []Record
}

func (fs *FlowSample) GetHeader() SampleHeader {
	return fs.SampleHeader
}

// FlowSampleExpanded see https://sflow.org/sflow_version_5.txt, pag 30, `flow_sample_expanded`
type FlowSampleExpanded struct {
	SampleHeader
	SequenceNum uint32
	SFlowDataSourceExpanded
	SamplingRate uint32
	SamplePool   uint32
	Drops        uint32
	Input        InterfaceExpanded
	Output       InterfaceExpanded
	Records      []Record
}

func (fse *FlowSampleExpanded) GetHeader() SampleHeader {
	return fse.SampleHeader
}

// FlowSampledHeader see https://sflow.org/sflow_version_5.txt, pag 34 `sampled_header`
type FlowSampledHeader struct {
	RecordHeader
	// Indicates the starting layer (see `header_protocol` enum in spec).
	HeaderProtocol uint32
	FrameLength    uint32
	Stripped       uint32
	// Contains raw packet bytes - use a packet parser like google/gopacket to decode.
	HeaderBytes XDRVariableLengthOpaque
}

func (fsh *FlowSampledHeader) GetHeader() RecordHeader {
	return fsh.RecordHeader
}

// NOTE  FlowSampledHeader is variable length, so no way to validate it

const FlowSampledHeaderRecordDataFormatValue uint32 = 1

// SampledEthernet see https://sflow.org/sflow_version_5.txt, pag 35 `sampled_ethernet`
type SampledEthernet struct {
	RecordHeader
	FrameLength uint32
	SrcMAC      XDRMACAddress
	DstMAC      XDRMACAddress
	Type        uint32
}

func (se *SampledEthernet) GetHeader() RecordHeader {
	return se.RecordHeader
}

var SampledEthernetRecordValidLength = packetSizeOf(SampledEthernet{}) - RecordHeaderSize

const SampledEthernetRecordDataFormatValue uint32 = 2

// SampledIPv4 see https://sflow.org/sflow_version_5.txt, pag 35 `sampled_ipv4`
type SampledIPv4 struct {
	RecordHeader
	FrameLength uint32
	Protocol    uint32
	SrcIP       IPv4
	DstIP       IPv4
	SrcPort     uint32
	DstPort     uint32
	TCPFlags    uint32
	TOS         uint32
}

func (si *SampledIPv4) GetHeader() RecordHeader {
	return si.RecordHeader
}

var SampledIPv4RecordValidLength = packetSizeOf(SampledIPv4{}) - RecordHeaderSize

const SampledIPv4RecordDataFormatValue uint32 = 3

// SampledIPv6 see https://sflow.org/sflow_version_5.txt, pag 36 `sampled_ipv6`
type SampledIPv6 struct {
	RecordHeader
	FrameLength uint32
	Protocol    uint32
	SrcIP       IPv6
	DstIP       IPv6
	SrcPort     uint32
	DstPort     uint32
	TCPFlags    uint32
	Priority    uint32
}

func (si *SampledIPv6) GetHeader() RecordHeader {
	return si.RecordHeader
}

var SampledIPv6RecordValidLength = packetSizeOf(SampledIPv6{}) - RecordHeaderSize

const SampledIPv6RecordDataFormatValue uint32 = 4

// ExtendedSwitch see https://sflow.org/sflow_version_5.txt, pag 36 `extended_switch`
type ExtendedSwitch struct {
	RecordHeader
	SrcVLAN     uint32
	SrcPriority uint32
	DstVLAN     uint32
	DstPriority uint32
}

func (es *ExtendedSwitch) GetHeader() RecordHeader {
	return es.RecordHeader
}

var ExtendedSwitchRecordValidLength = packetSizeOf(ExtendedSwitch{}) - RecordHeaderSize

const ExtendedSwitchRecordDataFormatValue uint32 = 1001

// ExtendedRouter see https://sflow.org/sflow_version_5.txt, pag 36 `extended_router`
type ExtendedRouter struct {
	RecordHeader
	NextHop    Address
	SrcMaskLen uint32
	DstMaskLen uint32
}

func (er *ExtendedRouter) GetHeader() RecordHeader {
	return er.RecordHeader
}

const (
	ExtendedRouterRecordUnknownLength   uint32 = 12 // 4 (type) + 0 (no IP) + 4 (SrcMask) + 4 (DstMask)
	ExtendedRouterRecordIPv4Length      uint32 = 16 // 4 (type) + 4 (IPv4) + 4 (SrcMask) + 4 (DstMask)
	ExtendedRouterRecordIPv6Length      uint32 = 28 // 4 (type) + 16 (IPv6) + 4 (SrcMask) + 4 (DstMask)
	ExtendedRouterRecordDataFormatValue uint32 = 1002
)

// ExtendedGateway see https://sflow.org/sflow_version_5.txt, pag 37 `extended_gateway`
type ExtendedGateway struct {
	RecordHeader
	NextHop     Address
	AS          uint32
	SrcAS       uint32
	SrcPeerAS   uint32
	DstASPath   []ASPathSegment
	Communities XDRVariableLengthArray
	LocalPref   uint32
}

func (eg *ExtendedGateway) GetHeader() RecordHeader {
	return eg.RecordHeader
}

// NOTE ExtendedGateway is variable length, so no way to validate it

const ExtendedGatewayRecordDataFormatValue uint32 = 1003

// ExtendedUser see https://sflow.org/sflow_version_5.txt, pag 38 `extended_user`
type ExtendedUser struct {
	RecordHeader
	SrcCharset uint32
	SrcUser    XDRString
	DstCharset uint32
	DstUser    XDRString
}

func (eu *ExtendedUser) GetHeader() RecordHeader {
	return eu.RecordHeader
}

const ExtendedUserRecordDataFormatValue uint32 = 1004

// ExtendedNAT see https://sflow.org/sflow_version_5.txt, pag 39 `extended_nat`
type ExtendedNAT struct {
	RecordHeader
	SrcAddress Address
	DstAddress Address
}

func (en *ExtendedNAT) GetHeader() RecordHeader {
	return en.RecordHeader
}

const ExtendedNATRecordDataFormatValue uint32 = 1007

// ExtendedTCPInfo see https://blog.sflow.com/2016/10/network-performance-monitoring.html and https://groups.google.com/g/sflow/c/JCG9iwacLZA
type ExtendedTCPInfo struct {
	RecordHeader
	Dir        uint32
	SndMss     uint32
	RcvMss     uint32
	Unacked    uint32
	Lost       uint32
	Retrans    uint32
	Pmtu       uint32
	Rtt        uint32
	Rttvar     uint32
	SndCwnd    uint32
	Reordering uint32
	MinRtt     uint32
}

func (eti *ExtendedTCPInfo) GetHeader() RecordHeader {
	return eti.RecordHeader
}

var ExtendedTCPInfoRecordValidLength = packetSizeOf(ExtendedTCPInfo{}) - RecordHeaderSize

const ExtendedTCPInfoRecordDataFormatValue uint32 = 2209

// ExtendedSocketIPv4 see https://sflow.org/sflow_host.txt, pag 9, `extended_socket_ipv4`
type ExtendedSocketIPv4 struct {
	RecordHeader
	Protocol   uint32
	LocalIP    IPv4
	RemoteIP   IPv4
	LocalPort  uint32
	RemotePort uint32
}

func (esi *ExtendedSocketIPv4) GetHeader() RecordHeader {
	return esi.RecordHeader
}

var ExtendedSocketIPv4RecordValidLength = packetSizeOf(ExtendedSocketIPv4{}) - RecordHeaderSize

const ExtendedSocketIPv4RecordDataFormatValue uint32 = 2100

// ExtendedSocketIPv6 see https://sflow.org/sflow_host.txt, pag 9, `extended_socket_ipv6`
type ExtendedSocketIPv6 struct {
	RecordHeader
	Protocol   uint32
	LocalIP    IPv6
	RemoteIP   IPv6
	LocalPort  uint32
	RemotePort uint32
}

func (esi *ExtendedSocketIPv6) GetHeader() RecordHeader {
	return esi.RecordHeader
}

var ExtendedSocketIPv6RecordValidLength = packetSizeOf(ExtendedSocketIPv6{}) - RecordHeaderSize

const ExtendedSocketIPv6RecordDataFormatValue uint32 = 2101

// ExtendedMPLS see https://sflow.org/sflow_version_5.txt, pag 39 `extended_mpls`
type ExtendedMPLS struct {
	RecordHeader
	NextHop  Address
	InStack  XDRVariableLengthArray
	OutStack XDRVariableLengthArray
}

func (em *ExtendedMPLS) GetHeader() RecordHeader {
	return em.RecordHeader
}

// NOTE ExtendedMPLS is variable length, so no way to validate it

const ExtendedMPLSRecordDataFormatValue uint32 = 1006

// ExtendedMPLSTunnel see https://sflow.org/sflow_version_5.txt, pag 40 `extended_mpls_tunnel`
type ExtendedMPLSTunnel struct {
	RecordHeader
	TunnelName XDRString
	TunnelID   uint32
	TunnelCOS  uint32
}

func (emt *ExtendedMPLSTunnel) GetHeader() RecordHeader {
	return emt.RecordHeader
}

// NOTE ExtendedMPLSTunnel is variable length, so no way to validate it

const ExtendedMPLSTunnelRecordDataFormatValue uint32 = 1008

// ExtendedMPLSVC see https://sflow.org/sflow_version_5.txt, pag 40 `extended_mpls_vc`
type ExtendedMPLSVC struct {
	RecordHeader
	VCInstanceName XDRString
	VLLVCID        uint32
	VCLabelCOS     uint32
}

func (emv *ExtendedMPLSVC) GetHeader() RecordHeader {
	return emv.RecordHeader
}

// NOTE ExtendedMPLSVC is variable length, so no way to validate it

const ExtendedMPLSVCRecordDataFormatValue uint32 = 1009

// ExtendedMPLSFTN see https://sflow.org/sflow_version_5.txt, pag 40 `extended_mpls_FTN`
type ExtendedMPLSFTN struct {
	RecordHeader
	Description XDRString
	Mask        uint32
}

func (emf *ExtendedMPLSFTN) GetHeader() RecordHeader {
	return emf.RecordHeader
}

// NOTE ExtendedMPLSFTN is variable length, so no way to validate it

const ExtendedMPLSFTNRecordDataFormatValue uint32 = 1010

// ExtendedMPLSLDPFEC see https://sflow.org/sflow_version_5.txt, pag 40 `extended_mpls_LDP_FEC`
type ExtendedMPLSLDPFEC struct {
	RecordHeader
	AddrPrefixLength uint32
}

func (emlf *ExtendedMPLSLDPFEC) GetHeader() RecordHeader {
	return emlf.RecordHeader
}

var ExtendedMPLSLDPFECRecordValidLength = packetSizeOf(ExtendedMPLSLDPFEC{}) - RecordHeaderSize

const ExtendedMPLSLDPFECRecordDataFormatValue uint32 = 1011

// ExtendedVLANTunnel see https://sflow.org/sflow_version_5.txt, pag 41 `extended_vlantunnel`
type ExtendedVLANTunnel struct {
	RecordHeader
	Stack XDRVariableLengthArray
}

func (evt *ExtendedVLANTunnel) GetHeader() RecordHeader {
	return evt.RecordHeader
}

// NOTE ExtendedVLANTunnel is variable length, so no way to validate it

const ExtendedVLANTunnelRecordDataFormatValue uint32 = 1012

// ExtendedEgressQueue see https://sflow.org/sflow_drops.txt, pag 5 `extended_egress_queue`
type ExtendedEgressQueue struct {
	RecordHeader
	Queue uint32
}

func (eeq *ExtendedEgressQueue) GetHeader() RecordHeader {
	return eeq.RecordHeader
}

var ExtendedEgressQueueRecordValidLength = packetSizeOf(ExtendedEgressQueue{}) - RecordHeaderSize

const ExtendedEgressQueueRecordDataFormatValue uint32 = 1036

// ExtendedACL see https://sflow.org/sflow_drops.txt, pag 6 `extended_acl`
type ExtendedACL struct {
	RecordHeader
	Number    uint32
	Name      XDRString
	Direction uint32
}

func (eacl *ExtendedACL) GetHeader() RecordHeader {
	return eacl.RecordHeader
}

// NOTE ExtendedACL is variable length, so no way to validate it

const ExtendedACLRecordDataFormatValue uint32 = 1037

// ExtendedFunction see https://sflow.org/sflow_drops.txt, pag 6 `extended_function`
type ExtendedFunction struct {
	RecordHeader
	Symbol XDRString
}

func (ef *ExtendedFunction) GetHeader() RecordHeader {
	return ef.RecordHeader
}

// NOTE ExtendedFunction is variable length, so no way to validate it

const ExtendedFunctionRecordDataFormatValue uint32 = 1038

// ExtendedLinuxReason see https://sflow.org/developers/structures.php `extended_linux_drop_reason`
type ExtendedLinuxReason struct {
	RecordHeader
	Reason XDRString
}

func (elr *ExtendedLinuxReason) GetHeader() RecordHeader {
	return elr.RecordHeader
}

// NOTE ExtendedLinuxReason is variable length, so no way to validate it

const ExtendedLinuxReasonRecordDataFormatValue uint32 = 1042
