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

//go:embed sflow_sample_3_record_1_2209_sample_3_record_1_2209.bin
var Sflow_sample_3_record_1_2209_sample_3_record_1_2209Bytes []byte

var Sflow_sample_3_record_1_2209_sample_3_record_1_2209Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 159,
	Uptime:         674969,
	SamplesCount:   2,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             33,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              33000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1372, RcvMss: 1436, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 23016, Rttvar: 1851, SndCwnd: 14, Reordering: 3, MinRtt: 22086},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1506, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 208, 200, 217, 64, 0, 55, 6, 115, 101, 23, 215, 223, 197, 10, 0, 0, 77, 1, 187, 211, 126, 173, 61, 148, 117, 241, 45, 176, 71, 128, 16, 1, 245, 42, 150, 0, 0, 1, 1, 8, 10, 9, 98, 47, 255, 138, 254, 199, 247, 159, 5, 164, 207, 69, 206, 147, 40, 86, 92, 160, 247, 110, 194, 194, 112, 102, 214, 130, 166, 144, 52, 26, 43, 102, 52, 180, 200, 41, 124, 169, 141, 231, 8, 174, 242, 52, 121, 63, 132, 159, 177, 209, 5, 70, 92, 114, 160, 223, 47, 146, 21, 50, 182, 203, 144, 205, 111, 119, 229, 188, 31}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             34,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              34000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1372, RcvMss: 1436, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 23016, Rttvar: 1851, SndCwnd: 14, Reordering: 3, MinRtt: 22086},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1506, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 208, 201, 38, 64, 0, 55, 6, 115, 24, 23, 215, 223, 197, 10, 0, 0, 77, 1, 187, 211, 126, 173, 63, 68, 97, 241, 45, 176, 71, 128, 16, 1, 245, 237, 132, 0, 0, 1, 1, 8, 10, 9, 98, 48, 11, 138, 254, 200, 0, 187, 222, 37, 63, 77, 73, 130, 25, 133, 233, 131, 252, 191, 224, 138, 184, 249, 53, 132, 215, 12, 220, 77, 147, 18, 237, 145, 213, 250, 12, 164, 241, 230, 191, 33, 197, 126, 60, 114, 109, 175, 106, 212, 211, 173, 139, 109, 62, 189, 157, 249, 116, 214, 216, 9, 34, 23, 103, 66, 147, 190, 28}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_2209_sample_3_record_1_2209(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_2209_sample_3_record_1_2209Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_2209_sample_3_record_1_2209Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_2209_sample_3_record_1_2209Decoded, s)
	}
}
