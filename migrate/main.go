/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"flag"
	"net"
	"os"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	confLoc   = flag.String("config-file", `/opt/gravwell/etc/migrate.conf`, "Location for configuration file")
	confdLoc  = flag.String("config-overlays", `/opt/gravwell/etc/migrate.conf.d`, "Location for configuration overlay files")
	verbose   = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver       = flag.Bool("version", false, "Print the version information and exit")
	status    = flag.Bool("status", false, "Print status updates and ingest rate")
	fParanoid = flag.Bool("paranoid", false, "Update the state file every time Splunk grabs a chunk (this can lead to really big state files!)")
	v         bool
	lg        *log.Logger
	src       net.IP

	st *StateTracker

	igst *ingest.IngestMuxer

	cfg *cfgType
)

func init() {
	v = true
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(&discard{})
	//lg.AddWriter(os.Stderr)
	lg.SetAppname(appName)
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
}

func main() {
	// Make a local writer so we can write to the console if something goes wrong
	llg := log.New(&discard{})
	llg.AddWriter(os.Stderr)
	llg.SetAppname(appName)

	var err error
	doneChan := make(chan bool)
	time.Sleep(500 * time.Millisecond)
	// this thing hits the filesystem, parallelism will almost always be bad
	//utils.MaxProcTune(1)
	cfg, err = GetConfig(*confLoc, *confdLoc)
	if err != nil {
		llg.FatalCode(0, "failed to get configuration", log.KVErr(err))
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			llg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			llg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if err = llg.AddWriter(fout); err != nil {
			llg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				llg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Log_Level), log.KVErr(err))
			}
		}
	}

	ctx, cf := context.WithCancel(context.Background())
	defer cf()
	st, err = NewStateTracker(cfg.StatePath())
	if err != nil {
		llg.FatalCode(0, "Failed to load state store file", log.KVErr(err))
	}

	go guiMain(doneChan, st)

	// Set up the early Splunk stuff
	if stop, err := initializeSplunk(cfg, st, ctx); err != nil {
		llg.FatalCode(0, "Failed to initialize splunk", log.KVErr(err))
	} else if stop {
		return
	}

	igst = getIngestConnection(cfg, lg)

	<-doneChan

	// write out the statuses to the state file
	for _, v := range splunkTracker.GetAllStatuses() {
		st.Add(splunkStateType, v)
	}

	if err = igst.Close(); err != nil {
		st.Close()
		llg.FatalCode(0, "failed to close ingest connection", log.KVErr(err))
	} else if err = st.Close(); err != nil {
		llg.FatalCode(0, "failed to close state store", log.KVErr(err))
	}
}
