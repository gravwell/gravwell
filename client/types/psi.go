//go:build !linux
// +build !linux

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "errors"

// ErrPSINotAvailable is returned on platforms that do not support Linux Pressure Stall Information.
var ErrPSINotAvailable = errors.New("pressure stall information is not available on this platform")

// SamplePSI is not supported on non-Linux platforms and always returns ErrPSINotAvailable.
func SamplePSI() (PSIStats, error) {
	return PSIStats{}, ErrPSINotAvailable
}
