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
	Input        Interface
	Output       Interface
	Records      []Record
}

func (fse *FlowSampleExpanded) GetHeader() SampleHeader {
	return fse.SampleHeader
}
