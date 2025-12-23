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

//go:embed sflow_sample_1_record_1_1001.bin
var Sflow_sample_1_record_1_1001Bytes []byte

var Sflow_sample_1_record_1_1001Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{208, 85, 240, 52},
	SubAgentID:     1,
	SequenceNumber: 1260270,
	Uptime:         1382429804,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 208},
			SequenceNum:     4446,
			SFlowDataSource: 4,
			SamplingRate:    4096,
			SamplePool:      18206720,
			Drops:           0,
			Input:           4,
			Output:          1,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 318, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 208, 1, 255, 88, 0, 0, 22, 60, 194, 169, 171, 8, 0, 69, 0, 1, 44, 0, 0, 64, 0, 64, 17, 209, 88, 199, 58, 161, 150, 197, 161, 57, 246, 200, 213, 38, 0, 1, 24, 166, 23, 100, 49, 58, 114, 100, 50, 58, 105, 100, 50, 48, 58, 107, 150, 139, 202, 74, 192, 181, 207, 16, 58, 214, 191, 141, 215, 52, 1, 70, 81, 183, 250, 53, 58, 110, 111, 100, 101, 115, 50, 48, 56, 58, 97, 74, 184, 100, 84, 238, 133, 95, 19, 154, 32, 150, 233, 131, 255, 207, 244, 208, 197, 165, 222, 103, 10, 143, 219, 29, 97, 74, 120, 18, 131, 49, 163, 119, 134, 104, 94, 28, 36, 206, 51, 25, 222}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
	},
}

func TestSflow_sample_1_record_1_1001(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_1_record_1_1001Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_1_record_1_1001Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_1_record_1_1001Decoded, s)
	}
}
