/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package utils

import (
	"log"
	"os"
	"runtime"
	"runtime/pprof"
)

const (
	envCpuProfileName = `CPU_PROFILE`
	envMemProfileName = `MEM_PROFILE`
)

var (
	cpuProfFile *os.File
	memProfFile *os.File
)

func StartProfile() {
	if pth := os.Getenv(envCpuProfileName); pth != `` {
		var err error
		if cpuProfFile, err = os.Create(pth); err != nil {
			log.Fatalf("failed to create CPU profile at %s - %v\n", pth, err)
		} else if err = pprof.StartCPUProfile(cpuProfFile); err != nil {
			log.Fatalf("failed to start CPU profile at %s - %v\n", pth, err)
		}
	}

}

func StopProfile() {
	if cpuProfFile != nil {
		pprof.StopCPUProfile()
		if err := cpuProfFile.Close(); err != nil {
			log.Printf("Failed to close CPU profile file - %v\n", err)
		}
	}
	if pth := os.Getenv(envMemProfileName); pth != `` {
		var err error
		if memProfFile, err = os.Create(pth); err != nil {
			log.Printf("failed to create MEM profile at %s - %v\n", pth, err)
		} else {
			runtime.GC()
			if err = pprof.WriteHeapProfile(memProfFile); err != nil {
				memProfFile.Close()
				log.Printf("failed to start Memory profile at %s - %v\n", pth, err)
			} else if err = memProfFile.Close(); err != nil {
				log.Printf("failed to close Memory profile file %s - %v\n", pth, err)
			}
		}
	}
}
