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
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/filewatch"
	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/file_follow.conf`
)

var (
	configOverride = flag.String("config-file-override", "", "Override location for configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	confLoc        string

	v bool
)

func init() {
	flag.Parse()
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
	}

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
		fmt.Fprintf(os.Stderr, "Failed to get configuration: %v\n", err)
		return
	}

	tags, err := cfg.Tags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get backend targets from configuration: %v\n", err)
		return
	}

	wtcher, err := filewatch.NewWatcher(cfg.StatePath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create notification watcher: %v\n", err)
		return
	}

	//fire up the ingesters
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))
	debugout("Verifying remote certs: %v\n", cfg.VerifyRemote())
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
		IngesterName: "filefollow",
		VerifyCert:   cfg.VerifyRemote(),
	}
	if cfg.EnableCache() {
		ingestConfig.EnableCache = true
		ingestConfig.CacheConfig.FileBackingLocation = cfg.LocalFileCachePath()
		ingestConfig.CacheConfig.MaxCacheSize = cfg.MaxCachedData()
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}

	//pass in the ingest muxer to the file watcher so it can throw info and errors down the muxer chan
	wtcher.SetLogger(igst)

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")
	ch := make(chan *entry.Entry, 2048)

	//build a list of base directories and globs
	for k, val := range cfg.Follower {
		//get the tag for this listener
		tag, err := igst.GetTag(val.Tag_Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to resolve tag \"%s\" for %s: %v\n", val.Tag_Name, k, err)
			return
		}
		//create our handler for this watcher
		lh, err := filewatch.NewLogHandler(tag, val.Ignore_Timestamps, val.Assume_Local_Timezone, ch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate handler: %v", err)
			return
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
			fmt.Fprintf(os.Stderr, "Failed to add watch directory for %s (%s): %v\n",
				val.Base_Directory, val.File_Filter, err)
			wtcher.Close()
			return
		}

	}

	if err := wtcher.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start file watcher: %v\n", err)
		wtcher.Close()
		igst.Close()
		return
	}

	debugout("Started following %d locations\n", len(cfg.Follower))
	//fire off our relay
	doneChan := make(chan error, 1)
	go relay(ch, doneChan, igst)

	debugout("Running\n")

	//listen for signals so we can close gracefully
	sch := make(chan os.Signal, 1)
	signal.Notify(sch, os.Interrupt)
	<-sch
	debugout("Attempting to close the watcher... ")
	if err := wtcher.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close file follower: %v\n", err)
	}
	debugout("Done\n")
	close(ch) //to inform the relay that no new entries are going to come down the pipe

	//wait for our ingest relay to exit
	<-doneChan
	if err := igst.Sync(time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sync: %v\n", err)
	}
	igst.Close()
}

func relay(ch chan *entry.Entry, done chan error, igst *ingest.IngestMuxer) {
	for e := range ch {
		if err := igst.WriteEntry(e); err != nil {
			fmt.Println("Failed to write entry", err)
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
