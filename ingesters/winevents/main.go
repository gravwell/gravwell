/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	dbg "runtime/debug"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/config/validate"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/version"
	"github.com/gravwell/gravwell/v4/winevent"
)

const (
	serviceName       = `GravwellEvents`
	appName           = `winevent`
	ingesterName      = `Windows Events`
	defaultConfigPath = `gravwell\eventlog\config.cfg`
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verboseF       = flag.Bool("v", false, "Verbose mode, do not run as a service and output status to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	confLoc string
	debugOn bool
	lg      *log.Logger
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
	validate.ValidateIngesterConfig(GetConfig, confLoc, ``)
}

func main() {
	dbg.SetTraceback("all")
	inter, err := svc.IsAnInteractiveSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get interactive session status: %v\n", err)
		return
	}
	if inter {
		lg = log.New(os.Stdout)
	} else {
		e, err := eventlog.Open(serviceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get event log handle: %v\n", err)
			return
		}
		lg = log.NewLevelRelay(levelLogger{elog: e})
	}
	lg.SetAppname(appName)

	cfg, err := GetConfig(confLoc)
	if err != nil {
		lg.Error("failed to get configuration", log.KVErr(err))
		return
	}
	//check if we have a UUID, if not try to write one back
	if id, ok := cfg.Global.IngestConfig.IngesterUUID(); !ok {
		id = uuid.New()
		if err := cfg.Global.IngestConfig.SetIngesterUUID(id, confLoc); err != nil {
			lg.Error("failed to set ingester UUID at startup", log.KVErr(err))
			return
		}
	}

	if len(cfg.Global.Log_Level) > 0 {
		if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
			lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Global.Log_Level), log.KVErr(err))
		}
	}
	lg.Info("service started", log.KV("name", serviceName))

	s, err := NewService(cfg)
	if err != nil {
		lg.Error("failed to create gravwell service", log.KVErr(err))
		return
	}

	if inter {
		runInteractive(s)
	} else {
		runService(s)
	}
	if err := s.Close(); err != nil {
		lg.Error("failed to create gravwell service", log.KVErr(err))
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
				lg.Error("failed to send shutdown command", log.KVErr(err))
			}
		case st := <-status:
			switch st.State {
			case svc.StopPending:
				lg.Debug("service is stopping")
			case svc.Stopped:
				lg.Debug("service stopped")
				break loop
			case svc.StartPending:
				lg.Debug("service is starting")
			case svc.Running:
				lg.Debug("service is running")
			default:
				lg.Error("got unknown status update", log.KV("state", st.State))
			}
		}
	}
}

func runService(s *mainService) {
	if err := svc.Run(serviceName, s); err != nil {
		lg.Error("failed to run service", log.KVErr(err))
		return
	}
	lg.Info("service stopped")
}

func serviceWriteTimeout(ch chan svc.ChangeRequest, r svc.ChangeRequest, to time.Duration) (err error) {
	select {
	case ch <- r:
	case <-time.After(to):
		err = errors.New("Timeout")
	}
	return
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

type levelLogger struct {
	elog debug.Log
}

// levelLogger implements the log.LevelRelay interface
func (l levelLogger) WriteLog(lvl log.Level, ts time.Time, msg, rawMsg string) error {
	switch lvl {
	case log.DEBUG:
		if debugOn {
			fmt.Fprintln(os.Stdout, rawMsg)
		}
	case log.INFO:
		return l.elog.Info(1, rawMsg)
	case log.WARN:
		return l.elog.Warning(1, rawMsg)
	case log.ERROR:
		fallthrough
	case log.CRITICAL:
		fallthrough
	case log.FATAL:
		return l.elog.Error(1, rawMsg)
	}
	return nil
}

func (l levelLogger) Close() error {
	return l.elog.Close()
}
