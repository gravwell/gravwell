/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"context"
	"os"
	"runtime"
	"time"
)

const (
	ExitSyncTimeout = 10 * time.Second
)

// MaxProcTune will set the GOMAXPROC value ONLY if the environment variable hasn't been set to a valid integer
func MaxProcTune(val int) bool {
	if ev := os.Getenv(`GOMAXPROCS`); ev == `` {
		//try to parse it as an int
		return runtime.GOMAXPROCS(val) != val
	}
	return false
}

func QuitableSleep(ctx context.Context, to time.Duration) (quit bool) {
	select {
	case <-time.After(to):
	case <-ctx.Done():
		quit = true
	}
	return
}
