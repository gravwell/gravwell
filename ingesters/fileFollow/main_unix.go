//go:build linux || darwin
// +build linux darwin

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
	"fmt"
	"net"
	"os"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/filewatch"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/file_follow.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/file_follow.conf.d`
	defaultStateLoc   = `/opt/gravwell/etc/file_follow.state`
	appName           = `filefollow`
)

var (
	dumpState = flag.Bool("dump-state", false, "Dump the file follower state file in a human format and exit")

	debugOn bool
	lg      *log.Logger
)

func main() {
	go debug.HandleDebugSignals(appName)
	utils.MaxProcTune(1) // this thing hits the filesystem, parallelism will almost always be bad

	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 appName,
		AppName:                      appName,
		DefaultConfigLocation:        defaultConfigLoc,
		DefaultConfigOverlayLocation: defaultConfigDLoc,
		GetConfigFunc:                GetConfig,
	}
	ib, err := base.Init(ibc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get configuration %v\n", err)
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	debugOn = ib.Verbose
	lg = ib.Logger

	//check if we are just dumping state, if so, do it and exit cleanly
	if *dumpState {
		dumpStateFile(cfg.State_Store_Location)
		os.Exit(0)
	}

	id, ok := cfg.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()

	debugout("Started ingester muxer\n")

	var src net.IP
	if cfg.Source_Override != "" {
		// global override
		if src = net.ParseIP(cfg.Source_Override); src == nil {
			lg.Fatal("Global Source-Override is invalid", log.KV("sourceoverride", cfg.Source_Override))
		}
	} else {
		//it is fine to set it to nil, it will be set by the ingest muxer, this can and WILL fail sometimes
		src, _ = igst.SourceIP()
	}

	wtcher, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		lg.Fatal("failed to create notification watcher", log.KVErr(err))
	}

	//pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtcher.SetLogger(igst)
	wtcher.SetMaxFilesWatched(cfg.Max_Files_Watched)

	var procs []*processors.ProcessorSet

	var window timegrinder.TimestampWindow
	window, err = cfg.GlobalTimestampWindow()
	if err != nil {
		lg.Fatal("Failed to get global timestamp window", log.KVErr(err))
	}

	//build a list of base directories and globs
	for k, val := range cfg.Follower {
		pproc, err := cfg.Preprocessor.ProcessorSet(igst, val.Preprocessor)
		if err != nil {
			lg.FatalCode(0, "preprocessor construction error", log.KVErr(err))
		}
		procs = append(procs, pproc)
		//get the tag for this listener
		tag, err := igst.GetTag(val.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("watcher", k), log.KV("tag", val.Tag_Name), log.KVErr(err))
		}

		tsFmtOverride, err := val.TimestampOverride()
		if err != nil {
			lg.FatalCode(0, "invalid timestamp override", log.KV("timestampoverride", val.Timestamp_Format_Override), log.KVErr(err))
		}

		//create our handler for this watcher
		cfg := filewatch.LogHandlerConfig{
			TagName:                 val.Tag_Name,
			Tag:                     tag,
			Src:                     src,
			IgnoreTS:                val.Ignore_Timestamps,
			AssumeLocalTZ:           val.Assume_Local_Timezone,
			IgnorePrefixes:          val.Ignore_Line_Prefix,
			IgnoreGlobs:             val.Ignore_Glob,
			TimestampFormatOverride: tsFmtOverride,
			UserTimeRegex:           val.Timestamp_Regex,
			UserTimeFormat:          val.Timestamp_Format_String,
			Logger:                  lg,
			TimezoneOverride:        val.Timezone_Override,
			Ctx:                     wtcher.Context(),
			TimeFormat:              cfg.TimeFormat,
			AttachFilename:          val.Attach_Filename,
			Trim:                    val.Trim,
			TimestampWindow:         window,
		}
		if debugOn {
			cfg.Debugger = debugout
		}
		lh, err := filewatch.NewLogHandler(cfg, pproc)
		if err != nil {
			lg.Fatal("failed to generate handler", log.KVErr(err))
		}
		c := filewatch.WatchConfig{
			ConfigName: k,
			BaseDir:    val.Base_Directory,
			FileFilter: val.File_Filter,
			Hnd:        lh,
			Recursive:  val.Recursive,
		}
		if rex, ok, err := val.TimestampDelimited(); err != nil {
			lg.FatalCode(0, "invalid timestamp delimiter", log.KVErr(err))
		} else if ok {
			c.Engine = filewatch.RegexEngine
			c.EngineArgs = rex
		} else if val.Regex_Delimiter != `` {
			c.Engine = filewatch.RegexEngine
			c.EngineArgs = val.Regex_Delimiter
		} else {
			c.Engine = filewatch.LineEngine
		}
		if err := wtcher.Add(c); err != nil {
			wtcher.Close()
			lg.Fatal("failed to add watch directory", log.KV("path", val.Base_Directory),
				log.KV("filter", val.File_Filter), log.KVErr(err))
		}
	}
	qc := utils.GetQuitChannel()
	if quit, err := wtcher.Catchup(qc); err != nil {
		lg.Error("failed to catchup file watcher", log.KVErr(err))
		wtcher.Close()
		igst.Close()
		os.Exit(-1)
	} else if !quit {
		//doing a normal startup
		if err := wtcher.Start(); err != nil {
			lg.Error("failed to start file watcher", log.KVErr(err))
			wtcher.Close()
			igst.Close()
			os.Exit(-1)
		}

		debugout("Started following %d locations\n", len(cfg.Follower))
		debugout("Running\n")
		//listen for signals so we can close gracefully
		select {
		case <-qc:
		case <-wtcher.Context().Done():
		}
	}
	debugout("Attempting to close the watcher... ")
	if err := wtcher.Close(); err != nil {
		lg.Error("failed to close file follower", log.KVErr(err))
	}
	debugout("Done\n")

	//close down all the preprocessors
	for _, v := range procs {
		if v != nil {
			if err := v.Close(); err != nil {
				lg.Error("failed to close processors", log.KVErr(err))
			}
		}
	}

	//wait for our ingest relay to exit
	lg.Info("filefollower ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

func (g *global) verifyStateStore() (err error) {
	if g.State_Store_Location == `` {
		g.State_Store_Location = defaultStateLoc
	}
	return
}
