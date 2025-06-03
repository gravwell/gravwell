/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

// Figured history requires its own test suite because it has a lot of edge cases.

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestHistoryLimits(t *testing.T) {
	t.Run("unset clash", func(t *testing.T) {
		// overlap between unset macro will cause indexing issues
		if _ARRAY_END >= unset {
			t.Errorf("unset macro (%d) is in the set of array indices (%v)", unset, _ARRAY_END)
		}
	})
}

// A somewhat redundant test to ensure new sets all parameters correctly
func TestNewHistory(t *testing.T) {
	t.Run("New", func(t *testing.T) {
		h := newHistory()
		if h.fetchedIndex != unset {
			t.Errorf("fetch index did not start unset")
		}
		if h.insertionIndex != 0 {
			t.Errorf("insertion index is not at 0th index")
		}
		for i, c := range h.commands {
			if c != "" {
				t.Errorf("non empty (%v) @ index %v", c, i)
			}
		}
	})
}

func Test_history_Insert(t *testing.T) {
	t.Parallel()
	t.Run("first record", func(t *testing.T) {
		h := newHistory()
		record := "first"
		h.insert(record)
		if r := h.commands[0]; r != record {
			t.Errorf("record mismatch: expected %s, got %s", record, h.commands[0])
		}
		if h.insertionIndex != 1 {
			t.Errorf("insertion index not incremeneted")
		}
		if h.fetchedIndex != unset {
			t.Errorf("fetch index altered during insert")
		}
	})
	t.Run("empty second record", func(t *testing.T) {
		h := newHistory()
		h.insert("first")
		record := ""
		h.insert(record)
		if h.insertionIndex != 1 {
			t.Errorf("insertion index was incremeneted")
		}
		if h.fetchedIndex != unset {
			t.Errorf("fetch index altered during insert")
		}
	})
	t.Run("interspersed empty records", func(t *testing.T) {
		// no number of empty insertions should alter history at all
		h := newHistory()
		h.insert("first")
		h.insert("")
		h.insert("second")
		insertCount := rand.Intn(500)
		for i := 0; i < insertCount; i++ {
			h.insert("")
		}
		h.insert("third")
		insertCount = rand.Intn(500)
		for i := 0; i < insertCount; i++ {
			h.insert("")
		}
		h.insert("fourth")

		if h.insertionIndex != 4 {
			t.Errorf("insertion index mismatch: expected %v, got %v", 4, h.insertionIndex)
		}
		if h.fetchedIndex != unset {
			t.Errorf("fetch index altered during insert")
		}
	})
}

func Test_history_GetRecord(t *testing.T) {
	t.Parallel()
	t.Run("empty fetches", func(t *testing.T) {
		h := newHistory()
		// fetchIndex should be altered even if the newest record is empty
		t.Run("first fetch sets fetchedIndex", func(t *testing.T) {
			r := h.getOlderRecord()
			if r != "" {
				t.Errorf("empty history did not fetch empty string (got: %s)", r)
			}

			// fetching immediately on creation should wrap fetchIndex to arrayEnd,
			//	as history does not know if we just started or overflowed
			if h.fetchedIndex != _ARRAY_END {
				t.Errorf("empty history did not unflow on decrement (fetchIndex: %d)", h.fetchedIndex)
			}
		})
		// fetchIndex should not be altered on the second empty record
		t.Run("second fetch does not alter fetchedIndex", func(t *testing.T) {
			r := h.getOlderRecord()
			if r != "" {
				t.Errorf("empty history did not fetch empty string (got: %s)", r)
			}
			if h.fetchedIndex != _ARRAY_END {
				t.Errorf("second decrement occured despite empty history (fetchIndex: %d, expected: %d)", h.fetchedIndex, _ARRAY_END)
			}
		})
		t.Run("unset, repeat first fetch sets fetchedIndex", func(t *testing.T) {
			h.unsetFetch()
			r := h.getOlderRecord()
			if r != "" {
				t.Errorf("empty history did not fetch empty string (got: %s)", r)
			}

			// fetching immediately on creation should wrap fetchIndex to arrayEnd,
			//	as history does not know if we just started or overflowed
			if h.fetchedIndex != _ARRAY_END {
				t.Errorf("empty history did not unflow on decrement (fetchIndex: %d)", h.fetchedIndex)
			}
		})
	})
	t.Run("arbitrary Older/Newer manipulation", func(t *testing.T) {
		h := newHistory()
		h.insert("A")
		h.insert("B")
		h.insert("C")
		t.Run("GetOlderRecord returns C, B, A (in that order)", func(t *testing.T) {
			if r := h.getOlderRecord(); r != "C" {
				t.Fatalf("expected: %v, got: %v", "C", r)
			}
			if r := h.getOlderRecord(); r != "B" {
				t.Fatalf("expected: %v, got: %v", "B", r)
			}
			if r := h.getOlderRecord(); r != "A" {
				t.Fatalf("expected: %v, got: %v", "A", r)
			}
		})

		randOlderRecordFunc := func(t *testing.T) {
			roll := rand.Intn(int(_ARRAY_SIZE) * 2)
			t.Logf("Rolled %d", roll)
			for i := 0; i < roll; i++ {
				if r := h.getOlderRecord(); r != "" {
					t.Fatalf("iteration #%d: found non-empty value %s", i, r)
				}
			}
		}
		t.Run("any number of GetOlderRecords returns empty record #1", randOlderRecordFunc)
		t.Run("any number of GetOlderRecords returns empty record #2", randOlderRecordFunc)
		t.Run("any number of GetOlderRecords returns empty record #3", randOlderRecordFunc)

		t.Run("GetNewerRecord returns A, B, C (in that order)", func(t *testing.T) {
			if r := h.getNewerRecord(); r != "A" {
				t.Fatalf("expected: %v, got: %v", "A", r)
			}
			if r := h.getNewerRecord(); r != "B" {
				t.Fatalf("expected: %v, got: %v", "B", r)
			}
			if r := h.getNewerRecord(); r != "C" {
				t.Fatalf("expected: %v, got: %v", "C", r)
			}
		})

		t.Run("GetOlderRecord returns B", func(t *testing.T) {
			if r := h.getOlderRecord(); r != "B" {
				t.Fatalf("expected: %v, got: %v", "B", r)
			}
		})
		h.getNewerRecord() // C

		randNewerRecordFunc := func(t *testing.T) {
			roll := rand.Intn(int(_ARRAY_SIZE) * 2)
			t.Logf("Rolled %d", roll)
			for i := 0; i < roll; i++ {
				if r := h.getNewerRecord(); r != "" {
					t.Fatalf("iteration #%d: found non-empty value %s", i, r)
				}
			}
		}
		t.Run("any number of GetNewerRecords returns empty record #1", randNewerRecordFunc)
		t.Run("any number of GetNewerRecords returns empty record #2", randNewerRecordFunc)
		t.Run("any number of GetNewerRecords returns empty record #3", randNewerRecordFunc)

		t.Run("GetOlderRecord returns C, B, A (in that order)", func(t *testing.T) {
			if r := h.getOlderRecord(); r != "C" {
				t.Fatalf("expected: %v, got: %v", "C", r)
			}
			if r := h.getOlderRecord(); r != "B" {
				t.Fatalf("expected: %v, got: %v", "B", r)
			}
			if r := h.getOlderRecord(); r != "A" {
				t.Fatalf("expected: %v, got: %v", "A", r)
			}
		})

	})
	t.Run("at limit", func(t *testing.T) {
		h := newHistory()
		var i uint16
		for i = 0; i < _ARRAY_SIZE; i++ {
			h.insert(fmt.Sprintf("%v", i))
		}
		t.Run("first GetRecord", func(t *testing.T) {
			if r := h.getOlderRecord(); r != fmt.Sprintf("%v", _ARRAY_END) ||
				r != h.commands[_ARRAY_END] {
				t.Errorf("GetRecord did not return last record. Expected %s, got %s. Commands: %v",
					fmt.Sprintf("%v", _ARRAY_END), r, h.commands)
			}
		})
		t.Run("second GetRecord", func(t *testing.T) {
			want := fmt.Sprintf("%v", _ARRAY_END-1)
			if r := h.getOlderRecord(); r != want || r != h.commands[_ARRAY_END-1] {
				t.Errorf("GetRecord did not return second-to-last record. Expected %s, got %s. Commands: %v",
					want, r, h.commands)
			}
		})
		t.Run("edge of underflow", func(t *testing.T) {
			want := fmt.Sprintf("%v", 0)
			for i := _ARRAY_END - 1; i > 1; i-- {
				_ = h.getOlderRecord()
			}
			r := h.getOlderRecord()
			if h.fetchedIndex != 0 {
				t.Fatalf("fetch index error. r: %s, h: %+v", r, h)
			}
			if r != want {
				t.Errorf("GetRecord did not return oldest (first) record. Expected %s, got %s. Commands: %v",
					want, r, h.commands)
			}
		})
		t.Run("underflow", func(t *testing.T) {
			want := fmt.Sprintf("%v", 999)
			r := h.getOlderRecord()
			if h.fetchedIndex != 999 {
				t.Fatalf("fetch index error. r: %s, h: %+v", r, h)
			}
			if r != want {
				t.Errorf("GetRecord did not return oldest (first) record. Expected %s, got %s. Commands: %v",
					want, r, h.commands)
			}
		})
	})
}

func Test_history_GetAllRecords(t *testing.T) {
	t.Run("Clipped", func(t *testing.T) {
		cap := 50
		h := newHistory()
		want := make([]string, cap)
		for i := 0; i < cap; i++ {
			h.insert("command")
			want[i] = "command"
		}
		rs := h.getAllRecords()
		if len(rs) != cap {
			t.Errorf("GetAllRecords did not clip return. Expected %v (len %d). Got %v (len %d).",
				want, len(want), rs, len(rs))
		}
	})

	h := newHistory()
	t.Run("no overflow", func(t *testing.T) {
		for i := int(_ARRAY_END); i >= 0; i-- {
			h.insert(fmt.Sprintf("%d", -i))
		}
		rs := h.getAllRecords()
		for i := 0; i < int(_ARRAY_END); i++ {
			if rs[i] != fmt.Sprintf("%d", -i) {
				t.Fatalf("value mismatch: (index: %d) (want: %s, got (rs[i]): %s)", i, fmt.Sprintf("%d", -i), rs[i])
			}
		}
	})
	t.Run("single overflow", func(t *testing.T) {
		h.insert("A")
		rs := h.getAllRecords()
		if rs[0] != "A" || rs[1] != "0" {
			t.Errorf("GetAllRecords did not sort newest first."+
				"Expected first record 'A' (got: %v), second record '0' (got: %v)", rs[0], 0)
		}
		if h.commands[0] != "A" || h.commands[1] != fmt.Sprintf("-%d", _ARRAY_END-1) {
			t.Errorf("Command list corrupt on overflow."+
				"Expected [A, -998, -997, ...]. Got: [%s, %s, %s, ...]",
				h.commands[0], h.commands[1], h.commands[2])
		}
	})

}
