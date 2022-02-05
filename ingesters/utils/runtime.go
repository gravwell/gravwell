/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"os"
	"runtime"
)

// this will set the GOMAXPROC value ONLY if the environment variable hasn't been set to a valid integer
func MaxProcTune(val int) bool {
	if ev := os.Getenv(`GOMAXPROCS`); ev == `` {
		//try to parse it as an int
		return runtime.GOMAXPROCS(val) != val
	}
	return false
}
