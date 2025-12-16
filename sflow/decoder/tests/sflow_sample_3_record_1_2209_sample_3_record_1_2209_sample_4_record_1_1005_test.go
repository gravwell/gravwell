/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
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

//go:embed sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005.bin
var Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005Bytes []byte

var Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 178,
	Uptime:         762942,
	SamplesCount:   3,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             36,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              36000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 55669, Rttvar: 7799, SndCwnd: 10, Reordering: 3, MinRtt: 53898},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 87, 36, 64, 0, 58, 6, 124, 68, 151, 101, 198, 49, 10, 0, 0, 77, 1, 187, 188, 104, 4, 182, 50, 14, 167, 16, 2, 57, 128, 16, 1, 29, 108, 77, 0, 0, 1, 1, 8, 10, 214, 151, 217, 169, 52, 0, 124, 47, 211, 127, 37, 191, 42, 228, 128, 18, 204, 118, 105, 96, 145, 72, 230, 59, 200, 227, 114, 232, 68, 203, 131, 237, 150, 57, 82, 41, 206, 226, 118, 59, 91, 204, 97, 23, 217, 96, 138, 12, 86, 47, 223, 209, 225, 173, 253, 66, 80, 141, 103, 106, 76, 74, 35, 203, 237, 33, 42, 72, 146, 210}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             37,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              37000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 55669, Rttvar: 7799, SndCwnd: 10, Reordering: 3, MinRtt: 53898},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 89, 229, 64, 0, 58, 6, 121, 131, 151, 101, 198, 49, 10, 0, 0, 77, 1, 187, 188, 104, 4, 197, 43, 126, 167, 16, 2, 57, 128, 16, 1, 29, 63, 58, 0, 0, 1, 1, 8, 10, 214, 151, 218, 9, 52, 0, 124, 143, 228, 201, 226, 82, 68, 59, 235, 211, 238, 157, 188, 22, 154, 252, 44, 223, 94, 241, 237, 217, 207, 5, 255, 159, 92, 93, 34, 49, 77, 54, 169, 16, 230, 231, 147, 161, 12, 204, 28, 142, 107, 161, 92, 73, 181, 17, 186, 104, 43, 140, 75, 177, 105, 156, 13, 172, 247, 199, 188, 113, 249, 20}},
			},
		},
		&datagram.CounterSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 4, Length: 128},
			SequenceNum:             26,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			Records: []datagram.Record{
				&datagram.OpenFlowPortName{RecordHeader: datagram.SampleHeader{Format: 1005, Length: 8}, Name: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{101, 116, 104, 48}}},
				&datagram.CounterIfRecord{RecordHeader: datagram.SampleHeader{Format: 1, Length: 88}, IfIndex: 17, IfType: 6, IfSpeed: 1000000000, IfDirection: 1, IfStatus: 3, IfInOctets: 15886045, IfInUcastPkts: 18383, IfInMulticastPkts: 4294967295, IfInBroadcastPkts: 4294967295, IfInDiscards: 0, IfInErrors: 0, IfInUnknownProtos: 4294967295, IfOutOctets: 3721444, IfOutUcastPkts: 9862, IfOutMulticastPkts: 4294967295, IfOutBroadcastPkts: 4294967295, IfOutDiscards: 0, IfOutErrors: 0, IfPromiscuousMode: 0},
			},
		},
	},
}

func TestSflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_4_record_1_1005Decoded, s)
	}
}
