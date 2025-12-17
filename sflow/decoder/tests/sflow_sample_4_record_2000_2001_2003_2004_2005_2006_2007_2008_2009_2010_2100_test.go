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

//go:embed sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100.bin
var Sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Bytes []byte

var Sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 14,
	Uptime:         405142,
	SamplesCount:   1,
	Samples: []datagram.Sample{
		&datagram.CounterSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 4, Length: 760},
			SequenceNum:             14,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 2, SourceIDIndex: 1},
			Records: []datagram.Record{
				&datagram.VirtNode{RecordHeader: datagram.SampleHeader{Format: 2100, Length: 28}, Mhz: 605, CPUs: 24, Memory: 58589396992, MemoryFree: 35347795968, NumDomains: 0},
				&datagram.HostAdapters{RecordHeader: datagram.SampleHeader{Format: 2001, Length: 68}, Adapters: []datagram.HostAdapter{datagram.HostAdapter{IFIndex: 6, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{54, 125, 216, 218, 183, 42, 0, 0}}}, datagram.HostAdapter{IFIndex: 5, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{6, 44, 209, 45, 22, 193, 0, 0}}}, datagram.HostAdapter{IFIndex: 2, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{24, 61, 45, 142, 71, 17, 0, 0}}}, datagram.HostAdapter{IFIndex: 4, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{72, 69, 230, 21, 11, 233, 0, 0}}}}},
				&datagram.MIB2UDPGroup{RecordHeader: datagram.SampleHeader{Format: 2010, Length: 28}, UDPInDatagrams: 35923, UDPNoPorts: 280, UDPInErrors: 0, UDPOutDatagrams: 41655, UDPRcvbufErrors: 0, UDPSndbufErrors: 0, UDPInCsumErrors: 0},
				&datagram.MIB2TCPGroup{RecordHeader: datagram.SampleHeader{Format: 2009, Length: 60}, TCPRtoAlgorithm: 1, TCPRtoMin: 200, TCPRtoMax: 120000, TCPMaxConn: 4294967295, TCPActiveOpens: 3225, TCPPassiveOpens: 661, TCPAttemptFails: 928, TCPEstabResets: 362, TCPCurrEstab: 16, TCPInSegs: 318327, TCPOutSegs: 302394, TCPRetransSegs: 2541, TCPInErrs: 1, TCPOutRsts: 1675, TCPInCsumErrors: 0},
				&datagram.MIB2ICMPGroup{RecordHeader: datagram.SampleHeader{Format: 2008, Length: 100}, ICMPInMsgs: 6315, ICMPInErrors: 1, ICMPInDestUnreachs: 0, ICMPInTimeExcds: 6314, ICMPInParamProbs: 0, ICMPInSrcQuenchs: 0, ICMPInRedirects: 0, ICMPInEchos: 0, ICMPInEchoReps: 0, ICMPInTimestamps: 1, ICMPInAddrMasks: 0, ICMPInAddrMaskReps: 0, ICMPOutMsgs: 0, ICMPOutErrors: 0, ICMPOutDestUnreachs: 281, ICMPOutTimeExcds: 0, ICMPOutParamProbs: 0, ICMPOutSrcQuenchs: 2, ICMPOutRedirects: 279, ICMPOutEchos: 0, ICMPOutEchoReps: 0, ICMPOutTimestamps: 0, ICMPOutTimestampReps: 0, ICMPOutAddrMasks: 2, ICMPOutAddrMaskReps: 0},
				&datagram.MIB2IPGroup{RecordHeader: datagram.SampleHeader{Format: 2007, Length: 76}, IPForwarding: 1, IPDefaultTTL: 64, IPInReceives: 388405, IPInHdrErrors: 0, IPInAddrErrors: 0, IPForwDatagrams: 89, IPInUnknownProtos: 0, IPInDiscards: 0, IPInDelivers: 360691, IPOutRequests: 304500, IPOutDiscards: 132, IPOutNoRoutes: 205, IPReasmTimeout: 2, IPReasmReqds: 32, IPReasmOKs: 15, IPReasmFails: 2, IPFragOKs: 7, IPFragFails: 0, IPFragCreates: 14},
				&datagram.HostDiskIO{RecordHeader: datagram.SampleHeader{Format: 2005, Length: 52}, DiskTotal: 1005904687104, DiskFree: 838225522688, MaxUsedPartitionPercent: 1666, Reads: 3617091, BytesRead: 32426641408, ReadTime: 440939, Writes: 2614138, BytesWritten: 20410566656, WriteTime: 11690725},
				&datagram.HostMemory{RecordHeader: datagram.SampleHeader{Format: 2004, Length: 72}, MemTotal: 58589396992, MemFree: 35347795968, MemShared: 0, MemBuffers: 3843522560, MemCached: 8420716544, SwapTotal: 5858914304, SwapFree: 5858914304, PageIn: 7922744, PageOut: 4983089, SwapIn: 0, SwapOut: 0},
				&datagram.HostCPU{RecordHeader: datagram.SampleHeader{Format: 2003, Length: 80}, LoadOne: 0.56, LoadFive: 0.51, LoadFifteen: 0.63, ProcessesRunning: 1, ProcessesTotal: 2371, CPUNume: 24, CPUSpeed: 605, Uptime: 14262, CPUUser: 7128840, CPUNice: 34440, CPUSys: 1973140, CPUIdle: 317495680, CPUWio: 153970, CPUIntr: 789180, CPUSoftIntr: 385500, Interrupts: 142007546, Contexts: 212361867, CPUSteal: 0, CPUGuest: 0, CPUGuestNice: 0},
				&datagram.HostNetIO{RecordHeader: datagram.SampleHeader{Format: 2006, Length: 40}, BytesIn: 5789512, PacketsIn: 7990, ErrorsIn: 0, DropsIn: 574, BytesOut: 2285988, PacketsOut: 4614, ErrorsOut: 0, DropsOut: 2},
				&datagram.HostDescr{RecordHeader: datagram.SampleHeader{Format: 2000, Length: 52}, HostName: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{116, 104, 105, 110, 107, 112, 97, 100, 80, 49, 52, 115}}, UUID: datagram.SFlowUUID{101, 113, 225, 155, 101, 113, 81, 155, 165, 113, 225, 155, 101, 113, 225, 155}, MachineType: 3, OSName: 2, OSRelease: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{54, 46, 49, 56, 46, 48}}},
			},
		},
	},
}

func TestSflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded, s)
	}
}
