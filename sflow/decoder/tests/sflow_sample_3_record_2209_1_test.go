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

//go:embed sflow_sample_3_record_2209_1.bin
var Sflow_sample_3_record_2209_1Bytes []byte

var Sflow_sample_3_record_2209_1Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 287,
	Uptime:         1247929,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 252},
			SequenceNum:             62,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              62000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.ExtendedTCPInfo{RecordHeader: datagram.SampleHeader{Format: 2209, Length: 48}, Dir: 1, SndMss: 1298, RcvMss: 1298, Unacked: 0, Lost: 0, Retrans: 0, Pmtu: 1500, Rtt: 165989, Rttvar: 62590, SndCwnd: 10, Reordering: 3, MinRtt: 164974},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1368, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 5, 70, 170, 208, 64, 0, 55, 6, 77, 71, 94, 130, 221, 203, 10, 0, 0, 77, 1, 187, 174, 166, 181, 132, 188, 71, 27, 225, 57, 235, 128, 16, 1, 238, 211, 138, 0, 0, 1, 1, 8, 10, 79, 148, 211, 7, 193, 211, 24, 122, 22, 3, 3, 0, 122, 2, 0, 0, 118, 3, 3, 164, 157, 100, 41, 139, 51, 4, 116, 132, 175, 206, 189, 134, 16, 127, 139, 234, 27, 199, 9, 248, 204, 214, 103, 157, 41, 43, 158, 54, 218, 217, 243, 32, 7, 29, 165, 248, 102, 249, 165, 249, 110, 205, 173, 5, 149, 36, 176, 37, 123, 239}},
			},
		},
	},
}

func TestSflow_sample_3_record_2209_1(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_2209_1Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_2209_1Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_2209_1Decoded, s)
	}
}
