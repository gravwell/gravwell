/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
	"errors"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingesters/v3/version"
	"github.com/gravwell/winevent/v3"
)

const (
	serviceName       = `GravwellEvents`
	defaultConfigPath = `gravwell\eventlog\config.cfg`
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verboseF       = flag.Bool("v", false, "Verbose mode, do not run as a service and output status to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	confLoc string
	verbose bool
	errW    errWriter = interactiveErrorWriter
	warnW   errWriter = interactiveWarnWriter
	infW    errWriter = interactiveInfoWriter
	elog    debug.Log
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	if *configOverride == "" {
		var err error
		confLoc, err = winevent.ProgramDataFilename(defaultConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get config file path: %v\n", err)
			os.Exit(-1)
		}
	} else {
		confLoc = *configOverride
	}
	verbose = *verboseF
}

func main() {
	inter, err := svc.IsAnInteractiveSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get interactive session status: %v\n", err)
		return
	}
	if !inter {
		e, err := eventlog.Open(serviceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get event log handle: %v\n", err)
			return
		}
		elog = e
		errW = serviceErrorWriter
		warnW = serviceWarnWriter
		infW = serviceInfoWriter
		infoout("%s started\n", serviceName)
	}
	cfg, err := winevent.GetConfig(confLoc)
	if err != nil {
		errorout("Failed to get configuration: %v\n", err)
		return
	}

	s, err := NewService(cfg)
	if err != nil {
		errorout("Failed to create gravwell servicer: %v\n", err)
		return
	}

	if inter {
		runInteractive(s)
	} else {
		runService(s)
	}
	if err := s.Close(); err != nil {
		errorout("Failed to close servicer: %v\n", err)
	}
}

func runInteractive(s *mainService) {
	//fire off the event consumers
	closer := make(chan svc.ChangeRequest, 1)
	defer close(closer)
	status := make(chan svc.Status, 1)
	defer close(status)
	sigChan := make(chan os.Signal, 1)
	defer close(sigChan)

	signal.Notify(sigChan, os.Interrupt, os.Kill)
	go s.Execute(nil, closer, status)
loop:
	for {
		select {
		case <-sigChan:
			debugout("Caught close signal\n")
			if err := serviceWriteTimeout(closer, svc.ChangeRequest{Cmd: svc.Shutdown}, time.Second); err != nil {
				errorout("Failed to send shutdown command: %v\n", err)
			}
		case st := <-status:
			switch st.State {
			case svc.StopPending:
				debugout("Service is stopping\n")
			case svc.Stopped:
				debugout("Service stopped\n")
				break loop
			case svc.StartPending:
				debugout("Service is starting\n")
			case svc.Running:
				debugout("Service is running\n")
			default:
				errorout("Got unknown status update: #%d\n", st.State)
			}
		}
	}
}

func runService(s *mainService) {
	if err := svc.Run(serviceName, s); err != nil {
		errorout("Failed to run service: %v\n", err)
		return
	}
	infoout("Service stopped\n")
}

func serviceWriteTimeout(ch chan svc.ChangeRequest, r svc.ChangeRequest, to time.Duration) (err error) {
	select {
	case ch <-r:
	case <-time.After(to):
		err = errors.New("Timeout")
	}
	return
}

func debugout(format string, args ...interface{}) {
	if !verbose {
		return
	}
	fmt.Printf(format, args...)
}

type errWriter func(format string, args ...interface{})

func interactiveErrorWriter(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func serviceErrorWriter(format string, args ...interface{}) {
	elog.Error(1, fmt.Sprintf(strings.Trim(format, "\n\r"), args...))
}

func interactiveWarnWriter(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func serviceWarnWriter(format string, args ...interface{}) {
	elog.Warning(1, fmt.Sprintf(strings.Trim(format, "\n\r"), args...))
}

func interactiveInfoWriter(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

func serviceInfoWriter(format string, args ...interface{}) {
	elog.Info(1, fmt.Sprintf(strings.Trim(format, "\n\r"), args...))
}

func errorout(format string, args ...interface{}) {
	if errW != nil {
		errW(format, args...)
	}
}

func infoout(format string, args ...interface{}) {
	if infW != nil {
		infW(format, args...)
	}
}

func warnout(format string, args ...interface{}) {
	if warnW != nil {
		warnW(format, args...)
	}
}
