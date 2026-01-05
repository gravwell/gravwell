/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Auto-generated code

package tests

import (
	"bytes"
	_ "embed"

	"net"
	"reflect"
	"testing"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
	"github.com/gravwell/gravwell/v3/sflow/decoder"
)

//go:embed sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104.bin
var Sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Bytes []byte

var Sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 335,
	Uptime:         1459938,
	SamplesCount:   5,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 196},
			SequenceNum:             69,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              69000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 17},
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1292, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{184, 213, 38, 248, 69, 52, 200, 163, 98, 18, 192, 115, 8, 0, 69, 0, 4, 250, 70, 94, 64, 0, 64, 17, 225, 117, 10, 0, 0, 77, 35, 186, 224, 24, 200, 165, 1, 187, 4, 230, 111, 193, 94, 230, 238, 38, 111, 126, 152, 229, 254, 6, 133, 210, 36, 38, 225, 234, 129, 106, 193, 131, 220, 166, 216, 181, 206, 248, 57, 71, 0, 130, 218, 108, 230, 47, 241, 29, 42, 50, 189, 245, 92, 208, 216, 159, 163, 19, 118, 9, 88, 195, 175, 42, 233, 109, 21, 62, 42, 12, 143, 209, 57, 34, 127, 163, 127, 108, 232, 131, 252, 5, 134, 152, 204, 147, 92, 92, 178, 42, 243, 142, 80, 184, 68, 74, 76, 253}},
			},
		},
		&datagram.CounterSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 4, Length: 260},
			SequenceNum:             50,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 3, SourceIDIndex: 114050},
			Records: []datagram.Record{
				&datagram.HostAdapters{RecordHeader: datagram.SampleHeader{Format: 2001, Length: 4}, Adapters: []datagram.HostAdapter{}},
				&datagram.VirtDiskIO{RecordHeader: datagram.SampleHeader{Format: 2103, Length: 52}, Capacity: 0, Allocation: 0, Available: 0, RDReq: 0, RDBytes: 0, WRReq: 0, WRBytes: 0, Errors: 0},
				&datagram.VirtMemory{RecordHeader: datagram.SampleHeader{Format: 2102, Length: 16}, Memory: 4362240, MaxMemory: 58589417472},
				&datagram.VirtCPU{RecordHeader: datagram.SampleHeader{Format: 2101, Length: 12}, State: 1, CPUTime: 2097, VirtualCPUCount: 0},
				&datagram.VirtNetIO{RecordHeader: datagram.SampleHeader{Format: 2104, Length: 40}, RXBytes: 0, RXPackets: 0, RXErrs: 0, RXDrop: 0, TXBytes: 0, TXPackets: 0, TXErrs: 0, TXDrop: 0},
				&datagram.HostParent{RecordHeader: datagram.SampleHeader{Format: 2002, Length: 8}, ContainerType: 2, ContainerIndex: 1},
				&datagram.HostDescr{RecordHeader: datagram.SampleHeader{Format: 2000, Length: 56}, HostName: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{102, 111, 99, 117, 115, 101, 100, 95, 107, 104, 111, 114, 97, 110, 97}}, UUID: datagram.SFlowUUID{35, 108, 249, 63, 62, 113, 51, 218, 24, 145, 26, 28, 177, 99, 83, 18}, MachineType: 3, OSName: 2, OSRelease: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{54, 46, 49, 56, 46, 48}}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             70,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              70000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 17},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 2, SndMss: 1428, RcvMss: 1428, Unacked: 3, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 33719, Rttvar: 12718, SndCwnd: 10, Reordering: 3, MinRtt: 33502},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 162, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{184, 213, 38, 248, 69, 52, 200, 163, 98, 18, 192, 115, 8, 0, 69, 0, 0, 144, 213, 69, 64, 0, 64, 6, 140, 194, 10, 0, 0, 77, 3, 169, 202, 106, 128, 156, 1, 187, 163, 68, 2, 34, 176, 158, 157, 175, 128, 24, 0, 78, 83, 238, 0, 0, 1, 1, 8, 10, 255, 164, 144, 141, 94, 59, 195, 96, 23, 3, 3, 0, 87, 208, 22, 208, 237, 18, 135, 191, 63, 29, 78, 199, 33, 179, 165, 30, 173, 219, 233, 78, 167, 99, 117, 109, 34, 41, 187, 189, 87, 31, 107, 182, 152, 235, 102, 134, 84, 111, 141, 255, 200, 28, 39, 229, 36, 129, 226, 247, 54, 64, 197, 101, 208, 106, 121, 240, 94, 63}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             71,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              71000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1428, RcvMss: 1428, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 34311, Rttvar: 3742, SndCwnd: 22, Reordering: 3, MinRtt: 32612},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1498, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 200, 147, 23, 0, 0, 248, 6, 70, 171, 99, 84, 117, 204, 10, 0, 0, 77, 1, 187, 192, 212, 73, 229, 171, 253, 33, 166, 92, 157, 128, 24, 0, 173, 217, 83, 0, 0, 1, 1, 8, 10, 23, 35, 135, 73, 154, 49, 59, 9, 145, 164, 38, 236, 83, 9, 241, 44, 193, 200, 5, 20, 200, 210, 138, 67, 0, 244, 212, 242, 125, 112, 84, 193, 224, 135, 131, 205, 143, 176, 188, 154, 34, 111, 119, 120, 63, 20, 88, 219, 181, 189, 0, 169, 204, 19, 122, 78, 54, 1, 186, 243, 182, 70, 155, 243, 197, 226, 200, 159, 120, 129}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 192},
			SequenceNum:             72,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              72000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 17},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 2, SndMss: 1428, RcvMss: 1428, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 34311, Rttvar: 3742, SndCwnd: 22, Reordering: 3, MinRtt: 32612},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 84}, HeaderProtocol: 1, FrameLength: 70, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{184, 213, 38, 248, 69, 52, 200, 163, 98, 18, 192, 115, 8, 0, 69, 0, 0, 52, 23, 241, 64, 0, 64, 6, 63, 102, 10, 0, 0, 77, 99, 84, 117, 204, 192, 212, 1, 187, 33, 166, 92, 157, 73, 231, 240, 29, 128, 16, 2, 171, 161, 208, 0, 0, 1, 1, 8, 10, 154, 49, 59, 69, 23, 35, 135, 98}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded, s)
	}
}
