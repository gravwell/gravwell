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

//go:embed sflow_sample_1_record_1_1001_sample_1_record_1_1001.bin
var Sflow_sample_1_record_1_1001_sample_1_record_1_1001Bytes []byte

var Sflow_sample_1_record_1_1001_sample_1_record_1_1001Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{208, 85, 240, 52},
	SubAgentID:     1,
	SequenceNumber: 6234811,
	Uptime:         3320957916,
	SamplesCount:   2,
	Samples: []datagram.Sample{
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 140},
			SequenceNum:     2804568,
			SFlowDataSource: 3,
			SamplingRate:    512,
			SamplePool:      2394376381,
			Drops:           0,
			Input:           3,
			Output:          1,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 76}, HeaderProtocol: 1, FrameLength: 64, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 0, 12, 159, 240, 1, 0, 37, 144, 80, 125, 167, 8, 0, 69, 0, 0, 40, 144, 126, 64, 0, 64, 6, 183, 153, 199, 58, 161, 130, 216, 87, 177, 163, 0, 80, 207, 210, 102, 213, 232, 213, 72, 128, 213, 78, 80, 16, 0, 63, 127, 64, 0, 0, 0, 0, 0, 0, 0, 0}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 168},
			SequenceNum:     241405,
			SFlowDataSource: 5,
			SamplingRate:    512,
			SamplePool:      395679243,
			Drops:           0,
			Input:           5,
			Output:          1,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 104}, HeaderProtocol: 1, FrameLength: 90, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 0, 12, 159, 240, 1, 0, 37, 144, 81, 100, 89, 8, 0, 69, 0, 0, 72, 175, 48, 64, 0, 64, 6, 95, 231, 199, 58, 161, 133, 212, 71, 238, 144, 0, 22, 180, 5, 29, 50, 1, 209, 142, 110, 15, 15, 128, 24, 0, 46, 215, 74, 0, 0, 1, 1, 8, 10, 63, 255, 175, 3, 235, 37, 115, 37, 83, 83, 72, 45, 50, 46, 48, 45, 79, 112, 101, 110, 83, 83, 72, 95, 52, 46, 51, 10}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
	},
}

func TestSflow_sample_1_record_1_1001_sample_1_record_1_1001(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_1_record_1_1001_sample_1_record_1_1001Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_1_record_1_1001_sample_1_record_1_1001Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_1_record_1_1001_sample_1_record_1_1001Decoded, s)
	}
}
