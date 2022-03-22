//go:build linux
// +build linux

/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	confLoc  = flag.String("config-file", ``, "Location for configuration file")
	confdLoc = flag.String("config-overlays", ``, "Location for configuration overlay files")
	verbose  = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver      = flag.Bool("version", false, "Print the version information and exit")
	status   = flag.Bool("status", false, "Print status updates and ingest rate")
	v        bool
	lg       *log.Logger
	src      net.IP
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(&discard{})
	if !*status {
		lg.AddWriter(os.Stdout)
	}
	lg.SetAppname(appName)
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
}

func main() {
	// this thing hits the filesystem, parallelism will almost always be bad
	utils.MaxProcTune(1)
	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Log_Level), log.KVErr(err))
			}
		}
	}

	igst := getIngestConnection(cfg, lg)
	st, err := NewStateTracker(cfg.StatePath())
	if err != nil {
		lg.FatalCode(0, "Failed to load state store file", log.KVErr(err))
	}
	ctx, cf := context.WithCancel(context.Background())
	qc := utils.GetQuitChannel()
	statusChan, errChan := processSources(cfg, st, igst, ctx)
	if *status {
		go statusPrinter(statusChan)
	} else {
		go statusEater(statusChan)
	}

	select {
	case <-qc:
		lg.Info("interrupt caught, waiting for processors to exit")
		fmt.Println("interrupt caught, waiting for processors to exit")
		cf()
		err = <-errChan //wait for the processor to exit
		debugout("Finished with %v after interrupt\n", err)
	case err = <-errChan:
		cf()
		if err != nil {
			lg.Error("migration error", log.KVErr(err))
			debugout("error: %v\n", err)
		} else {
			debugout("done\n")
		}
	}

	debugout("Closing migration engine\n")
	if err = igst.Close(); err != nil {
		st.Close()
		lg.FatalCode(0, "failed to close ingest connection", log.KVErr(err))
	} else if err = st.Close(); err != nil {
		lg.FatalCode(0, "failed to close state store", log.KVErr(err))
	}
	debugout("Completed\n")
}

func processSources(cfg *cfgType, st *StateTracker, igst *ingest.IngestMuxer, ctx context.Context) (<-chan statusUpdate, <-chan error) {
	sc := make(chan statusUpdate, 1)
	ec := make(chan error, 1)
	go processSourcesAsync(cfg, st, igst, ctx, sc, ec)
	return sc, ec
}

func processSourcesAsync(cfg *cfgType, st *StateTracker, igst *ingest.IngestMuxer, ctx context.Context, sc chan statusUpdate, errch chan error) {
	defer close(errch)
	defer close(sc)
	//process flat files
	if err := processFiles(cfg, st, igst, ctx, sc); err != nil {
		errch <- err
		return
	}

	return
}
