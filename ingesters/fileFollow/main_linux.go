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
	"fmt"
	"net"
	"os"
	"path"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/filewatch"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/file_follow.conf`
	defaultStateLoc  = `/opt/gravwell/etc/file_follow.state`
	appName          = `filefollow`
)

var (
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")

	v  bool
	lg *log.Logger
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
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fout.Close()
				lg.Fatal("Failed to dup2 stderr", log.KVErr(err))
			}
		}
	}

	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc)
}

func main() {
	debug.SetTraceback("all")
	utils.MaxProcTune(1) // this thing hits the filesystem, parallelism will almost always be bad
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

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
	}

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))
	debugout("INSECURE skipping TLS certs verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		IngesterName:       "filefollow",
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

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
		var ignore [][]byte
		for _, prefix := range val.Ignore_Line_Prefix {
			if prefix != "" {
				ignore = append(ignore, []byte(prefix))
			}
		}
		tsFmtOverride, err := val.TimestampOverride()
		if err != nil {
			lg.FatalCode(0, "invalid timestamp override", log.KV("timestampoverride", val.Timestamp_Format_Override), log.KVErr(err))
		}

		//create our handler for this watcher
		cfg := filewatch.LogHandlerConfig{
			Tag:                     tag,
			Src:                     src,
			IgnoreTS:                val.Ignore_Timestamps,
			AssumeLocalTZ:           val.Assume_Local_Timezone,
			IgnorePrefixes:          ignore,
			TimestampFormatOverride: tsFmtOverride,
			UserTimeRegex:           val.Timestamp_Regex,
			UserTimeFormat:          val.Timestamp_Format_String,
			Logger:                  lg,
			TimezoneOverride:        val.Timezone_Override,
			Ctx:                     wtcher.Context(),
			TimeFormat:              cfg.TimeFormat,
		}
		if v {
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
		<-qc
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
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

func (g *global) verifyStateStore() (err error) {
	if g.State_Store_Location == `` {
		g.State_Store_Location = defaultStateLoc
	}
	return
}
