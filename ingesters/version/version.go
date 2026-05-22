/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package version just implements some globals and helpers that all ingesters can import
package version

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	MajorVersion = 5
	MinorVersion = 9
	PointVersion = 0
)

var (
	BuildDate time.Time = time.Date(2026, 4, 23, 23, 59, 59, 0, time.UTC)
)

func PrintVersion(wtr io.Writer) {
	fmt.Fprintf(wtr, "Version:\t%d.%d.%d\n", MajorVersion, MinorVersion, PointVersion)
	fmt.Fprintf(wtr, "BuildDate:\t%s\n", BuildDate.Format(`2006-01-02 15:04:05`))
	fmt.Fprintf(wtr, "Runtime:\t%s\n", strings.TrimPrefix(runtime.Version(), "go"))
}

func GetVersion() string {
	return Current().String()
}

type Canonical struct {
	Major int
	Minor int
	Point int
}

func Current() Canonical {
	return Canonical{
		Major: MajorVersion,
		Minor: MinorVersion,
		Point: PointVersion,
	}
}

var rx = regexp.MustCompile(`^(?P<major>\d+)\.(?P<minor>\d+)\.(?P<point>\d+)$`)

func Parse(v string) (c Canonical, err error) {
	m := rx.FindStringSubmatch(v)
	if len(m) != 4 {
		err = errors.New("invalid canonical version string")
		return
	}
	major, minor, point := m[1], m[2], m[3]
	// we can use Atoi here and just do a simple check on < 0 because the regex should prevent negative numbers
	// the < 0 check is redundant but I am leaving it
	if c.Major, err = strconv.Atoi(major); err != nil || c.Major < 0 {
		err = fmt.Errorf("invalid major version %q %w", major, err)
	} else if c.Minor, err = strconv.Atoi(minor); err != nil || c.Minor < 0 {
		err = fmt.Errorf("invalid minor version %q %w", minor, err)
	} else if c.Point, err = strconv.Atoi(point); err != nil || c.Point < 0 {
		err = fmt.Errorf("invalid point version %q %w", point, err)
	}
	return
}

func (c Canonical) String() string {
	return fmt.Sprintf("%d.%d.%d", c.Major, c.Minor, c.Point)
}
