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
