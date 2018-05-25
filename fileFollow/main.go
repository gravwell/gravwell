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
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/filewatch"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/ingesters/version"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/file_follow.conf`
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	confLoc        string

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
	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
		version.PrintVersion(fout)
		ingest.PrintVersion(fout)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing

	if *configOverride == "" {
		confLoc = defaultConfigLoc
	} else {
		confLoc = *configOverride
	}
	v = *verbose
}

func main() {
	cfg, err := GetConfig(confLoc)
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

	wtcher, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		lg.Fatal("Failed to create notification watcher: %v\n", err)
	}

	//fire up the ingesters
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))
	debugout("INSECURE skipping TLS certs verification: %v\n", cfg.InsecureSkipTLSVerification())
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		IngesterName: "filefollow",
		VerifyCert:   !cfg.InsecureSkipTLSVerification(),
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
	ch := make(chan *entry.Entry, 2048)

	//pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtcher.SetLogger(igst)

	//build a list of base directories and globs
	for k, val := range cfg.Follower {
		//get the tag for this listener
		tag, err := igst.GetTag(val.Tag_Name)
		if err != nil {
			lg.Fatal("Failed to resolve tag \"%s\" for %s: %v\n", val.Tag_Name, k, err)
		}
		//create our handler for this watcher
		lh, err := filewatch.NewLogHandler(tag, val.Ignore_Timestamps, val.Assume_Local_Timezone, ch)
		if err != nil {
			lg.Fatal("Failed to generate handler: %v", err)
		}
		if v {
			lh.SetLogger(debugout)
		}
		c := filewatch.WatchConfig{
			ConfigName: k,
			BaseDir:    val.Base_Directory,
			FileFilter: val.File_Filter,
			Hnd:        lh,
		}
		if err := wtcher.Add(c); err != nil {
			wtcher.Close()
			lg.Fatal("Failed to add watch directory for %s (%s): %v\n",
				val.Base_Directory, val.File_Filter, err)
		}

	}

	if err := wtcher.Start(); err != nil {
		wtcher.Close()
		igst.Close()
		lg.Fatal("Failed to start file watcher: %v\n", err)
	}

	debugout("Started following %d locations\n", len(cfg.Follower))
	//fire off our relay
	var src net.IP
	if cfg.Source_Override != "" {
		// global override
		src = net.ParseIP(cfg.Source_Override)
		if src == nil {
			lg.Fatal("Global Source-Override is invalid")
		}
	}
	doneChan := make(chan error, 1)
	go relay(ch, doneChan, src, igst)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	<-sch
	debugout("Attempting to close the watcher... ")
	if err := wtcher.Close(); err != nil {
		lg.Error("Failed to close file follower: %v\n", err)
	}
	debugout("Done\n")
	close(ch) //to inform the relay that no new entries are going to come down the pipe

	//wait for our ingest relay to exit
	<-doneChan
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err = igst.Close(); err != nil {
		lg.Error("Failed to close ingest muxer: %v", err)
	}
}

func relay(ch chan *entry.Entry, done chan error, srcOverride net.IP, igst *ingest.IngestMuxer) {
	for e := range ch {
		if srcOverride != nil {
			e.SRC = srcOverride
		}
		if err := igst.WriteEntry(e); err != nil {
			lg.Warn("Failed to write entry: %v", err)
		}
	}
	done <- nil
}
func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
