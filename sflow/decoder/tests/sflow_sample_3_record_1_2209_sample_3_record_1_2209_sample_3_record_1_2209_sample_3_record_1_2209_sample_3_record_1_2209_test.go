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

//go:embed sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209.bin
var Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209Bytes []byte

var Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 256,
	Uptime:         1106059,
	SamplesCount:   5,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 192},
			SequenceNum:             55,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              55000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 17},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 2, SndMss: 1396, RcvMss: 1392, Unacked: 2, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 55152, Rttvar: 9716, SndCwnd: 10, Reordering: 3, MinRtt: 53589},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 84}, HeaderProtocol: 1, FrameLength: 70, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{184, 213, 38, 248, 69, 52, 200, 163, 98, 18, 192, 115, 8, 0, 69, 0, 0, 52, 131, 24, 64, 0, 64, 6, 78, 255, 10, 0, 0, 77, 151, 101, 198, 250, 204, 58, 1, 187, 77, 216, 56, 127, 181, 233, 138, 36, 128, 16, 25, 226, 176, 227, 0, 0, 1, 1, 8, 10, 39, 23, 197, 198, 176, 63, 17, 210, 100, 215}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             56,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              56000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 1, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 55123, Rttvar: 7344, SndCwnd: 10, Reordering: 3, MinRtt: 53589},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 56, 202, 64, 0, 58, 6, 153, 213, 151, 101, 198, 250, 10, 0, 0, 77, 1, 187, 204, 58, 181, 236, 109, 164, 77, 216, 56, 92, 128, 16, 1, 30, 44, 184, 0, 0, 1, 1, 8, 10, 176, 63, 17, 212, 39, 23, 197, 146, 168, 174, 176, 42, 125, 21, 241, 197, 230, 88, 216, 115, 146, 129, 248, 153, 24, 197, 201, 182, 251, 215, 251, 147, 81, 213, 172, 155, 210, 122, 183, 85, 185, 6, 149, 123, 2, 212, 38, 73, 120, 212, 229, 86, 72, 116, 226, 253, 232, 252, 106, 57, 160, 206, 101, 219, 20, 174, 115, 32, 241, 117}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             57,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              57000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 1, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 55123, Rttvar: 7344, SndCwnd: 10, Reordering: 3, MinRtt: 53589},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 58, 128, 64, 0, 58, 6, 152, 31, 151, 101, 198, 250, 10, 0, 0, 77, 1, 187, 204, 58, 181, 245, 187, 68, 77, 216, 56, 92, 128, 16, 1, 30, 239, 142, 0, 0, 1, 1, 8, 10, 176, 63, 17, 219, 39, 23, 197, 149, 119, 118, 255, 32, 112, 25, 133, 245, 83, 73, 202, 181, 197, 167, 30, 41, 61, 117, 39, 138, 89, 63, 248, 168, 108, 139, 244, 190, 201, 140, 17, 248, 3, 107, 106, 44, 204, 190, 82, 164, 148, 87, 60, 206, 14, 146, 24, 232, 49, 228, 51, 21, 39, 221, 113, 138, 232, 234, 122, 224, 225, 16}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             58,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              58000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 54851, Rttvar: 4568, SndCwnd: 10, Reordering: 3, MinRtt: 53388},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 62, 82, 64, 0, 58, 6, 148, 77, 151, 101, 198, 250, 10, 0, 0, 77, 1, 187, 204, 58, 182, 10, 81, 138, 77, 216, 57, 19, 128, 16, 1, 36, 133, 227, 0, 0, 1, 1, 8, 10, 176, 63, 18, 78, 39, 23, 197, 211, 58, 20, 252, 192, 52, 168, 55, 18, 144, 145, 97, 52, 220, 131, 34, 152, 169, 229, 38, 132, 19, 251, 121, 154, 150, 232, 168, 219, 158, 19, 94, 254, 225, 6, 208, 113, 140, 57, 29, 61, 18, 123, 36, 129, 52, 219, 188, 193, 48, 122, 221, 90, 251, 85, 168, 190, 84, 217, 225, 212, 122, 65}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             59,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              59000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1396, RcvMss: 1392, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 54851, Rttvar: 4568, SndCwnd: 10, Reordering: 3, MinRtt: 53388},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1462, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 8, 5, 164, 63, 118, 64, 0, 58, 6, 147, 41, 151, 101, 198, 250, 10, 0, 0, 77, 1, 187, 204, 58, 182, 16, 133, 74, 77, 216, 57, 19, 128, 16, 1, 36, 150, 199, 0, 0, 1, 1, 8, 10, 176, 63, 18, 81, 39, 23, 197, 211, 198, 250, 35, 54, 91, 235, 93, 148, 243, 107, 189, 24, 75, 48, 78, 54, 170, 163, 18, 100, 65, 62, 213, 66, 177, 221, 62, 186, 185, 247, 238, 85, 199, 168, 127, 226, 97, 72, 129, 26, 220, 90, 81, 208, 68, 244, 158, 209, 151, 163, 104, 22, 11, 74, 174, 26, 198, 105, 20, 88, 82, 225}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209_sample_3_record_1_2209Decoded, s)
	}
}
