/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
	"github.com/open-networks/go-msgraph"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/msgraph_ingest.conf`
)

var (
	configLoc      = flag.String("config-file", defaultConfigLoc, "Location of configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	lg             *log.Logger
	tracker        *stateTracker
	running        bool
	src            net.IP

	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
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
}

type event struct {
	Id string
}

func main() {
	cfg, err := GetConfig(*configLoc)
	if err != nil {
		lg.Fatal("Failed to get configuration: %v", err)
	}
	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Global.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Global.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("Failed to get tags from configuration: %v", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %s", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID\n")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		Logger:             lg,
		IngesterName:       "Microsoft Graph",
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Global.Label,
		RateLimitBps:       lmt,
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v", err)
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.FatalCode(0, "Failed start our ingest system: %v", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "Failed to set configuration for ingester state messages\n")
	}

	tracker, err = NewTracker(cfg.Global.State_Store_Location, 48*time.Hour, igst)
	if err != nil {
		lg.Fatal("Failed to initialize state file: %v", err)
	}
	tracker.Start()

	// get the src we'll attach to entries
	if cfg.Global.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Global.Source_Override)
		if src == nil {
			lg.Fatal("Global Source-Override is invalid")
		}
	}

	var wg sync.WaitGroup

	// Instantiate the client
	graphClient, err := msgraph.NewGraphClient(cfg.Global.Tenant_Domain, cfg.Global.Client_ID, cfg.Global.Client_Secret)
	if err != nil {
		lg.Fatal("Failed to get new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// For each content type we're interested in, launch a
	// goroutine to read entries from the Graph API
	running = true
	for k, ct := range cfg.ContentType {
		// figure out which tag we're using
		tag, err := igst.GetTag(ct.Tag_Name)
		if err != nil {
			lg.Fatal("Can't resolve tag %v: %v", ct.Tag_Name, err)
		}

		procset, err := cfg.Preprocessor.ProcessorSet(igst, ct.Preprocessor)
		if err != nil {
			lg.Fatal("Preprocessor construction error: %v", err)
		}

		// set up time extraction rules
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		tg, err := timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			ct.Ignore_Timestamps = true
		}
		if ct.Assume_Local_Timezone {
			tg.SetLocalTime()
		}
		if ct.Timezone_Override != `` {
			err = tg.SetTimezone(ct.Timezone_Override)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to set timezone to %v: %v\n", ct.Timezone_Override, err)
				return
			}
		}
		// build the config
		rcfg := routineCfg{
			name:        k,
			ct:          ct,
			igst:        igst,
			wg:          &wg,
			cfg:         cfg,
			graphClient: graphClient,
			ctx:         ctx,
			tg:          tg,
			procset:     procset,
			tag:         tag,
		}
		switch ct.Content_Type {
		case "alerts":
			go alertRoutine(rcfg)
		case "secureScores":
			go secureScoreRoutine(rcfg)
		case "controlProfiles":
			go secureScoreProfileRoutine(rcfg)
		}
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()

	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	running = false
	wg.Wait()

	// Write the final state info
	tracker.Close()
}

type routineCfg struct {
	name        string
	ct          *contentType
	igst        *ingest.IngestMuxer
	wg          *sync.WaitGroup
	cfg         *cfgType
	graphClient *msgraph.GraphClient
	ctx         context.Context
	tg          *timegrinder.TimeGrinder
	procset     *processors.ProcessorSet
	tag         entry.EntryTag
}

func alertRoutine(c routineCfg) {
	lg.Info("Started reader for content type %v\n", c.ct.Content_Type)
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying alerts\n")
		alerts, err := c.graphClient.ListAlerts()
		if err != nil {
			lg.Error("Failed to list alerts: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var ent *entry.Entry
		// Attempt to ingest each alert
		for _, item := range alerts {
			// CHECK IF ALREADY SEEN
			if tracker.IdExists(item.ID) {
				debugout("skipping already-seen alert %v\n", item.ID)
				continue
			}
			debugout("extracting %v\n", item.ID)

			// Now re-pack this as json
			packed, err := json.Marshal(item)
			if err != nil {
				lg.Warn("Failed to re-pack entry %v: %v", item.ID, err)
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  src,
			}
			if c.ct.Ignore_Timestamps {
				ent.TS = entry.Now()
			} else {
				ent.TS = entry.FromStandard(item.CreatedDateTime)
			}
			// now write the entry
			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				lg.Warn("Failed to handle entry: %v", err)
			}
			// Mark down this alert as ingested
			tracker.RecordId(item.ID, time.Now())

		}
		// Here's how we shut down quickly
		for i := 0; i < 30; i++ {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		lg.Error("Failed to close processor set: %v", err)
	}

}

func secureScoreRoutine(c routineCfg) {
	lg.Info("Started reader for content type %v\n", c.ct.Content_Type)
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure scores\n")
		scores, err := c.graphClient.ListSecureScores()
		if err != nil {
			lg.Error("Failed to list secure scores: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var ent *entry.Entry
		// Attempt to ingest each score
		for _, item := range scores {
			// CHECK IF ALREADY SEEN
			if tracker.IdExists(item.ID) {
				debugout("skipping already-seen score %v\n", item.ID)
				continue
			}
			debugout("extracting %v\n", item.ID)

			// Now re-pack this as json
			packed, err := json.Marshal(item)
			if err != nil {
				lg.Warn("Failed to re-pack secure score entry %v: %v", item.ID, err)
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  src,
			}
			if c.ct.Ignore_Timestamps {
				ent.TS = entry.Now()
			} else {
				ent.TS = entry.FromStandard(item.CreatedDateTime)
			}
			// now write the entry
			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				lg.Warn("Failed to handle entry: %v", err)
			}
			// Mark down this alert as ingested
			tracker.RecordId(item.ID, time.Now())

		}
		// Here's how we shut down quickly
		// Secure scores are created very infrequently, so we sleep for a long time.
		for i := 0; i < 300; i++ {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		lg.Error("Failed to close processor set: %v", err)
	}

}

func secureScoreProfileRoutine(c routineCfg) {
	lg.Info("Started reader for content type %v\n", c.ct.Content_Type)
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure score profiles\n")
		profiles, err := c.graphClient.ListSecureScoreControlProfiles()
		if err != nil {
			lg.Error("Failed to list secure score profiles: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var ent *entry.Entry
		// Attempt to ingest each profile
		for _, item := range profiles {
			debugout("extracting %v\n", item.ID)

			// Re-pack this as json
			packed, err := json.Marshal(item)
			if err != nil {
				lg.Warn("Failed to re-pack secure score profile %v: %v", item.ID, err)
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  src,
			}

			// They don't change very often, so always make it now
			ent.TS = entry.Now()

			// write the entry
			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				lg.Warn("Failed to handle entry: %v", err)
			}

		}
		// Here's how we shut down quickly
		// We poll the profiles every hour just so they exist in the system
		for i := 0; i < 3600; i++ {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		lg.Error("Failed to close processor set: %v", err)
	}

}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}
