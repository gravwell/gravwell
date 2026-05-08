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

	azidentity "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"
	jsonserialization "github.com/microsoft/kiota-serialization-json-go"
	msgraphsdkgo "github.com/microsoftgraph/msgraph-sdk-go"
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

	ErrInvalidStateFile = errors.New("state file exists and is not a regular file")
	ErrFailedSeek       = errors.New("failed to seek to the start of the states file")
)

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
	lg = ib.Logger

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
	cred, err := azidentity.NewClientSecretCredential(
		cfg.Global.Tenant_Domain,
		cfg.Global.Client_ID,
		cfg.Global.Client_Secret,
		nil,
	)
	if err != nil {
		lg.FatalCode(0, "failed to create credentials", log.KVErr(err))
	}
	graphClient, err := msgraphsdkgo.NewGraphServiceClientWithCredentials(
		cred,
		[]string{"https://graph.microsoft.com/.default"},
	)
	if err != nil {
		lg.FatalCode(0, "failed to create graph client", log.KVErr(err))
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
	graphClient *msgraphsdkgo.GraphServiceClient
	ctx         context.Context
	tg          *timegrinder.TimeGrinder
	procset     *processors.ProcessorSet
	tag         entry.EntryTag
}

func alertRoutine(c routineCfg) {
	_ = lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying alerts\n")
		resp, err := c.graphClient.Security().Alerts_v2().Get(c.ctx, nil)
		if err != nil {
			lg.Error("failed to list alerts", log.KVErr(err))
			time.Sleep(10 * time.Second)
			continue
		}

		alerts := resp.GetValue()
		for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
			resp, err = c.graphClient.Security().Alerts_v2().WithUrl(*resp.GetOdataNextLink()).Get(c.ctx, nil)
			if err != nil {
				lg.Error("failed to get next page of alerts", log.KVErr(err))
				break
			}
			alerts = append(alerts, resp.GetValue()...)
		}

		var ent *entry.Entry
		for _, item := range alerts {
			id := item.GetId()
			if id == nil {
				continue
			}

			if tracker.IdExists(*id) {
				debugout("skipping already-seen alert %v\n", *id)
				continue
			}

			debugout("extracting %v\n", *id)

			writer := jsonserialization.NewJsonSerializationWriter()
			if err := item.Serialize(writer); err != nil {
				_ = lg.Warn("failed to serialize alert", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				_ = lg.Warn("failed to get serialized alert content", log.KV("id", *id), log.KVErr(err))
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
				ts := item.GetCreatedDateTime()
				if ts != nil {
					ent.TS = entry.FromStandard(*ts)
				} else {
					ent.TS = entry.Now()
				}
			}

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = lg.Warn("failed to handle entry", log.KVErr(err))
			}

			if err := tracker.RecordId(*id, time.Now()); err != nil {
				_ = lg.Warn("failed to record alert", log.KV("id", *id), log.KVErr(err))
			}
		}

		for range 30 {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		_ = lg.Error("failed to close processor set", log.KVErr(err))
	}

}

func secureScoreRoutine(c routineCfg) {
	_ = lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure scores\n")
		resp, err := c.graphClient.Security().SecureScores().Get(c.ctx, nil)
		if err != nil {
			lg.Error("failed to list secure scores", log.KVErr(err))
			time.Sleep(10 * time.Second)
			continue
		}

		scores := resp.GetValue()
		for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
			resp, err = c.graphClient.Security().SecureScores().WithUrl(*resp.GetOdataNextLink()).Get(c.ctx, nil)
			if err != nil {
				lg.Error("failed to get next page of secure scores", log.KVErr(err))
				break
			}
			scores = append(scores, resp.GetValue()...)
		}

		var ent *entry.Entry
		for _, item := range scores {
			id := item.GetId()
			if id == nil {
				continue
			}

			if tracker.IdExists(*id) {
				debugout("skipping already-seen score %v\n", *id)
				continue
			}
			debugout("extracting %v\n", *id)

			writer := jsonserialization.NewJsonSerializationWriter()
			if err := item.Serialize(writer); err != nil {
				lg.Warn("failed to serialize secure score", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				lg.Warn("failed to get serialized secure score content", log.KV("id", *id), log.KVErr(err))
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
				ts := item.GetCreatedDateTime()
				if ts != nil {
					ent.TS = entry.FromStandard(*ts)
				} else {
					ent.TS = entry.Now()
				}
			}

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = lg.Warn("failed to handle secure score entry", log.KVErr(err))
			}

			if err := tracker.RecordId(*id, time.Now()); err != nil {
				_ = lg.Warn("failed to record secure score", log.KV("id", *id), log.KVErr(err))
			}
		}

		// Secure scores are created very infrequently, so we sleep for a long time.
		for range 300 {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		_ = lg.Error("failed to close processor set", log.KVErr(err))
	}
}

func secureScoreProfileRoutine(c routineCfg) {
	_ = lg.Info("started reader for content type", log.KV("contenttype", c.ct.Content_Type))
	c.wg.Add(1)
	defer c.wg.Done()

	for running {
		debugout("Querying secure score profiles\n")
		resp, err := c.graphClient.Security().SecureScoreControlProfiles().Get(c.ctx, nil)
		if err != nil {
			_ = lg.Error("failed to list secure score profiles", log.KVErr(err))
			time.Sleep(10 * time.Second)
			continue
		}

		profiles := resp.GetValue()
		for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
			resp, err = c.graphClient.Security().SecureScoreControlProfiles().WithUrl(*resp.GetOdataNextLink()).Get(c.ctx, nil)
			if err != nil {
				_ = lg.Error("failed to get next page of secure score profiles", log.KVErr(err))
				break
			}
			profiles = append(profiles, resp.GetValue()...)
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
				_ = lg.Warn("failed to serialize secure score profile", log.KV("id", *id), log.KVErr(err))
				continue
			}

			packed, err := writer.GetSerializedContent()
			if err != nil {
				_ = lg.Warn("failed to get serialized secure score profile contine", log.KV("id", *id), log.KVErr(err))
				continue
			}

			ent = &entry.Entry{
				Data: packed,
				Tag:  c.tag,
				SRC:  src,
			}

			// They don't change very often, so always make it now
			ent.TS = entry.Now()

			if err := c.procset.ProcessContext(ent, c.ctx); err != nil {
				_ = lg.Warn("failed to handle entry", log.KVErr(err))
			}
		}

		// We poll the profiles every hour just so they exist in the system
		for range 3600 {
			if !running {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err := c.procset.Close(); err != nil {
		_ = lg.Error("failed to close processor set", log.KVErr(err))
	}

}

func debugout(format string, args ...any) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
