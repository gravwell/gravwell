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

//go:embed sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001.bin
var Sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001Bytes []byte

var Sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001Decoded = &datagram.Datagram{
	Version:        5,
	IPVersion:      1,
	AgentIP:        net.IP{208, 85, 240, 52},
	SubAgentID:     1,
	SequenceNumber: 6239418,
	Uptime:         3327524916,
	SamplesCount:   3,
	Samples: []datagram.Sample{
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 148},
			SequenceNum:     155556,
			SFlowDataSource: 1,
			SamplingRate:    512,
			SamplePool:      313452466,
			Drops:           0,
			Input:           1,
			Output:          4,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 84}, HeaderProtocol: 1, FrameLength: 70, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 22, 60, 197, 244, 254, 0, 15, 248, 20, 48, 0, 8, 0, 69, 0, 0, 52, 32, 157, 64, 0, 46, 6, 45, 17, 109, 175, 40, 137, 199, 58, 161, 163, 233, 68, 0, 80, 117, 36, 190, 213, 139, 76, 216, 221, 128, 16, 10, 140, 68, 71, 0, 0, 1, 1, 8, 10, 0, 61, 0, 253, 41, 222, 124, 2}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 592},
			SequenceNum:     187667,
			SFlowDataSource: 4,
			SamplingRate:    512,
			SamplePool:      266168292,
			Drops:           0,
			Input:           4,
			Output:          1,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 528}, HeaderProtocol: 1, FrameLength: 558, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 0, 12, 159, 240, 1, 0, 22, 60, 197, 244, 254, 8, 0, 69, 0, 2, 28, 235, 24, 64, 0, 64, 6, 58, 211, 199, 58, 161, 163, 14, 139, 155, 135, 0, 80, 103, 116, 141, 253, 179, 224, 108, 242, 14, 243, 128, 24, 0, 63, 220, 169, 0, 0, 1, 1, 8, 10, 41, 222, 126, 78, 0, 92, 137, 75, 72, 84, 84, 80, 47, 49, 46, 49, 32, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 13, 10, 68, 97, 116, 101, 58, 32, 87, 101, 100, 44, 32, 50, 51, 32, 74, 117, 108, 32, 50, 48, 49, 52, 32, 48, 54, 58, 48, 50, 58, 51, 54, 32, 71, 77, 84, 13, 10, 83, 101, 114, 118, 101, 114, 58, 32, 65, 112, 97, 99, 104, 101, 47, 50, 46, 52, 46, 57, 32, 40, 68, 101, 98, 105, 97, 110, 41, 13, 10, 67, 111, 110, 116, 101, 110, 116, 45, 76, 101, 110, 103, 116, 104, 58, 32, 51, 50, 56, 13, 10, 67, 111, 110, 116, 101, 110, 116, 45, 84, 121, 112, 101, 58, 32, 116, 101, 120, 116, 47, 104, 116, 109, 108, 59, 32, 99, 104, 97, 114, 115, 101, 116, 61, 105, 115, 111, 45, 56, 56, 53, 57, 45, 49, 13, 10, 13, 10, 60, 33, 68, 79, 67, 84, 89, 80, 69, 32, 72, 84, 77, 76, 32, 80, 85, 66, 76, 73, 67, 32, 34, 45, 47, 47, 73, 69, 84, 70, 47, 47, 68, 84, 68, 32, 72, 84, 77, 76, 32, 50, 46, 48, 47, 47, 69, 78, 34, 62, 10, 60, 104, 116, 109, 108, 62, 60, 104, 101, 97, 100, 62, 10, 60, 116, 105, 116, 108, 101, 62, 52, 48, 52, 32, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 116, 105, 116, 108, 101, 62, 10, 60, 47, 104, 101, 97, 100, 62, 60, 98, 111, 100, 121, 62, 10, 60, 104, 49, 62, 78, 111, 116, 32, 70, 111, 117, 110, 100, 60, 47, 104, 49, 62, 10, 60, 112, 62, 84, 104, 101, 32, 114, 101, 113, 117, 101, 115, 116, 101, 100, 32, 85, 82, 76, 32, 47, 100, 101, 98, 105, 97, 110, 47, 100, 105, 115, 116, 115, 47, 115, 105, 100, 47, 109, 97, 105, 110, 47, 98, 105, 110, 97, 114, 121, 45, 105, 51, 56, 54, 47, 80, 97, 99, 107, 97, 103, 101, 115, 46, 100, 105, 102, 102, 47, 73, 110, 100, 101, 120, 32, 119, 97, 115, 32, 110, 111, 116, 32, 102, 111, 117, 110, 100, 32, 111, 110, 32, 116, 104, 105, 115, 32, 115, 101, 114, 118, 101, 114, 46, 60, 47, 112, 62, 10, 60, 104, 114, 62, 10, 60, 97, 100, 100, 114, 101, 115, 115, 62, 65, 112, 97, 99, 104, 101, 47, 50, 46, 52, 46, 57, 32, 40, 68, 101, 98, 105, 97, 110, 41, 32, 83, 101, 114, 118, 101, 114, 32, 97, 116, 32, 108, 105, 113, 117}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
		&datagram.FlowSample{
			SampleHeader:    datagram.SampleHeader{Format: 1, Length: 592},
			SequenceNum:     2805089,
			SFlowDataSource: 3,
			SamplingRate:    512,
			SamplePool:      2394643133,
			Drops:           0,
			Input:           3,
			Output:          1,
			Records: []datagram.Record{
				&datagram.FlowSampledHeader{RecordHeader: datagram.SampleHeader{Format: 1, Length: 528}, HeaderProtocol: 1, FrameLength: 1518, Stripped: 4, HeaderBytes: datagram.XDRVariableLengthOpaque{0, 0, 12, 159, 240, 1, 0, 37, 144, 80, 125, 167, 8, 0, 69, 0, 5, 220, 222, 41, 64, 0, 64, 6, 168, 193, 199, 58, 161, 130, 54, 208, 14, 164, 160, 98, 1, 187, 27, 61, 98, 34, 128, 238, 104, 17, 128, 16, 0, 137, 15, 142, 0, 0, 1, 1, 8, 10, 62, 94, 181, 92, 43, 212, 144, 247, 223, 73, 247, 185, 240, 250, 247, 221, 237, 248, 230, 228, 106, 85, 93, 6, 95, 167, 182, 179, 232, 145, 55, 153, 123, 38, 126, 20, 5, 16, 98, 72, 134, 72, 2, 8, 211, 121, 75, 38, 242, 41, 134, 170, 143, 115, 224, 96, 3, 255, 102, 100, 179, 155, 32, 208, 28, 148, 39, 122, 240, 27, 137, 241, 133, 203, 206, 30, 192, 209, 81, 165, 176, 238, 175, 203, 203, 51, 253, 211, 46, 225, 117, 153, 118, 170, 202, 152, 159, 42, 168, 25, 245, 97, 74, 148, 109, 87, 250, 89, 26, 96, 10, 194, 192, 87, 2, 246, 40, 99, 63, 133, 110, 70, 37, 148, 212, 250, 137, 176, 226, 253, 52, 95, 187, 227, 243, 84, 70, 122, 35, 115, 18, 190, 224, 163, 233, 49, 99, 116, 95, 11, 6, 140, 21, 182, 117, 197, 90, 80, 57, 239, 97, 101, 10, 15, 43, 178, 198, 255, 101, 158, 181, 253, 100, 114, 58, 6, 3, 190, 130, 102, 162, 111, 225, 127, 100, 88, 53, 35, 134, 233, 215, 167, 100, 69, 202, 46, 225, 215, 178, 251, 255, 135, 97, 112, 63, 87, 218, 232, 13, 152, 81, 81, 222, 12, 125, 201, 199, 52, 44, 12, 119, 227, 191, 100, 248, 206, 224, 89, 81, 110, 160, 166, 174, 199, 252, 196, 224, 28, 55, 2, 91, 113, 66, 168, 120, 191, 207, 46, 242, 166, 159, 116, 194, 208, 243, 100, 100, 174, 246, 222, 181, 21, 57, 146, 170, 208, 241, 79, 4, 89, 90, 148, 249, 119, 245, 105, 211, 231, 138, 132, 2, 105, 78, 206, 252, 196, 170, 188, 96, 250, 6, 151, 73, 169, 157, 45, 22, 7, 63, 154, 47, 130, 221, 82, 220, 242, 29, 252, 232, 97, 48, 253, 107, 77, 91, 39, 231, 96, 97, 158, 103, 75, 97, 109, 176, 144, 206, 217, 76, 129, 35, 218, 6, 200, 5, 88, 188, 143, 54, 197, 183, 133, 25, 143, 63, 201, 249, 163, 22, 159, 92, 124, 95, 81, 109, 205, 57, 183, 133, 79, 82, 65, 128, 136, 86, 63, 188, 203, 159, 21, 79, 53, 230, 238, 124, 89, 190, 10, 84, 29, 72, 51, 26, 103, 196, 60, 104, 69, 15, 239, 12, 16, 191, 207, 29, 202, 159, 19, 30, 179, 60, 39, 1, 120, 233, 40, 125, 237, 237, 193, 209, 141, 149, 99, 45, 254, 61, 170, 14, 69, 30, 72, 111, 66, 130, 189, 108, 229, 198, 15, 40, 94, 33, 86, 152, 235, 6, 171, 21, 226, 62, 104, 189, 30, 123, 63, 55, 35, 105, 195, 5, 159, 246, 116}},
				&datagram.ExtendedSwitch{RecordHeader: datagram.SampleHeader{Format: 1001, Length: 16}, SrcVLAN: 16, SrcPriority: 0, DstVLAN: 16, DstPriority: 0},
			},
		},
	},
}

func TestSflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001(t *testing.T) {
	d := decoder.NewDatagramDecoder(bytes.NewReader(Sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001Bytes))
	s, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(s, Sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001Decoded) {
		t.Fatalf("Decoded datagram does not match expected value.\nExpected: %+v\nGot: %+v\n", Sflow_sample_1_record_1_1001_sample_1_record_1_1001_sample_1_record_1_1001Decoded, s)
	}
}
