//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"os"
	"testing"
)

func TestSamplePSI(t *testing.T) {
	if _, err := os.Stat(psiCPUPath); err != nil {
		t.Skipf("PSI not available on this kernel (%v), skipping", err)
	}

	stats, err := SamplePSI()
	if err != nil {
		t.Fatalf("SamplePSI returned unexpected error: %v", err)
	}

	// CPU exposes only a "some" line, so all Full fields must be zero.
	if stats.CPU.FullAvg10 != 0 {
		t.Errorf("CPU.FullAvg10 = %v, want 0", stats.CPU.FullAvg10)
	}
	if stats.CPU.FullAvg60 != 0 {
		t.Errorf("CPU.FullAvg60 = %v, want 0", stats.CPU.FullAvg60)
	}
	if stats.CPU.FullAvg300 != 0 {
		t.Errorf("CPU.FullAvg300 = %v, want 0", stats.CPU.FullAvg300)
	}

	// Averages must be non-negative.
	for _, tc := range []struct {
		name string
		v    float64
	}{
		{"CPU.SomeAvg10", stats.CPU.SomeAvg10},
		{"CPU.SomeAvg60", stats.CPU.SomeAvg60},
		{"CPU.SomeAvg300", stats.CPU.SomeAvg300},
		{"Memory.SomeAvg10", stats.Memory.SomeAvg10},
		{"Memory.SomeAvg60", stats.Memory.SomeAvg60},
		{"Memory.SomeAvg300", stats.Memory.SomeAvg300},
		{"Memory.FullAvg10", stats.Memory.FullAvg10},
		{"Memory.FullAvg60", stats.Memory.FullAvg60},
		{"Memory.FullAvg300", stats.Memory.FullAvg300},
		{"IO.SomeAvg10", stats.IO.SomeAvg10},
		{"IO.SomeAvg60", stats.IO.SomeAvg60},
		{"IO.SomeAvg300", stats.IO.SomeAvg300},
		{"IO.FullAvg10", stats.IO.FullAvg10},
		{"IO.FullAvg60", stats.IO.FullAvg60},
		{"IO.FullAvg300", stats.IO.FullAvg300},
	} {
		if tc.v < 0 {
			t.Errorf("%s = %v, want >= 0", tc.name, tc.v)
		}
	}
}

func TestReadPressureFileCPU(t *testing.T) {
	if _, err := os.Stat(psiCPUPath); err != nil {
		t.Skipf("PSI not available on this kernel (%v), skipping", err)
	}

	ps, err := readPressureFile(psiCPUPath)
	if err != nil {
		t.Fatalf("readPressureFile(%q) error: %v", psiCPUPath, err)
	}

	// CPU /proc/pressure/cpu has no "full" line.
	if ps.FullAvg10 != 0 || ps.FullAvg60 != 0 || ps.FullAvg300 != 0 {
		t.Errorf("CPU full averages must be zero, got FullAvg10=%v FullAvg60=%v FullAvg300=%v",
			ps.FullAvg10, ps.FullAvg60, ps.FullAvg300)
	}
}

func TestReadPressureFileBadPath(t *testing.T) {
	_, err := readPressureFile(`/proc/pressure/doesnotexist`)
	if err == nil {
		t.Fatal("expected error for nonexistent pressure file, got nil")
	}
}

func TestReadPressureFileMalformed(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "psi")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("some garbage data that is not valid\n")
	f.Close()

	_, err = readPressureFile(f.Name())
	if err == nil {
		t.Fatal("expected error parsing malformed pressure file, got nil")
	}
}

func TestReadPressureFileWellFormed(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "psi")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("some avg10=1.10 avg60=2.20 avg300=3.30 total=12345\n")
	f.WriteString("full avg10=0.10 avg60=0.20 avg300=0.30 total=6789\n")
	f.Close()

	ps, err := readPressureFile(f.Name())
	if err != nil {
		t.Fatalf("readPressureFile error: %v", err)
	}
	if ps.SomeAvg10 != 1.10 {
		t.Errorf("SomeAvg10 = %v, want 1.10", ps.SomeAvg10)
	}
	if ps.SomeAvg60 != 2.20 {
		t.Errorf("SomeAvg60 = %v, want 2.20", ps.SomeAvg60)
	}
	if ps.SomeAvg300 != 3.30 {
		t.Errorf("SomeAvg300 = %v, want 3.30", ps.SomeAvg300)
	}
	if ps.FullAvg10 != 0.10 {
		t.Errorf("FullAvg10 = %v, want 0.10", ps.FullAvg10)
	}
	if ps.FullAvg60 != 0.20 {
		t.Errorf("FullAvg60 = %v, want 0.20", ps.FullAvg60)
	}
	if ps.FullAvg300 != 0.30 {
		t.Errorf("FullAvg300 = %v, want 0.30", ps.FullAvg300)
	}
}

// TestReadPressureFileCPUOnlyHasSome simulates a CPU-style file (some line only)
// and confirms Full fields remain zero.
func TestReadPressureFileCPUOnlyHasSome(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "psi_cpu")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("some avg10=0.50 avg60=1.00 avg300=2.00 total=99999\n")
	f.Close()

	ps, err := readPressureFile(f.Name())
	if err != nil {
		t.Fatalf("readPressureFile error: %v", err)
	}
	if ps.FullAvg10 != 0 || ps.FullAvg60 != 0 || ps.FullAvg300 != 0 {
		t.Errorf("full averages must be zero for some-only file, got %v %v %v",
			ps.FullAvg10, ps.FullAvg60, ps.FullAvg300)
	}
	if ps.SomeAvg10 != 0.50 {
		t.Errorf("SomeAvg10 = %v, want 0.50", ps.SomeAvg10)
	}
}
