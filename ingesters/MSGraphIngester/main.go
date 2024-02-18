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
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/open-networks/go-msgraph"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/msgraph_ingest.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/msgraph_ingest.conf.d`
	appName           = `msgraph`
)

var (
	lg      *log.Logger
	debugOn bool
	tracker *stateTracker
	running bool
	src     net.IP

	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
)

type event struct {
	Id string
}

func main() {
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

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

	tracker, err = NewTracker(cfg.Global.State_Store_Location, 48*time.Hour, igst)
	if err != nil {
		lg.Fatal("failed to initialize state file", log.KVErr(err))
	}
	tracker.Start()

	// get the src we'll attach to entries
	if cfg.Global.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Global.Source_Override)
		if src == nil {
			lg.FatalCode(0, "Global Source-Override is invalid")
		}
	}

	var wg sync.WaitGroup

	// Instantiate the client
	graphClient, err := msgraph.NewGraphClient(cfg.Global.Tenant_Domain, cfg.Global.Client_ID, cfg.Global.Client_Secret)
	if err != nil {
		lg.FatalCode(0, "Failed to get new client", log.KVErr(err))
	}

	ctx, cancel := context.WithCancel(context.Background())

	// For each content type we're interested in, launch a
	// goroutine to read entries from the Graph API
	running = true
	for k, ct := range cfg.ContentType {
		// figure out which tag we're using
		tag, err := igst.GetTag(ct.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("tag", ct.Tag_Name), log.KVErr(err))
		}

		procset, err := cfg.Preprocessor.ProcessorSet(igst, ct.Preprocessor)
		if err != nil {
			lg.Fatal("preprocessor failure", log.KVErr(err))
		}

		// set up time extraction rules
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		tg, err := timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			ct.Ignore_Timestamps = true
		} else if err = cfg.TimeFormat.LoadFormats(tg); err != nil {
			lg.FatalCode(0, "failed to set load custom time formats", log.KVErr(err))
			return
		}
		if ct.Assume_Local_Timezone {
			tg.SetLocalTime()
		}
		if ct.Timezone_Override != `` {
			if err = tg.SetTimezone(ct.Timezone_Override); err != nil {
				lg.FatalCode(0, "failed to set timezone", log.KV("timezone", ct.Timezone_Override), log.KVErr(err))
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
	ib.AnnounceShutdown()

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
	lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying alerts\n")
		alerts, err := c.graphClient.ListAlerts()
		if err != nil {
			lg.Error("failed to list alerts", log.KVErr(err))
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
				lg.Warn("failed to re-pack entry", log.KV("id", item.ID), log.KVErr(err))
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
				lg.Warn("failed to handle entry", log.KVErr(err))
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
		lg.Error("failed to close processor set", log.KVErr(err))
	}

}

func secureScoreRoutine(c routineCfg) {
	lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure scores\n")
		scores, err := c.graphClient.ListSecureScores()
		if err != nil {
			lg.Error("failed to list secure scores", log.KVErr(err))
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
				lg.Warn("failed to re-pack secure score entry", log.KV("id", item.ID), log.KVErr(err))
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
				lg.Warn("failed to handle entry", log.KVErr(err))
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
		lg.Error("failed to close processor set", log.KVErr(err))
	}

}

func secureScoreProfileRoutine(c routineCfg) {
	lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure score profiles\n")
		profiles, err := c.graphClient.ListSecureScoreControlProfiles()
		if err != nil {
			lg.Error("failed to list secure score profiles", log.KVErr(err))
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
				lg.Warn("failed to re-pack secure score profile", log.KV("id", item.ID), log.KVErr(err))
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
				lg.Warn("failed to handle entry", log.KVErr(err))
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
		lg.Error("failed to close processor set", log.KVErr(err))
	}

}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
