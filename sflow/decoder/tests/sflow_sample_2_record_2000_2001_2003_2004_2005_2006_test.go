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

//go:embed sflow_sample_2_record_2000_2001_2003_2004_2005_2006.bin
var Sflow_sample_2_record_2000_2001_2003_2004_2005_2006Bytes []byte

var Sflow_sample_2_record_2000_2001_2003_2004_2005_2006Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{192, 168, 1, 7},
	SubAgentID:     100000,
	SequenceNumber: 23,
	Uptime:         23000,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.CounterSample{
			SampleHeader:    datagram.SampleHeader{Format: 2, Length: 388},
			SequenceNum:     23,
			SFlowDataSource: 33554433,
			Records: []datagram.Record{
				&datagram.HostAdapters{RecordHeader: datagram.SampleHeader{Format: 2001, Length: 36}, Adapters: []datagram.HostAdapter{datagram.HostAdapter{IFIndex: 2, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{60, 151, 14, 37, 240, 86, 0, 0}}}, datagram.HostAdapter{IFIndex: 3, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{156, 78, 54, 89, 178, 84, 0, 0}}}}},
				&datagram.HostDiskIO{RecordHeader: datagram.SampleHeader{Format: 2005, Length: 52}, DiskTotal: 488129277952, DiskFree: 450023897088, MaxUsedPartitionPercent: 5639, Reads: 249289, BytesRead: 4761008128, ReadTime: 6064940, Writes: 3572946, BytesWritten: 23503597568, WriteTime: 75425312},
				&datagram.HostMemory{RecordHeader: datagram.SampleHeader{Format: 2004, Length: 72}, MemTotal: 3855892480, MemFree: 575180800, MemShared: 0, MemBuffers: 201179136, MemCached: 1707036672, SwapTotal: 4001361920, SwapFree: 4001337344, PageIn: 1190510, PageOut: 5752462, SwapIn: 0, SwapOut: 6},
				&datagram.HostCPU{RecordHeader: datagram.SampleHeader{Format: 2003, Length: 68}, LoadOne: 0.47, LoadFive: 0.58, LoadFifteen: 0.5, ProcessesRunning: 2, ProcessesTotal: 511, CPUNume: 4, CPUSpeed: 1200, Uptime: 103412, CPUUser: 5240030, CPUNice: 67660, CPUSys: 2148900, CPUIdle: 145795710, CPUWio: 4661420, CPUIntr: 58860, CPUSoftIntr: 0, Interrupts: 19770787, Contexts: 87991375, CPUSteal: 0, CPUGuest: 0, CPUGuestNice: 0},
				&datagram.HostNetIO{RecordHeader: datagram.SampleHeader{Format: 2006, Length: 40}, BytesIn: 14867, PacketsIn: 72, ErrorsIn: 0, DropsIn: 0, BytesOut: 2880, PacketsOut: 32, ErrorsOut: 0, DropsOut: 0},
				&datagram.HostDescr{RecordHeader: datagram.SampleHeader{Format: 2000, Length: 60}, HostName: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{102, 114, 97, 99, 116, 97, 108}}, UUID: datagram.SFlowUUID{32, 209, 29, 1, 81, 80, 17, 203, 149, 125, 153, 5, 33, 54, 91, 163}, MachineType: 3, OSName: 2, OSRelease: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{51, 46, 49, 51, 46, 48, 45, 50, 57, 45, 103, 101, 110, 101, 114, 105, 99}}},
			},
		},
	},
}

func TestSflow_sample_2_record_2000_2001_2003_2004_2005_2006(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_2_record_2000_2001_2003_2004_2005_2006Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_2_record_2000_2001_2003_2004_2005_2006Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_2_record_2000_2001_2003_2004_2005_2006Decoded, s)
	}
}
