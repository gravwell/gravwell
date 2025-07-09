//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"bytes"
	"os"
)

var kernelVersion string

func init() {
	if val, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernelVersion = string(bytes.Trim(val, " \n\r"))
	}
}
