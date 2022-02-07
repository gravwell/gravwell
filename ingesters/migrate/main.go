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
	"flag"
	"net"
	"os"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

var (
	confLoc = flag.String("config-file", ``, "Location for configuration file")
	verbose = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver     = flag.Bool("version", false, "Print the version information and exit")
	status  = flag.Bool("status", false, "Print status updates and ingest rate")
	v       bool
	lg      *log.Logger
	src     net.IP
)

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)

	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc)
}

func main() {
	// this thing hits the filesystem, parallelism will almost always be bad
	utils.MaxProcTune(1)
	cfg, err := GetConfig(*confLoc)
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
	defer igst.Close()
	st, err := NewStateTracker(cfg.StatePath())
	if err != nil {
		lg.FatalCode(0, "Failed to load state store file", log.KVErr(err))
	}
	statusChan := make(chan statusUpdate, 1)
	if *status {
		go statusPrinter(statusChan)
	} else {
		go statusEater(statusChan)
	}
	if err := processSources(cfg, st, igst, statusChan); err != nil {
		igst.Close()
		st.Close()
		lg.FatalCode(0, "failed to process files", log.KVErr(err))
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

func processSources(cfg *cfgType, st *StateTracker, igst *ingest.IngestMuxer, sc chan statusUpdate) (err error) {
	var shouldQuit bool
	qc := utils.GetQuitChannel()
	//process flat files
	if shouldQuit, err = processFiles(cfg, st, igst, qc, sc); err != nil || shouldQuit {
		return
	}

	return
}
