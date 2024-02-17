/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	dbg "runtime/debug"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/crewjam/rfc5424"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/winevent"
)

const (
	serviceName       = `GravwellFileFollow`
	defaultConfigPath = `gravwell\filefollow\file_follow.cfg`
	defaultStateLoc   = `gravwell\filefollow\file_follow.state`
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verboseF       = flag.Bool("v", false, "Verbose mode, do not run as a service and output status to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	dumpState      = flag.Bool("dump-state", false, "Dump the file follower state file in a human format and exit")

	confLoc string
	debugOn bool
	errW    errWriter = interactiveErrorWriter
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
	debugOn = *verboseF
	validate.ValidateIngesterConfig(GetConfig, confLoc, ``) //windows doesn't support conf.d style overlays for now
}

func main() {
	dbg.SetTraceback("all")
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
		infW = serviceInfoWriter
		infoout("%s started", serviceName)
	} else {
		fmt.Fprintf(os.Stderr, "Starting as interactive application\n")
	}
	cfg, err := GetConfig(confLoc, ``) // windows doesn't support the conf.d style overlays
	if err != nil {
		errorout("Failed to get configuration: %v", err)
		return
	}
	//check if we have a UUID, if not try to write one back
	if id, ok := cfg.global.IngestConfig.IngesterUUID(); !ok {
		id = uuid.New()
		if err := cfg.global.IngestConfig.SetIngesterUUID(id, confLoc); err != nil {
			errorout("failed to set ingester UUID at startup: %v", err)
			return
		}
	}

	//create a service, this is used even if we are running in interactive mode
	s, err := NewService(cfg)
	if err != nil {
		errorout("Failed to create gravwell service: %v", err)
		return
	}

	if inter {
		if *dumpState {
			dumpStateFile(cfg.State_Store_Location)
			os.Exit(0)
		}
		runInteractive(s)
	} else {
		runService(s)
	}
	if err := s.Close(); err != nil {
		errorout("Failed to close servicer: %v\n", err)
	}
}

func runInteractive(s *mainService) {
	//fire off the event consumers, there is no reason to close any of these, we are leaving anyway
	closer := make(chan svc.ChangeRequest, 1)
	status := make(chan svc.Status, 1)
	sigChan := make(chan os.Signal, 1)

	signal.Notify(sigChan, os.Interrupt, os.Kill)
	go s.Execute(nil, closer, status)

	debugout("Running interactive loop")
loop:
	for {
		select {
		case <-sigChan:
			debugout("Caught close signal\n")
			closer <- svc.ChangeRequest{Cmd: svc.Shutdown}
		case st := <-status:
			switch st.State {
			case svc.StopPending:
				debugout("Service is stopping\n")
				fallthrough
			case svc.Stopped:
				debugout("Service stopped\n")
				break loop
			case svc.StartPending:
				debugout("Service is starting\n")
			case svc.Running:
				debugout("Service is running\n")
			default:
				debugout("Got unknown status update: #%d\n", st.State)
			}
		}
	}
}

func runService(s *mainService) {
	if err := svc.Run(serviceName, s); err != nil {
		errorout("Failed to run service: %v\n", err)
	}
	debugout("Service stopped\n")
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

type errWriter func(format string, args ...interface{})

func interactiveErrorWriter(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

func serviceErrorWriter(format string, args ...interface{}) {
	elog.Error(1, fmt.Sprintf(format, args...))
}

func interactiveInfoWriter(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

func serviceInfoWriter(format string, args ...interface{}) {
	elog.Info(1, fmt.Sprintf(format, args...))
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

type debugLogger struct {
}

func (dl debugLogger) Debugf(f string, args ...interface{}) error {
	debugout(f, args...)
	return nil
}

func (dl debugLogger) Infof(f string, args ...interface{}) error {
	infoout(f, args...)
	return nil
}

func (dl debugLogger) Warnf(f string, args ...interface{}) error {
	infoout(f, args...)
	return nil
}

func (dl debugLogger) Errorf(f string, args ...interface{}) error {
	errorout(f, args...)
	return nil
}

func (dl debugLogger) Criticalf(f string, args ...interface{}) error {
	errorout(f, args...)
	return nil
}

func (dl debugLogger) Debug(msg string, args ...rfc5424.SDParam) error {
	infoout(formatStructured(msg, args...))
	return nil
}

func (dl debugLogger) Info(msg string, args ...rfc5424.SDParam) error {
	infoout(formatStructured(msg, args...))
	return nil
}

func (dl debugLogger) Warn(msg string, args ...rfc5424.SDParam) error {
	infoout(formatStructured(msg, args...))
	return nil
}

func (dl debugLogger) Error(msg string, args ...rfc5424.SDParam) error {
	errorout(formatStructured(msg, args...))
	return nil
}

func (dl debugLogger) Critical(msg string, args ...rfc5424.SDParam) error {
	errorout(formatStructured(msg, args...))
	return nil
}

func (g *global) verifyStateStore() (err error) {
	if g.State_Store_Location == `` {
		g.State_Store_Location, err = winevent.ProgramDataFilename(defaultStateLoc)
	}
	return
}

func formatStructured(msg string, args ...rfc5424.SDParam) (r string) {
	if len(msg) > 0 {
		r = msg
	}
	for _, arg := range args {
		if len(r) > 0 {
			r += " "
		}
		r += fmt.Sprintf("%q=%q", arg.Name, arg.Value)
	}
	return
}
