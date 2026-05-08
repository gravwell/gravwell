/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"
	jsonserialization "github.com/microsoft/kiota-serialization-json-go"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/msgraph_ingest.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/msgraph_ingest.conf.d`
	appName           = `msgraph`
)

var (
	debugOn bool

	ErrInvalidStateFile = errors.New("state file exists and is not a regular file")
	ErrFailedSeek       = errors.New("failed to seek to the start of the states file")
)

type stateTrackable interface {
	IdExists(id string) bool
	RecordId(id string, t time.Time) error
}

type entryProcessor interface {
	ProcessContext(ent *entry.Entry, ctx context.Context) error
	Close() error
}

func main() {
	go debug.HandleDebugSignals(appName)

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
	lg := ib.Logger

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer func() {
		if err := igst.Close(); err != nil {
			_ = ib.Logger.Error("error closing ingester", log.KVErr(err))
		}
	}()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

	tracker, err := NewTracker(cfg.Global.State_Store_Location, cfg.lookbackPeriod(), igst)
	if err != nil {
		lg.Fatal("failed to initialize state file", log.KVErr(err))
	}
	tracker.Start()

	// get the src we'll attach to entries
	var src net.IP
	if cfg.Global.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Global.Source_Override)
		if src == nil {
			lg.FatalCode(0, "Global Source-Override is invalid")
		}
	}

	var wg sync.WaitGroup

	// Instantiate the client
	graphClient, err := newGraphClient(msGraphConfig{
		clientID:     cfg.Global.Client_ID,
		clientSecret: cfg.Global.Client_Secret,
		tenantDomain: cfg.Global.Tenant_Domain,
	})
	if err != nil {
		lg.FatalCode(0, "failed to create graph client", log.KVErr(err))
	}

	ctx, cancel := context.WithCancel(context.Background())

	// For each content type we're interested in, launch a
	// goroutine to read entries from the Graph API
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
		var window timegrinder.TimestampWindow
		window, err = cfg.Global.GlobalTimestampWindow()
		if err != nil {
			return
		}
		tcfg := timegrinder.Config{
			TSWindow:           window,
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
			cfg:         cfg,
			graphClient: graphClient,
			ctx:         ctx,
			tg:          tg,
			procset:     procset,
			tag:         tag,
			tracker:     tracker,
			src:         src,
			lg:          lg,
		}
		switch ct.Content_Type {
		case "alerts":
			wg.Go(func() {
				alertRoutine(rcfg)
			})
		case "secureScores":
			wg.Go(func() {
				secureScoreRoutine(rcfg)
			})
		case "controlProfiles":
			wg.Go(func() {
				secureScoreProfileRoutine(rcfg)
			})
		}
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	cancel()
	wg.Wait()

	// Write the final state info
	tracker.Close()
}

type routineCfg struct {
	name        string
	ct          *contentType
	igst        *ingest.IngestMuxer
	cfg         *cfgType
	graphClient msGraphFetcher
	ctx         context.Context
	tg          *timegrinder.TimeGrinder
	procset     entryProcessor
	tag         entry.EntryTag
	tracker     stateTrackable
	src         net.IP
	lg          *log.Logger
}

func alertRoutine(c routineCfg) {
	_ = c.lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	defer func() {
		if err := c.procset.Close(); err != nil {
			_ = c.lg.Error("failed to close processor set", log.KVErr(err))
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		debugout("Querying alerts\n")
		filter, err := c.cfg.alertFilter()
		if err != nil {
			_ = c.lg.Error("failed to build alerts filter", log.KVErr(err))
			select {
			case <-time.After(10 * time.Second):
			case <-c.ctx.Done():
			}
			continue
		}
		alerts, err := c.graphClient.ListAlerts(c.ctx, filter)
		if err != nil {
			_ = c.lg.Error("failed to list alerts", log.KVErr(err))
			select {
			case <-time.After(10 * time.Second):
			case <-c.ctx.Done():
			}
			continue
		}

		var ent *entry.Entry
		for _, item := range alerts {
			id := item.GetId()
			if id == nil {
				continue
			}

			if c.tracker.IdExists(*id) {
				debugout("skipping already-seen alert %v\n", *id)
				continue
			}

			debugout("extracting %v\n", *id)

			writer := jsonserialization.NewJsonSerializationWriter()
			if err := item.Serialize(writer); err != nil {
				_ = c.lg.Warn("failed to serialize alert", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				_ = c.lg.Warn("failed to get serialized alert content", log.KV("id", *id), log.KVErr(err))
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  c.src,
			}
			if c.ct.Ignore_Timestamps {
				ent.TS = entry.Now()
			} else {
				ts := item.GetCreatedDateTime()
				if ts != nil {
					ent.TS = entry.FromStandard(*ts)
				} else {
					ent.TS = entry.Now()
				}
			}

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = c.lg.Warn("failed to handle entry", log.KVErr(err))
			}

			if err := c.tracker.RecordId(*id, time.Now()); err != nil {
				_ = c.lg.Warn("failed to record alert", log.KV("id", *id), log.KVErr(err))
			}
		}

		select {
		case <-time.After(30 * time.Second):
		case <-c.ctx.Done():
		}
	}
}

func secureScoreRoutine(c routineCfg) {
	_ = c.lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	defer func() {
		if err := c.procset.Close(); err != nil {
			_ = c.lg.Error("failed to close processor set", log.KVErr(err))
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		debugout("Querying secure scores\n")
		scores, err := c.graphClient.ListSecureScores(c.ctx)
		if err != nil {
			_ = c.lg.Error("failed to list secure scores", log.KVErr(err))
			select {
			case <-time.After(10 * time.Second):
			case <-c.ctx.Done():
			}
			continue
		}

		var ent *entry.Entry
		for _, item := range scores {
			id := item.GetId()
			if id == nil {
				continue
			}

			if c.tracker.IdExists(*id) {
				debugout("skipping already-seen score %v\n", *id)
				continue
			}
			debugout("extracting %v\n", *id)

			writer := jsonserialization.NewJsonSerializationWriter()
			if err := item.Serialize(writer); err != nil {
				_ = c.lg.Warn("failed to serialize secure score", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				_ = c.lg.Warn("failed to get serialized secure score content", log.KV("id", *id), log.KVErr(err))
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  c.src,
			}

			if c.ct.Ignore_Timestamps {
				ent.TS = entry.Now()
			} else {
				ts := item.GetCreatedDateTime()
				if ts != nil {
					ent.TS = entry.FromStandard(*ts)
				} else {
					ent.TS = entry.Now()
				}
			}

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = c.lg.Warn("failed to handle secure score entry", log.KVErr(err))
			}

			if err := c.tracker.RecordId(*id, time.Now()); err != nil {
				_ = c.lg.Warn("failed to record secure score", log.KV("id", *id), log.KVErr(err))
			}
		}

		// Secure scores are created very infrequently, so we sleep for a long time.
		select {
		case <-time.After(300 * time.Second):
		case <-c.ctx.Done():
		}
	}
}

func secureScoreProfileRoutine(c routineCfg) {
	_ = c.lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	defer func() {
		if err := c.procset.Close(); err != nil {
			_ = c.lg.Error("failed to close processor set", log.KVErr(err))
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		debugout("Querying secure score profiles\n")
		profiles, err := c.graphClient.ListSecureScoreControlProfiles(c.ctx)
		if err != nil {
			_ = c.lg.Error("failed to list secure score profiles", log.KVErr(err))
			select {
			case <-time.After(10 * time.Second):
			case <-c.ctx.Done():
			}
			continue
		}

		var ent *entry.Entry
		for _, item := range profiles {
			id := item.GetId()
			if id == nil {
				continue
			}
			debugout("extracting %v\n", *id)

			writer := jsonserialization.NewJsonSerializationWriter()
			if err := item.Serialize(writer); err != nil {
				_ = c.lg.Warn("failed to serialize secure score profile", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				_ = c.lg.Warn("failed to get serialized secure score profile content", log.KV("id", *id), log.KVErr(err))
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  c.src,
			}

			// They don't change very often, so always make it now
			ent.TS = entry.Now()

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = c.lg.Warn("failed to handle entry", log.KVErr(err))
			}
		}

		// We poll the profiles every hour just so they exist in the system
		select {
		case <-time.After(time.Hour):
		case <-c.ctx.Done():
		}
	}
}

func debugout(format string, args ...any) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
