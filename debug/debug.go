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

const CPU_SLEEP = 10 * time.Second

// HandleDebugSignals is a SIGUSR1 trap that can be installed at the beginning
// of runtime to generate a stack trace, memory profile, and CPU profile. It
// takes a name to be used as a directory prefix, and creates files in the
// system temporary directory.
func HandleDebugSignals(name string) {
	for {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGUSR1)

		<-c

		// get files prepped
		dir, err := os.MkdirTemp("", name)
		if err != nil {
			continue
		}
		stackTraceName := filepath.Join(dir, "stack")
		memName := filepath.Join(dir, "mem.prof")
		cpuName := filepath.Join(dir, "cpu.prof")

		st, err := os.Create(stackTraceName)
		if err != nil {
			continue
		}
		mem, err := os.Create(memName)
		if err != nil {
			continue
		}
		cpu, err := os.Create(cpuName)
		if err != nil {
			continue
		}

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
		}
		st.Write(buf[:n])
		st.Close()

		// return a memory profile
		membuf := &bytes.Buffer{}
		runtime.GC()
		if err := pprof.WriteHeapProfile(membuf); err == nil {
			mem.Write(membuf.Bytes())
			mem.Close()
		}

		// return a cpu profile
		cpubuf := &bytes.Buffer{}
		if err := pprof.StartCPUProfile(cpubuf); err == nil {
			time.Sleep(CPU_SLEEP)
			pprof.StopCPUProfile()
			cpu.Write(cpubuf.Bytes())
			cpu.Close()
		}
	}
}
