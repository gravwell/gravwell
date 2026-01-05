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

//go:embed sflow_sample_1_record_1.bin
var Sflow_sample_1_record_1Bytes []byte

var Sflow_sample_1_record_1Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{172, 16, 0, 17},
	SubAgentID:     1,
	SequenceNumber: 426,
	Uptime:         1743694337,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 136},
			SequenceNum:     6,
			SFlowDataSource: 1043,
			SamplingRate:    2048,
			SamplePool:      12288,
			Drops:           0,
			Input:           1194,
			Output:          1043,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 96}, HeaderProtocol: 1, FrameLength: 82, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 255, 18, 52, 53, 27, 255, 171, 205, 239, 171, 100, 129, 0, 0, 32, 8, 0, 69, 0, 0, 60, 92, 7, 0, 0, 124, 1, 72, 160, 172, 16, 32, 254, 172, 16, 32, 241, 8, 0, 151, 97, 169, 72, 12, 178, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 97, 98, 99, 100, 101, 102, 103, 104, 105}},
			},
		},
	},
}

func TestSflow_sample_1_record_1(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_1_record_1Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_1_record_1Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_1_record_1Decoded, s)
	}
}
