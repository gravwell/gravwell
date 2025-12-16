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

//go:embed sflow_sample_3_record_1_2209.bin
var Sflow_sample_3_record_1_2209Bytes []byte

var Sflow_sample_3_record_1_2209Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 310,
	Uptime:         1354916,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             65,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              65000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1400, RcvMss: 1400, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 50003, Rttvar: 1063, SndCwnd: 10, Reordering: 3, MinRtt: 48517},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 136, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 0, 118, 52, 16, 0, 0, 121, 6, 81, 220, 142, 251, 34, 78, 10, 0, 0, 77, 1, 187, 141, 26, 237, 222, 162, 96, 14, 93, 30, 63, 128, 24, 2, 32, 227, 216, 0, 0, 1, 1, 8, 10, 171, 23, 156, 110, 225, 211, 47, 229, 23, 3, 3, 0, 61, 162, 16, 46, 228, 181, 47, 227, 47, 47, 12, 56, 13, 249, 231, 124, 144, 5, 155, 77, 9, 13, 125, 109, 202, 219, 220, 116, 253, 100, 94, 207, 247, 13, 185, 33, 92, 50, 73, 111, 252, 55, 237, 37, 249, 22, 3, 34, 82, 22, 114, 27, 16, 50, 161, 1, 49, 107}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_2209(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_2209Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_2209Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_2209Decoded, s)
	}
}
