/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package xdr

import "testing"

func TestCalculatePad(t *testing.T) {
	tests := []struct {
		length   uint32
		expected uint32
	}{
		{0, 0},  // 0 is already aligned
		{1, 3},  // 1 + 3 = 4
		{2, 2},  // 2 + 2 = 4
		{3, 1},  // 3 + 1 = 4
		{4, 0},  // 4 is already aligned
		{5, 3},  // 5 + 3 = 8
		{6, 2},  // 6 + 2 = 8
		{7, 1},  // 7 + 1 = 8
		{8, 0},  // 8 is already aligned
		{17, 3}, // 17 + 3 = 20 (e.g., "determined_panini")
	}

	for _, tc := range tests {
		got := CalculatePad(tc.length)
		if got != tc.expected {
			t.Errorf("CalculatePad(%d) = %d, want %d", tc.length, got, tc.expected)
		}
	}
}

func TestPaddedLength(t *testing.T) {
	tests := []struct {
		length   uint32
		expected uint32
	}{
		{0, 0},
		{1, 4},
		{2, 4},
		{3, 4},
		{4, 4},
		{5, 8},
		{6, 8},
		{7, 8},
		{8, 8},
		{17, 20}, // "determined_panini" -> 20 bytes with padding
	}

	for _, tc := range tests {
		got := PaddedLength(tc.length)
		if got != tc.expected {
			t.Errorf("PaddedLength(%d) = %d, want %d", tc.length, got, tc.expected)
		}
	}
}
