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

//go:embed sflow_sample_5_record_1_1038_1042.bin
var Sflow_sample_5_record_1_1038_1042Bytes []byte

var Sflow_sample_5_record_1_1038_1042Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 30, 1, 254},
	SubAgentID:     100000,
	SequenceNumber: 19263,
	Uptime:         155386440,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.DiscardedPacket{
			SampleHeader:            datagram.SampleHeader{Format: 5, Length: 180},
			SequenceNum:             17145,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 2},
			Drops:                   1799,
			Input:                   2,
			Output:                  0,
			DiscardReason:           289,
			Records: []datagram.Record{
				&datagram.ExtendedLinuxReason{RecordHeader: datagram.SampleHeader{Format: 1042, Length: 20}, Reason: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{78, 79, 84, 95, 83, 80, 69, 67, 73, 70, 73, 69, 68}}},
				&datagram.ExtendedFunction{RecordHeader: datagram.SampleHeader{Format: 1038, Length: 32}, Symbol: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{95, 95, 117, 100, 112, 52, 95, 108, 105, 98, 95, 114, 99, 118, 43, 48, 120, 98, 101, 100, 47, 48, 120, 100, 57, 48}}},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 72}, HeaderProtocol: 1, FrameLength: 56, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{255, 255, 255, 255, 255, 255, 36, 254, 154, 7, 210, 169, 8, 0, 69, 0, 0, 42, 230, 180, 0, 0, 1, 17, 17, 72, 192, 168, 1, 31, 255, 255, 255, 255, 246, 79, 12, 217, 0, 22, 82, 227, 69, 80, 83, 79, 78, 80, 0, 255, 0, 0, 0, 0, 0, 0}},
			},
		},
	},
}

func TestSflow_sample_5_record_1_1038_1042(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_5_record_1_1038_1042Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_5_record_1_1038_1042Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_5_record_1_1038_1042Decoded, s)
	}
}
