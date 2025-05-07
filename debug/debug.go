/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package debug implements some debug helpers that are used in all ingesters
package debug

import (
	"bytes"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"
)

const (
	CPU_SLEEP      = 10 * time.Second
	MAX_STACK_SIZE = 256 * 1024 * 1024
)

// HandleDebugSignals is a SIGUSR1 trap that can be installed at the beginning
// of runtime to generate a stack trace, memory profile, and CPU profile. It
// takes a name to be used as a directory prefix, and creates files in the
// system temporary directory.
func HandleDebugSignals(name string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR1)

	for range c {
		// get files prepped
		dir, err := os.MkdirTemp("", name)
		if err != nil {
			continue
		}

		DumpDebugFiles(dir)
	}
}

// DumpDebugFiles generates a stacktrace, memory profile, and CPU profile into the provided
// directory.  These are useful for runtime debugging and profiling.
func DumpDebugFiles(dir string) {
	generateStackTrace(dir)
	generateMemoryProfile(dir)
	generateCPUProfile(dir)
}

func generateStackTrace(dir string) {
	stackTraceName := filepath.Join(dir, "stack")
	st, err := os.Create(stackTraceName)
	if err != nil {
		return
	}
	defer st.Close()

	// return a trace, growing the buffer until it's big enough
	size := 1024 * 1024
	var buf []byte
	var n int
	for {
		buf = make([]byte, size)
		n = runtime.Stack(buf, true)
		if n < size {
			break
		}
		size *= 2
		if size >= MAX_STACK_SIZE {
			return
		}
	}
	st.Write(buf[:n])
}

func generateMemoryProfile(dir string) {
	memName := filepath.Join(dir, "mem.prof")
	mem, err := os.Create(memName)
	if err != nil {
		return
	}
	defer mem.Close()

	membuf := &bytes.Buffer{}
	runtime.GC()
	if err := pprof.WriteHeapProfile(membuf); err == nil {
		mem.Write(membuf.Bytes())
	}
}

func generateCPUProfile(dir string) {
	cpuName := filepath.Join(dir, "cpu.prof")
	cpu, err := os.Create(cpuName)
	if err != nil {
		return
	}
	defer cpu.Close()

	cpubuf := &bytes.Buffer{}
	if err := pprof.StartCPUProfile(cpubuf); err == nil {
		time.Sleep(CPU_SLEEP)
		pprof.StopCPUProfile()
		cpu.Write(cpubuf.Bytes())
	}
}
