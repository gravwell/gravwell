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

//go:embed sflow_sample_5_record_1036.bin
var Sflow_sample_5_record_1036Bytes []byte

var Sflow_sample_5_record_1036Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{192, 168, 119, 184},
	SubAgentID:     100000,
	SequenceNumber: 3,
	Uptime:         12414,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.DiscardedPacket{
			SampleHeader:            datagram.SampleHeader{Format: 5, Length: 44},
			SequenceNum:             2,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1},
			Drops:                   256,
			Input:                   1,
			Output:                  2,
			DiscardReason:           1,
			Records: []datagram.Record{
				&datagram.ExtendedEgressQueue{RecordHeader: datagram.SampleHeader{Format: 1036, Length: 4}, Queue: 42},
			},
		},
	},
}

func TestSflow_sample_5_record_1036(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_5_record_1036Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_5_record_1036Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_5_record_1036Decoded, s)
	}
}
