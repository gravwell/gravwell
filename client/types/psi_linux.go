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
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	psiCPUPath    = `/proc/pressure/cpu`
	psiMemoryPath = `/proc/pressure/memory`
	psiIOPath     = `/proc/pressure/io`
)

// SamplePSI reads Linux Pressure Stall Information from /proc/pressure and returns a populated PSIStats.
// CPU only exposes a "some" line; memory and IO expose both "some" and "full" lines.
func SamplePSI() (PSIStats, error) {
	var stats PSIStats
	var err error

	if stats.CPU, err = readPressureFile(psiCPUPath); err != nil {
		return PSIStats{}, fmt.Errorf("reading CPU pressure: %w", err)
	}
	if stats.Memory, err = readPressureFile(psiMemoryPath); err != nil {
		return PSIStats{}, fmt.Errorf("reading memory pressure: %w", err)
	}
	if stats.IO, err = readPressureFile(psiIOPath); err != nil {
		return PSIStats{}, fmt.Errorf("reading IO pressure: %w", err)
	}
	return stats, nil
}

// readPressureFile parses a /proc/pressure/* file into a PressureStats.
// Each file contains one or two lines with the format:
//
//	some avg10=X.XX avg60=X.XX avg300=X.XX total=N
//	full avg10=X.XX avg60=X.XX avg300=X.XX total=N
func readPressureFile(path string) (PressureStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return PressureStats{}, err
	}
	defer f.Close()

	var ps PressureStats
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		kind := fields[0] // "some" or "full"
		var avg10, avg60, avg300 float64
		var total uint64
		if _, err := fmt.Sscanf(
			strings.Join(fields[1:], " "),
			"avg10=%f avg60=%f avg300=%f total=%d",
			&avg10, &avg60, &avg300, &total,
		); err != nil {
			return PressureStats{}, fmt.Errorf("parsing line %q: %w", line, err)
		}
		switch kind {
		case "some":
			ps.SomeAvg10 = avg10
			ps.SomeAvg60 = avg60
			ps.SomeAvg300 = avg300
		case "full":
			ps.FullAvg10 = avg10
			ps.FullAvg60 = avg60
			ps.FullAvg300 = avg300
		}
	}
	return ps, scanner.Err()
}
