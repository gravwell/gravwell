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

//go:embed sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001.bin
var Sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001Bytes []byte

var Sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{1, 2, 3, 4},
	SubAgentID:     0,
	SequenceNumber: 262632130,
	Uptime:         259421000,
	SamplesCount:   5,
	Samples: []datagram.Sample{
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 220},
			SequenceNum:             546345766,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1000100},
			SamplingRate:            16383,
			SamplePool:              70839514,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1000100},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1000018},
			Records: []datagram.Record{
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 30, SrcPriority: 0, DstVLAN: 30, DstPriority: 0},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1514, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{8, 236, 245, 42, 143, 190, 116, 131, 239, 48, 101, 183, 129, 0, 0, 30, 8, 0, 69, 0, 5, 212, 59, 186, 64, 0, 63, 6, 189, 153, 185, 59, 220, 147, 88, 238, 78, 19, 1, 187, 207, 214, 69, 183, 27, 192, 213, 184, 255, 36, 128, 16, 0, 4, 1, 85, 0, 0, 1, 1, 8, 10, 200, 200, 86, 149, 0, 52, 246, 15, 232, 29, 189, 65, 69, 146, 76, 194, 113, 224, 235, 46, 53, 23, 124, 47, 185, 168, 5, 146, 14, 3, 27, 80, 83, 12, 229, 125, 134, 117, 50, 138, 204, 226, 38, 168, 144, 33, 120, 191, 206, 122, 248, 181, 141, 72, 228, 170, 254, 38, 52, 224, 173, 185, 236, 121, 116, 216}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 220},
			SequenceNum:             546345767,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1000100},
			SamplingRate:            16383,
			SamplePool:              70855897,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1000100},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1000011},
			Records: []datagram.Record{
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 23, SrcPriority: 0, DstVLAN: 23, DstPriority: 0},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1482, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{218, 177, 34, 251, 217, 207, 116, 131, 239, 48, 101, 183, 129, 0, 0, 23, 8, 0, 69, 0, 5, 180, 226, 40, 64, 0, 63, 6, 21, 15, 195, 181, 175, 38, 5, 146, 198, 158, 0, 80, 15, 179, 53, 142, 54, 2, 161, 1, 237, 176, 128, 16, 0, 59, 247, 212, 0, 0, 1, 1, 8, 10, 210, 232, 172, 190, 0, 54, 188, 60, 55, 54, 196, 128, 63, 102, 51, 197, 80, 166, 99, 178, 146, 195, 106, 122, 128, 101, 11, 34, 98, 254, 22, 156, 171, 85, 3, 71, 166, 84, 99, 165, 188, 23, 142, 90, 246, 188, 36, 82, 233, 210, 123, 8, 232, 194, 107, 5, 28, 192, 97, 180, 224, 67, 89, 98, 191, 10}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 220},
			SequenceNum:             68329573,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1000104},
			SamplingRate:            16383,
			SamplePool:              2751897499,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1000104},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1000100},
			Records: []datagram.Record{
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 1337, SrcPriority: 0, DstVLAN: 1337, DstPriority: 0},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1522, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{116, 131, 239, 48, 101, 183, 40, 153, 58, 78, 137, 39, 129, 0, 5, 57, 8, 0, 69, 24, 5, 220, 142, 92, 64, 0, 58, 6, 83, 119, 137, 74, 204, 213, 89, 187, 169, 85, 7, 143, 173, 220, 242, 155, 9, 180, 206, 29, 188, 238, 128, 16, 117, 64, 88, 2, 0, 0, 1, 1, 8, 10, 176, 24, 91, 111, 215, 214, 139, 71, 238, 106, 3, 11, 155, 82, 177, 202, 97, 75, 132, 87, 117, 196, 178, 24, 17, 57, 206, 93, 42, 56, 145, 41, 118, 17, 125, 193, 204, 92, 75, 10, 222, 187, 168, 173, 157, 136, 54, 139, 192, 2, 135, 167, 165, 28, 217, 133, 113, 133, 104, 43, 89, 198, 44, 60, 132, 12}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 220},
			SequenceNum:             546345768,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1000100},
			SamplingRate:            16383,
			SamplePool:              70872280,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1000100},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1000011},
			Records: []datagram.Record{
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 23, SrcPriority: 0, DstVLAN: 23, DstPriority: 0},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1522, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{218, 177, 34, 251, 217, 207, 116, 131, 239, 48, 101, 183, 129, 0, 0, 23, 8, 0, 69, 0, 5, 220, 126, 66, 64, 0, 63, 6, 18, 77, 185, 102, 219, 67, 103, 194, 169, 32, 99, 117, 87, 174, 109, 191, 89, 124, 147, 113, 9, 103, 128, 16, 0, 235, 252, 22, 0, 0, 1, 1, 8, 10, 64, 150, 136, 56, 54, 225, 100, 199, 27, 67, 188, 14, 31, 129, 109, 57, 246, 18, 12, 234, 192, 234, 123, 193, 119, 226, 146, 106, 191, 190, 132, 217, 0, 24, 87, 73, 146, 114, 143, 163, 120, 69, 111, 198, 152, 143, 113, 176, 197, 82, 125, 138, 130, 239, 82, 219, 233, 220, 10, 82, 219, 6, 81, 128, 128, 169}},
			},
		},
		&datagram.FlowSampleExpanded{
			SampleHeader:            datagram.SampleHeader{Format: 3, Length: 220},
			SequenceNum:             546345769,
			SFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: 0, SourceIDIndex: 1000100},
			SamplingRate:            16383,
			SamplePool:              70888663,
			Drops:                   0,
			Input:                   datagram.InterfaceExpanded{Format: 0, Value: 1000100},
			Output:                  datagram.InterfaceExpanded{Format: 0, Value: 1000101},
			Records: []datagram.Record{
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 957, SrcPriority: 0, DstVLAN: 957, DstPriority: 0},
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 144}, HeaderProtocol: 1, FrameLength: 1522, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{144, 226, 186, 137, 33, 173, 116, 131, 239, 48, 101, 183, 129, 0, 3, 189, 8, 0, 69, 0, 5, 220, 118, 162, 64, 0, 56, 6, 172, 117, 51, 91, 116, 108, 195, 181, 174, 135, 31, 64, 128, 104, 171, 187, 47, 144, 1, 238, 58, 175, 128, 16, 0, 235, 142, 244, 0, 0, 1, 1, 8, 10, 52, 192, 255, 38, 172, 144, 213, 196, 204, 215, 164, 165, 91, 163, 121, 51, 193, 37, 205, 132, 220, 170, 55, 201, 227, 171, 198, 180, 235, 227, 141, 114, 6, 209, 90, 31, 154, 139, 233, 154, 247, 51, 53, 229, 202, 103, 186, 4, 249, 60, 39, 255, 163, 202, 94, 144, 249, 199, 209, 228, 248, 245, 122, 20, 220, 28}},
			},
		},
	},
}

func TestSflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001_sample_3_record_1_1001Decoded, s)
	}
}
