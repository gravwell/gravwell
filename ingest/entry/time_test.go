/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package entry_test

import (
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/entry"
)

func TestTimestamp_IsZero(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		ts := entry.Timestamp{}
		if !ts.IsZero() {
			t.Fatal("new timestamp is not zero")
		}
	})
	t.Run("add resulting in zero", func(t *testing.T) {
		ts := entry.Timestamp{Sec: 100}.Add(-100 * time.Second)
		if !ts.IsZero() {
			t.Fatalf("ts (%+v) is not considered zero.", ts)
		}
	})
	t.Run("from standard", func(t *testing.T) {
		ts := entry.FromStandard(time.Time{})
		if !ts.IsZero() {
			t.Fatalf("ts (%+v) is not considered zero.", ts)
		}
	})
}

func TestTimestamp_StandardTime(t *testing.T) {
	now := time.Now()
	// entry.Timestamp operates in nanosecond precision
	now = now.Round(time.Nanosecond)

	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		ts   entry.Timestamp
		want time.Time
	}{
		{"zero", entry.Timestamp{}, time.Time{}},
		{"unix", entry.UnixTime(0, 0), time.Unix(0, 0)},
		{"from -> to", entry.FromStandard(now), now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ts.StandardTime()
			if got.Equal(tt.want) {
				t.Errorf("StandardTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromStandard(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		tm   time.Time
		want entry.Timestamp
	}{
		{"zero", time.Time{}, entry.Timestamp{}},
		{"unix", time.Unix(0, 0), entry.UnixTime(0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entry.FromStandard(tt.tm)
			if got != tt.want {
				t.Errorf("StandardTime() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("duration equality across multiple calls", func(t *testing.T) {
		tmBD := time.Date(1997, 10, 05, 10, 04, 02, 00, time.UTC)
		tsBD := entry.FromStandard(tmBD)

		tmNow := time.Now().Round(time.Nanosecond)
		tsNow := entry.FromStandard(tmNow)

		tmSince := tmBD.Sub(tmNow)
		tsSince := tsBD.Sub(tsNow)
		if tmSince != tsSince {
			t.Errorf("since mismatch! tm: %v | ts: %v", tmSince, tsSince)
		}
	})

}
