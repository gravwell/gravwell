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

	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/timegrinder"
	"github.com/gravwell/o365"
)

const (
	defaultConfigLoc         = `/opt/gravwell/etc/o365_ingest.conf`
	defaultConfigDLoc        = `/opt/gravwell/etc/o365_ingest.conf.d`
	appName           string = `o365`
	ingesterName      string = "Office 365"
)

var (
	lg      *log.Logger
	debugOn bool
	tracker *stateTracker

	ErrInvalidStateFile = errors.New("State file exists and is not a regular file")
	ErrFailedSeek       = errors.New("Failed to seek to the start of the states file")
)

type event struct {
	Id string
}

func main() {
	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 ingesterName,
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

	// get the src we'll attach to entries
	var src net.IP
	if cfg.Global.Source_Override != `` {
		// global override
		src = net.ParseIP(cfg.Global.Source_Override)
		if src == nil {
			lg.FatalCode(0, "Global Source-Override is invalid")
		}
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()
	debugout("Successfully connected to ingesters\n")

	tracker, err = NewTracker(cfg.Global.State_Store_Location, 48*time.Hour, igst)
	if err != nil {
		lg.Fatal("failed to initialize state file", log.KVErr(err))
	}
	tracker.Start()

	var wg sync.WaitGroup

	// Instantiate the client
	ocfg := o365.DefaultConfig
	ocfg.ClientID = cfg.Global.Client_ID
	ocfg.ClientSecret = cfg.Global.Client_Secret
	ocfg.DirectoryID = cfg.Global.Directory_ID
	ocfg.TenantDomain = cfg.Global.Tenant_Domain
	ocfg.ContentTypes = cfg.ContentTypes()
	ocfg.PlanName = cfg.Global.Plan_Name
	o, err := o365.New(ocfg)
	if err != nil {
		lg.FatalCode(0, "Failed to get new client", log.KVErr(err))
	}

	// Make sure there are subscriptions for all our requested content types
	err = o.EnableSubscriptions()
	if err != nil {
		lg.FatalCode(0, "Failed to enable subscriptions", log.KVErr(err))
	}

	ctx, cancel := context.WithCancel(context.Background())

	// For each content type we're interested in, launch a
	// goroutine to read entries from Office 365 maintenance API
	running := true
	for k, v := range cfg.ContentType {
		//get timegrinder stood up
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
		}
		tgr, err := timegrinder.NewTimeGrinder(tcfg)
		if err != nil {
			lg.FatalCode(0, "failed to create timegrinder", log.KVErr(err))
			return
		} else if err := cfg.TimeFormat.LoadFormats(tgr); err != nil {
			lg.FatalCode(0, "failed to set load custom time formats", log.KVErr(err))
			return
		}
		if v.Assume_Local_Timezone {
			tgr.SetLocalTime()
		}
		if v.Timezone_Override != `` {
			if err = tgr.SetTimezone(v.Timezone_Override); err != nil {
				lg.FatalCode(0, "failed to set timezone", log.KV("timezone", v.Timezone_Override), log.KVErr(err))
				return
			}
		}

		go func(name string, ct contentType, tg *timegrinder.TimeGrinder) {
			debugout("Started reader for content type %v\n", ct.Content_Type)
			wg.Add(1)
			defer wg.Done()

			// figure out which tag we're using
			tag, err := igst.GetTag(ct.Tag_Name)
			if err != nil {
				lg.Fatal("failed to resolve tag", log.KV("tag", ct.Tag_Name), log.KV("handler", name), log.KVErr(err))
			}

			procset, err := cfg.Preprocessor.ProcessorSet(igst, ct.Preprocessor)
			if err != nil {
				lg.Fatal("preprocessor failure", log.KVErr(err))
			}

			// we'll do a sliding window, they warn it can take a long time for some logs to show up
			for running {
				end := time.Now()
				start := end.Add(-24 * time.Hour)

				content, err := o.ListAvailableContent(ct.Content_Type, start, end)
				if err != nil {
					lg.Error("failed to list content type", log.KV("contenttype", ct.Content_Type), log.KVErr(err))
					time.Sleep(10 * time.Second)
					continue
				}

				var uri, contentId string
				var ok bool
				var ent *entry.Entry
				var events []json.RawMessage
				var eventUnpacked event
				for _, item := range content {
					contentId, ok = item["contentId"]
					if !ok {
						continue
					}

					// CHECK IF ALREADY SEEN
					if tracker.IdExists(contentId) {
						continue
					}
					debugout("extracting %v\n", contentId)

					uri, ok = item["contentUri"]
					if !ok {
						continue
					}
					result, err := o.GetContent(uri)
					if err != nil {
						continue
					}

					// Dumb fact: each item may have multiple events
					err = json.Unmarshal(result, &events)
					if err != nil {
						continue
					}
					for _, evt := range events {
						err = json.Unmarshal(evt, &eventUnpacked)
						if err != nil {
							continue
						}
						if tracker.IdExists(eventUnpacked.Id) {
							continue
						}
						ent = &entry.Entry{
							Data: []byte(evt),
							Tag:  tag,
							SRC:  src,
						}
						if ct.Ignore_Timestamps {
							ent.TS = entry.Now()
						} else {
							ts, ok, err := tg.Extract(ent.Data)
							if !ok || err != nil {
								// something went wrong, switch to using the current time
								ct.Ignore_Timestamps = true
								ent.TS = entry.Now()
							} else {
								ent.TS = entry.FromStandard(ts)
							}
						}

						// now write the entry
						if err := procset.ProcessContext(ent, ctx); err != nil {
							lg.Warn("failed to handle entry", log.KVErr(err))
						}
						// Add the Id to the temporary map
						tracker.RecordId(eventUnpacked.Id, time.Now())
					}
					// Add the contentId to the temporary map
					tracker.RecordId(contentId, time.Now())

				}
				time.Sleep(5 * time.Second)
			}
			if err := procset.Close(); err != nil {
				lg.Error("failed to close processor set", log.KVErr(err))
			}

		}(k, *v, tgr)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	running = false
	wg.Wait()

	// Write the final state info
	tracker.Close()
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
