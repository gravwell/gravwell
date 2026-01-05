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

//go:embed sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104.bin
var Sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Bytes []byte

var Sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 232,
	Uptime:         1006962,
	SamplesCount:   2,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             47,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              47000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 17},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 2, SndMss: 1424, RcvMss: 1424, Unacked: 25, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 57553, Rttvar: 612, SndCwnd: 25, Reordering: 3, MinRtt: 55244},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1494, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{184, 213, 38, 248, 69, 52, 200, 163, 98, 18, 192, 115, 8, 0, 69, 0, 5, 196, 110, 92, 64, 0, 64, 6, 191, 34, 10, 0, 0, 77, 140, 82, 113, 22, 157, 202, 1, 187, 44, 157, 104, 175, 247, 83, 22, 56, 128, 16, 0, 83, 122, 178, 0, 0, 1, 1, 8, 10, 50, 255, 69, 168, 207, 209, 220, 180, 56, 183, 234, 19, 79, 109, 56, 191, 38, 128, 49, 160, 97, 185, 221, 107, 247, 241, 209, 148, 62, 248, 24, 73, 34, 89, 253, 149, 95, 55, 68, 158, 238, 197, 224, 155, 133, 17, 62, 128, 11, 97, 48, 137, 49, 152, 207, 75, 119, 111, 65, 96, 23, 255, 229, 248, 250, 176, 51, 149, 245, 81}},
			},
		},
		&datagram.CounterSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 4, Length: 264},
			SequenceNum:             35,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 3, SourceIDIndex: 149361},
			Records: []datagram.Record{
				&datagram.HostAdapters{RecordHeader: datagram.SampleHeader{Format: 2001, Length: 4}, Adapters: []datagram.HostAdapter{}},
				&datagram.VirtDiskIO{RecordHeader: datagram.SampleHeader{Format: 2103, Length: 52}, Capacity: 0, Allocation: 0, Available: 0, RDReq: 0, RDBytes: 0, WRReq: 0, WRBytes: 0, Errors: 0},
				&datagram.VirtMemory{RecordHeader: datagram.SampleHeader{Format: 2102, Length: 16}, Memory: 372736, MaxMemory: 58589417472},
				&datagram.VirtCPU{RecordHeader: datagram.SampleHeader{Format: 2101, Length: 12}, State: 1, CPUTime: 10126, VirtualCPUCount: 0},
				&datagram.VirtNetIO{RecordHeader: datagram.SampleHeader{Format: 2104, Length: 40}, RXBytes: 0, RXPackets: 0, RXErrs: 0, RXDrop: 0, TXBytes: 0, TXPackets: 0, TXErrs: 0, TXDrop: 0},
				&datagram.HostParent{RecordHeader: datagram.SampleHeader{Format: 2002, Length: 8}, ContainerType: 2, ContainerIndex: 1},
				&datagram.HostDescr{RecordHeader: datagram.SampleHeader{Format: 2000, Length: 60}, HostName: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{100, 101, 116, 101, 114, 109, 105, 110, 101, 100, 95, 112, 97, 110, 105, 110, 105}}, UUID: datagram.SFlowUUID{169, 120, 27, 149, 170, 161, 164, 13, 23, 150, 69, 118, 53, 239, 214, 101}, MachineType: 3, OSName: 2, OSRelease: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{54, 46, 49, 56, 46, 48}}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_2209_sample_4_record_2000_2001_2002_2101_2102_2103_2104Decoded, s)
	}
}
