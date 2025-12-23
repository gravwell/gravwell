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

//go:embed sflow_sample_2_record_1_2.bin
var Sflow_sample_2_record_1_2Bytes []byte

var Sflow_sample_2_record_1_2Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{208, 85, 240, 52},
	SubAgentID:     1,
	SequenceNumber: 1086077,
	Uptime:         921964804,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.CounterSample{
			SampleHeader:    datagram.SampleHeader{Format: 2, Length: 168},
			SequenceNum:     101239,
			SFlowDataSource: 9,
			Records: []datagram.Record{
				&datagram.EthernetCounters{RecordHeader: datagram.SampleHeader{Format: 2, Length: 52}, Dot3StatsAlignmentErrors: 0, Dot3StatsFCSErrors: 0, Dot3StatsSingleCollisionFrames: 0, Dot3StatsMultipleCollisionFrames: 0, Dot3StatsSQETestErrors: 0, Dot3StatsDeferredTransmissions: 0, Dot3StatsLateCollisions: 0, Dot3StatsExcessiveCollisions: 0, Dot3StatsInternalMacTransmitErrors: 0, Dot3StatsCarrierSenseErrors: 0, Dot3StatsFrameTooLongs: 0, Dot3StatsInternalMacReceiveErrors: 0, Dot3StatsSymbolErrors: 0},
				&datagram.CounterIfRecord{RecordHeader: datagram.SampleHeader{Format: 1, Length: 88}, IfIndex: 9, IfType: 6, IfSpeed: 100000000, IfDirection: 1, IfStatus: 3, IfInOctets: 79282473, IfInUcastPkts: 329128, IfInMulticastPkts: 0, IfInBroadcastPkts: 1493, IfInDiscards: 0, IfInErrors: 0, IfInUnknownProtos: 0, IfOutOctets: 764247430, IfOutUcastPkts: 9470970, IfOutMulticastPkts: 780342, IfOutBroadcastPkts: 877721, IfOutDiscards: 0, IfOutErrors: 0, IfPromiscuousMode: 1},
			},
		},
	},
}

func TestSflow_sample_2_record_1_2(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_2_record_1_2Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_2_record_1_2Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_2_record_1_2Decoded, s)
	}
}
