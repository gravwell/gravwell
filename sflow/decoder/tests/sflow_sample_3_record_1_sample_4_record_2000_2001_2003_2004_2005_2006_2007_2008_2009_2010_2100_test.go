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

//go:embed sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100.bin
var Sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Bytes []byte

var Sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{10, 0, 0, 39},
	SubAgentID:     100000,
	SequenceNumber: 32,
	Uptime:         128970,
	SamplesCount:   2,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 196},
			SequenceNum:             5,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 17},
			SamplingRate:            1000,
			SamplePool:              5000,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 17},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1073741823},
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 169, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{200, 163, 98, 18, 192, 115, 184, 213, 38, 248, 69, 52, 8, 0, 69, 0, 0, 151, 0, 0, 64, 0, 58, 17, 124, 162, 172, 217, 12, 142, 10, 0, 0, 77, 1, 187, 168, 165, 0, 131, 73, 104, 69, 232, 227, 251, 8, 150, 187, 185, 225, 19, 99, 171, 11, 30, 81, 51, 43, 64, 181, 140, 148, 180, 109, 25, 198, 169, 27, 34, 38, 21, 53, 93, 230, 46, 91, 83, 245, 191, 233, 113, 173, 6, 157, 182, 82, 43, 8, 220, 176, 232, 188, 120, 214, 115, 65, 79, 184, 209, 235, 101, 159, 201, 154, 77, 22, 63, 22, 205, 218, 197, 64, 236, 247, 122, 50, 168, 225, 195, 32, 206, 164, 161, 19, 165, 218, 33}},
			},
		},
		&datagram.CounterSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 4, Length: 776},
			SequenceNum:             5,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 2, SourceIDIndex: 1},
			Records: []datagram.Record{
				&datagram.VirtNode{RecordHeader: datagram.SampleHeader{Format: 2100, Length: 28}, Mhz: 3570, CPUs: 24, Memory: 58589417472, MemoryFree: 11942858752, NumDomains: 4},
				&datagram.HostAdapters{RecordHeader: datagram.SampleHeader{Format: 2001, Length: 84}, Adapters: []datagram.HostAdapter{datagram.HostAdapter{IFIndex: 17, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{200, 163, 98, 18, 192, 115, 0, 0}}}, datagram.HostAdapter{IFIndex: 5, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{30, 189, 42, 106, 138, 230, 0, 16}}}, datagram.HostAdapter{IFIndex: 6, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{214, 11, 75, 216, 124, 243, 144, 0}}}, datagram.HostAdapter{IFIndex: 2, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{24, 61, 45, 142, 71, 17, 5, 33}}}, datagram.HostAdapter{IFIndex: 4, MACAddresses: []datagram.XDRMACAddress{datagram.XDRMACAddress{72, 69, 230, 21, 11, 233, 0, 0}}}}},
				&datagram.MIB2UDPGroup{RecordHeader: datagram.SampleHeader{Format: 2010, Length: 28}, UDPInDatagrams: 13958474, UDPNoPorts: 4395, UDPInErrors: 0, UDPOutDatagrams: 11551464, UDPRcvbufErrors: 0, UDPSndbufErrors: 6, UDPInCsumErrors: 0},
				&datagram.MIB2TCPGroup{RecordHeader: datagram.SampleHeader{Format: 2009, Length: 60}, TCPRtoAlgorithm: 1, TCPRtoMin: 200, TCPRtoMax: 120000, TCPMaxConn: 4294967295, TCPActiveOpens: 23200, TCPPassiveOpens: 7972, TCPAttemptFails: 5293, TCPEstabResets: 1024, TCPCurrEstab: 38, TCPInSegs: 2342631, TCPOutSegs: 1495402, TCPRetransSegs: 17028, TCPInErrs: 12, TCPOutRsts: 8998, TCPInCsumErrors: 0},
				&datagram.MIB2ICMPGroup{RecordHeader: datagram.SampleHeader{Format: 2008, Length: 100}, ICMPInMsgs: 37712, ICMPInErrors: 1, ICMPInDestUnreachs: 0, ICMPInTimeExcds: 37711, ICMPInParamProbs: 0, ICMPInSrcQuenchs: 0, ICMPInRedirects: 0, ICMPInEchos: 0, ICMPInEchoReps: 0, ICMPInTimestamps: 1, ICMPInAddrMasks: 0, ICMPInAddrMaskReps: 0, ICMPOutMsgs: 0, ICMPOutErrors: 0, ICMPOutDestUnreachs: 4704, ICMPOutTimeExcds: 0, ICMPOutParamProbs: 0, ICMPOutSrcQuenchs: 69, ICMPOutRedirects: 4702, ICMPOutEchos: 0, ICMPOutEchoReps: 0, ICMPOutTimestamps: 0, ICMPOutTimestampReps: 0, ICMPOutAddrMasks: 2, ICMPOutAddrMaskReps: 0},
				&datagram.MIB2IPGroup{RecordHeader: datagram.SampleHeader{Format: 2007, Length: 76}, IPForwarding: 1, IPDefaultTTL: 64, IPInReceives: 16546462, IPInHdrErrors: 0, IPInAddrErrors: 0, IPForwDatagrams: 1149, IPInUnknownProtos: 0, IPInDiscards: 0, IPInDelivers: 16343037, IPOutRequests: 13025372, IPOutDiscards: 1818, IPOutNoRoutes: 1962, IPReasmTimeout: 72, IPReasmReqds: 452, IPReasmOKs: 190, IPReasmFails: 72, IPFragOKs: 59, IPFragFails: 0, IPFragCreates: 118},
				&datagram.HostDiskIO{RecordHeader: datagram.SampleHeader{Format: 2005, Length: 52}, DiskTotal: 1005904687104, DiskFree: 840501231616, MaxUsedPartitionPercent: 2.304e-42, Reads: 3895965, BytesRead: 39174373376, ReadTime: 483683, Writes: 14504095, BytesWritten: 192722598912, WriteTime: 71549131},
				&datagram.HostMemory{RecordHeader: datagram.SampleHeader{Format: 2004, Length: 72}, MemTotal: 58589417472, MemFree: 11942858752, MemShared: 0, MemBuffers: 4388151296, MemCached: 16689614848, SwapTotal: 5858914304, SwapFree: 5858914304, PageIn: 9570155, PageOut: 47051475, SwapIn: 0, SwapOut: 0},
				&datagram.HostCPU{RecordHeader: datagram.SampleHeader{Format: 2003, Length: 80}, LoadOne: 0.6, LoadFive: 0.96, LoadFifteen: 1.23, ProcessesRunning: 0, ProcessesTotal: 3449, CPUNume: 24, CPUSpeed: 3570, Uptime: 268702, CPUUser: 67186260, CPUNice: 212880, CPUSys: 17687500, CPUIdle: 1648387660, CPUWio: 505940, CPUIntr: 4405820, CPUSoftIntr: 2303170, Interrupts: 1397178766, Contexts: 1869600048, CPUSteal: 0, CPUGuest: 0, CPUGuestNice: 0},
				&datagram.HostNetIO{RecordHeader: datagram.SampleHeader{Format: 2006, Length: 40}, BytesIn: 1010345, PacketsIn: 2838, ErrorsIn: 0, DropsIn: 184, BytesOut: 830063, PacketsOut: 2105, ErrorsOut: 0, DropsOut: 7},
				&datagram.HostDescr{RecordHeader: datagram.SampleHeader{Format: 2000, Length: 52}, HostName: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{116, 104, 105, 110, 107, 112, 97, 100, 80, 49, 52, 115}}, UUID: datagram.SFlowUUID{101, 113, 225, 155, 101, 113, 81, 155, 165, 113, 225, 155, 101, 113, 225, 155}, MachineType: 3, OSName: 2, OSRelease: datagram.XDRString{XDRVariableLengthOpaque: datagram.XDRVariableLengthOpaque{54, 46, 49, 56, 46, 48}}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_sample_4_record_2000_2001_2003_2004_2005_2006_2007_2008_2009_2010_2100Decoded, s)
	}
}
