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
	"syscall"
	"time"

	"github.com/gravwell/filewatch/v3"
	"github.com/gravwell/ingest/v3"
	"github.com/gravwell/ingest/v3/log"
	"github.com/gravwell/ingesters/v3/utils"
	"github.com/gravwell/ingesters/v3/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/file_follow.conf`
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
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}

	v = *verbose
}

func main() {
	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
	}

	//fire up the ingesters
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))
	debugout("INSECURE skipping TLS certs verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations:    conns,
		Tags:            tags,
		Auth:            cfg.Secret(),
		LogLevel:        cfg.LogLevel(),
		IngesterName:    "filefollow",
		IngesterVersion: version.GetVersion(),
		IngesterUUID:    id.String(),
		VerifyCert:      !cfg.InsecureSkipTLSVerification(),
		Logger:          lg,
	}
	if cfg.EnableCache() {
		ingestConfig.EnableCache = true
		ingestConfig.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		ingestConfig.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("Failed build ingest system: %v\n", err)
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start ingest system: %v\n", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	var src net.IP
	if cfg.Source_Override != "" {
		// global override
		if src = net.ParseIP(cfg.Source_Override); src == nil {
			lg.Fatal("Global Source-Override is invalid")
		}
	} else if src, err = igst.SourceIP(); err != nil {
		lg.Fatal("Failed to resolve source IP from muxer: %v", err)
	}

	wtcher, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		lg.Fatal("Failed to create notification watcher: %v\n", err)
	}

	//pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtcher.SetLogger(igst)
	wtcher.SetMaxFilesWatched(cfg.Max_Files_Watched)

	//build a list of base directories and globs
	for k, val := range cfg.Follower {
		pproc, err := cfg.Preprocessor.ProcessorSet(igst, val.Preprocessor)
		if err != nil {
			lg.FatalCode(0, "Preprocessor construction error: %v", err)
		}
		//get the tag for this listener
		tag, err := igst.GetTag(val.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", val.Tag_Name, k, err)
		}
		var ignore [][]byte
		for _, prefix := range val.Ignore_Line_Prefix {
			if prefix != "" {
				ignore = append(ignore, []byte(prefix))
			}
		}
		tsFmtOverride, err := val.TimestampOverride()
		if err != nil {
			lg.FatalCode(0, "Invalid timestamp override \"%s\": %v\n", val.Timestamp_Format_Override, err)
		}

		//create our handler for this watcher
		cfg := filewatch.LogHandlerConfig{
			Tag:                     tag,
			Src:                     src,
			IgnoreTS:                val.Ignore_Timestamps,
			AssumeLocalTZ:           val.Assume_Local_Timezone,
			IgnorePrefixes:          ignore,
			TimestampFormatOverride: tsFmtOverride,
			Logger:                  lg,
			TimezoneOverride:        val.Timezone_Override,
		}
		if v {
			cfg.Debugger = debugout
		}
		lh, err := filewatch.NewLogHandler(cfg, pproc)
		if err != nil {
			lg.Fatal("Failed to generate handler: %v", err)
		}
		c := filewatch.WatchConfig{
			ConfigName: k,
			BaseDir:    val.Base_Directory,
			FileFilter: val.File_Filter,
			Hnd:        lh,
			Recursive:  val.Recursive,
		}
		if rex, ok, err := val.TimestampDelimited(); err != nil {
			lg.FatalCode(0, "Invalid timestamp delimiter: %v\n", err)
		} else if ok {
			c.Engine = filewatch.RegexEngine
			c.EngineArgs = rex
		} else {
			c.Engine = filewatch.LineEngine
		}
		if err := wtcher.Add(c); err != nil {
			wtcher.Close()
			lg.Fatal("Failed to add watch directory for %s (%s): %v\n",
				val.Base_Directory, val.File_Filter, err)
		}
	}

	if err := wtcher.Start(); err != nil {
		lg.Error("Failed to start file watcher: %v\n", err)
		wtcher.Close()
		igst.Close()
		os.Exit(-1)
	}

	debugout("Started following %d locations\n", len(cfg.Follower))

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	debugout("Attempting to close the watcher... ")
	if err := wtcher.Close(); err != nil {
		lg.Error("Failed to close file follower: %v\n", err)
	}
	debugout("Done\n")

	//wait for our ingest relay to exit
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err = igst.Close(); err != nil {
		lg.Error("Failed to close ingest muxer: %v", err)
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
