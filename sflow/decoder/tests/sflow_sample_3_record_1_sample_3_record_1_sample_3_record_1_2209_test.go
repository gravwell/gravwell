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

//go:embed sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209.bin
var Sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209Bytes []byte

var Sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 337,
	Uptime:         1461967,
	SamplesCount:   3,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 196},
			SequenceNum:             75,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              75000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1298, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 0, 0, 0, 64, 0, 58, 17, 242, 162, 142, 250, 176, 3, 10, 0, 0, 77, 1, 187, 143, 132, 4, 236, 123, 161, 89, 87, 240, 143, 207, 86, 75, 32, 1, 111, 108, 105, 108, 9, 121, 210, 156, 39, 70, 198, 179, 62, 57, 242, 209, 70, 137, 186, 254, 209, 84, 56, 100, 72, 161, 56, 250, 199, 30, 235, 102, 105, 61, 133, 216, 162, 155, 228, 126, 132, 154, 10, 86, 247, 208, 6, 236, 180, 53, 193, 58, 68, 17, 249, 20, 104, 185, 122, 241, 62, 134, 90, 229, 234, 250, 38, 16, 69, 187, 235, 169, 63, 87, 82, 20, 72}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 196},
			SequenceNum:             76,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              76000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1298, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 0, 0, 0, 64, 0, 58, 17, 242, 162, 142, 250, 176, 3, 10, 0, 0, 77, 1, 187, 143, 132, 4, 236, 85, 225, 71, 87, 240, 143, 82, 188, 113, 80, 106, 19, 20, 153, 108, 141, 239, 68, 199, 41, 23, 48, 173, 132, 68, 51, 158, 47, 2, 34, 112, 71, 165, 24, 206, 64, 113, 236, 178, 222, 40, 43, 114, 212, 249, 142, 116, 8, 102, 11, 31, 171, 112, 198, 193, 126, 67, 56, 196, 184, 106, 36, 5, 171, 98, 143, 187, 153, 19, 64, 62, 121, 139, 146, 238, 188, 207, 189, 113, 233, 199, 61, 129, 43, 69, 162, 175, 184}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             77,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              77000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1400, RcvMss: 1400, Unacked: 4, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 50628, Rttvar: 6203, SndCwnd: 12, Reordering: 3, MinRtt: 49351},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1470, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 172, 241, 180, 0, 0, 121, 6, 1, 77, 142, 250, 176, 3, 10, 0, 0, 77, 1, 187, 151, 12, 129, 24, 104, 185, 63, 214, 61, 208, 128, 24, 4, 18, 199, 56, 0, 0, 1, 1, 8, 10, 59, 164, 28, 11, 161, 100, 154, 90, 143, 16, 212, 72, 78, 50, 207, 128, 178, 194, 106, 122, 127, 83, 67, 20, 69, 84, 101, 104, 48, 253, 85, 82, 44, 96, 45, 140, 188, 138, 203, 250, 45, 218, 121, 61, 197, 170, 25, 112, 206, 128, 23, 3, 3, 5, 115, 42, 87, 213, 79, 125, 81, 179, 218, 14, 142, 154, 187, 83, 130, 107}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_sample_3_record_1_sample_3_record_1_2209Decoded, s)
	}
}
