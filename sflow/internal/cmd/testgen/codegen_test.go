/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"testing"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
)

func TestBuildBaseName(t *testing.T) {
	tests := []struct {
		name     string
		dgram    *datagram.Datagram
		expected string
	}{
		{
			name:     "empty datagram",
			dgram:    &datagram.Datagram{},
			expected: "sflow",
		},
		{
			name: "single flow sample with one record",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 1}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_1",
		},
		{
			name: "single flow sample with multiple records sorted",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 1}},
							&datagram.ExtendedTCPInfo{RecordHeader: datagram.RecordHeader{Format: 2007}},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 3}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_1_3_2007",
		},
		{
			name: "multiple samples with records",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 1}},
						},
					},
					&datagram.CounterSample{
						SampleHeader: datagram.SampleHeader{Format: 2},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 5}},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 6}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_1_sample_2_record_5_6",
		},
		{
			name: "sample with unknown record",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 1}},
							&datagram.UnknownRecord{Format: 9999},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_1_unknown_record_9999",
		},
		{
			name: "sample with multiple unknown records sorted",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.UnknownRecord{Format: 9999},
							&datagram.UnknownRecord{Format: 8888},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 1}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_1_unknown_record_8888_9999",
		},
		{
			name: "sample with only unknown records",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.UnknownRecord{Format: 9999},
						},
					},
				},
			},
			expected: "sflow_sample_1_unknown_record_9999",
		},
		{
			name: "unknown sample only",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.UnknownSample{Format: 66},
				},
			},
			expected: "sflow_unknown_sample_66",
		},
		{
			name: "multiple unknown samples sorted",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.UnknownSample{Format: 66},
					&datagram.UnknownSample{Format: 45},
				},
			},
			expected: "sflow_unknown_sample_45_66",
		},
		{
			name: "mixed known and unknown samples",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.UnknownSample{Format: 66},
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 3}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_3_unknown_sample_66",
		},
		{
			name: "complex: multiple samples with known records, unknown records, and unknown samples sorted",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 3}},
							&datagram.UnknownRecord{Format: 89},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 4}},
						},
					},
					&datagram.CounterSample{
						SampleHeader: datagram.SampleHeader{Format: 5},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 3}},
							&datagram.UnknownRecord{Format: 89},
						},
					},
					&datagram.UnknownSample{Format: 66},
					&datagram.UnknownSample{Format: 45},
				},
			},
			expected: "sflow_sample_1_record_3_4_unknown_record_89_sample_5_record_3_unknown_record_89_unknown_sample_45_66",
		},
		{
			name: "sample with no records",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records:      []datagram.Record{},
					},
				},
			},
			expected: "sflow_sample_1",
		},
		{
			name: "sort samples to guarantee uniqueness",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.CounterSample{
						SampleHeader: datagram.SampleHeader{Format: 2},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 5}},
						},
					},
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 3}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_3_sample_2_record_5",
		},
		{
			name: "sort records to guarantee uniqueness",
			dgram: &datagram.Datagram{
				Samples: []datagram.Sample{
					&datagram.FlowSample{
						SampleHeader: datagram.SampleHeader{Format: 1},
						Records: []datagram.Record{
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 9}},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 2}},
							&datagram.FlowSampledHeader{RecordHeader: datagram.RecordHeader{Format: 5}},
						},
					},
				},
			},
			expected: "sflow_sample_1_record_2_5_9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBaseName(tt.dgram)
			if got != tt.expected {
				t.Errorf("buildBaseName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple hex literal",
			input:    "package main\n\nvar x = 0x10\n",
			expected: "package main\n\nvar x = 16\n",
		},
		{
			name:     "multiple hex literals",
			input:    "package main\n\nvar x = 0x10\nvar y = 0xFF\n",
			expected: "package main\n\nvar x = 16\nvar y = 255\n",
		},
		{
			name:     "uppercase hex prefix",
			input:    "package main\n\nvar X = 0X10\n",
			expected: "package main\n\nvar X = 16\n",
		},
		{
			name:     "mixed hex and decimal",
			input:    "package main\n\nvar x = 0x10\nvar y = 42\n",
			expected: "package main\n\nvar x = 16\nvar y = 42\n",
		},
		{
			name:     "hex in struct literal",
			input:    "package main\n\ntype S struct{ A int }\n\nvar s = S{A: 0x20}\n",
			expected: "package main\n\ntype S struct{ A int }\n\nvar s = S{A: 32}\n",
		},
		{
			name:     "zero value hex",
			input:    "package main\n\nvar x = 0x0\n",
			expected: "package main\n\nvar x = 0\n",
		},
		{
			name:     "large hex value",
			input:    "package main\n\nvar x = 0xFFFFFFFF\n",
			expected: "package main\n\nvar x = 4294967295\n",
		},
		{
			name:     "hex in slice",
			input:    "package main\n\nvar s = []int{0x1, 0x2, 0x3}\n",
			expected: "package main\n\nvar s = []int{1, 2, 3}\n",
		},
		{
			name:     "no hex literals",
			input:    "package main\n\nvar x = 123\n",
			expected: "package main\n\nvar x = 123\n",
		},
		{
			name:     "hex byte values",
			input:    "package main\n\nvar b = []byte{0x00, 0x01, 0x02, 0xff}\n",
			expected: "package main\n\nvar b = []byte{0, 1, 2, 255}\n",
		},
		{
			name:     "hex in line comment preserved",
			input:    "package main\n\n// 0xFF is a hex value\nvar x = 0x10\n",
			expected: "package main\n\n// 0xFF is a hex value\nvar x = 16\n",
		},
		{
			name:     "hex in block comment preserved",
			input:    "package main\n\n/* 0xFF is hex */\nvar x = 0x10\n",
			expected: "package main\n\n/* 0xFF is hex */\nvar x = 16\n",
		},
	}

	for _, tt := range tests {
		got, err := formatCode([]byte(tt.input))
		if err != nil {
			t.Fatalf("formatCode() unexpected error = %v", err)
		}
		if string(got) != tt.expected {
			t.Errorf("formatCode() =\n%s\nwant:\n%s", got, tt.expected)
		}
	}

	t.Run("invalid go code returns error", func(t *testing.T) {
		_, err := formatCode([]byte("this is not valid go code {{{"))
		if err == nil {
			t.Error("formatCode() expected error for invalid Go code, got nil")
		}
	})

	t.Run("incomplete struct returns error", func(t *testing.T) {
		_, err := formatCode([]byte("package main\n\nvar x = S{A: 0x10"))
		if err == nil {
			t.Error("formatCode() expected error for incomplete struct, got nil")
		}
	})
}
