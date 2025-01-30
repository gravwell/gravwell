/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"os"
	"sync"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/collectd.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/collectd.conf.d`
	appName           = `collectd`
)

var (
	debugOn bool
	lg      *log.Logger
)

type instance struct {
	name string
	inst *collectdInstance
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
	lg = ib.Logger
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
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

	//get our collectors built up
	wg := &sync.WaitGroup{}
	ccBase := collConfig{
		wg:   wg,
		igst: igst,
	}

	var instances []instance

	for k, v := range cfg.Collector {
		cc := ccBase
		//resolve tags for each collector
		overrides, err := v.getOverrides()
		if err != nil {
			lg.Fatal("failed to get overrides", log.KV("collector", k), log.KVErr(err))
		}
		if cc.defTag, err = igst.GetTag(v.Tag_Name); err != nil {
			lg.Fatal("failed to resolve tag", log.KV("collector", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
		}

		if cc.srcOverride, err = v.srcOverride(); err != nil {
			lg.Fatal("invalid Source-Override", log.KV("collector", k), log.KV("sourceoverride", v.Source_Override), log.KVErr(err))
		}
		if cc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor error", log.KV("collector", k), log.KV("preprocessor", v.Preprocessor), log.KVErr(err))
		}

		cc.src = nil

		cc.overrides = map[string]entry.EntryTag{}
		for plugin, tagname := range overrides {
			tagid, err := igst.GetTag(tagname)
			if err != nil {
				lg.Fatal("failed to resolve tag", log.KV("collector", k), log.KV("tag", v.Tag_Name), log.KVErr(err))
			}
			cc.overrides[plugin] = tagid
		}

		//populate the creds and sec level for each collector
		cc.pl, cc.seclevel = v.creds()

		//build out UDP listeners and register them
		laddr, err := v.udpAddr()
		if err != nil {
			lg.Fatal("failed to resolve udp address", log.KV("collector", k), log.KVErr(err))
		}
		inst, err := newCollectdInstance(cc, laddr)
		if err != nil {
			lg.Fatal("failed to create a new collector", log.KV("collector", k), log.KVErr(err))
		}
		if err := inst.Start(); err != nil {
			lg.Fatal("failed to start collector", log.KV("collector", k), log.KVErr(err))
		}
		instances = append(instances, instance{name: k, inst: inst})
	}

	//listen for the stop signal so we can die gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	//ask that everything close
	for i := range instances {
		if err := instances[i].inst.Close(); err != nil {
			lg.Fatal("failed to close collector", log.KV("collector", instances[i].name), log.KVErr(err))
		}
	}

	lg.Info("collectd ingester exiting", log.KV("ingesteruuid", id))
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
