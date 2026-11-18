/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"net"
	"os"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/mqtt.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/mqtt.conf.d`
	ingesterName      = `mqtt_ingester`
	appName           = `mqtt`
)

var (
	debugOn bool
	lg      *log.Logger
)

func main() {
	go debug.HandleDebugSignals(ingesterName)
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
		os.Exit(1)
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		os.Exit(1)
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

	clsrs := newClosers()
	var procs []*processors.ProcessorSet

	for k, v := range cfg.Subscriptions {
		sc := subscriberConfig{
			cfg:  v,
			igst: igst,
			lg:   lg,
			src:  resolveSource(v, cfg, igst),
		}
		if v.clientID == `` {
			v.clientID = defaultClientID(id.String(), k)
		}
		if sc.defTag, err = igst.GetTag(v.defTag); err != nil {
			lg.Fatal("failed to resolve default tag",
				log.KV("subscription", k), log.KV("tag", v.defTag), log.KVErr(err))
		}
		if sc.routes, err = buildRoutes(v, igst); err != nil {
			lg.Fatal("failed to resolve Topic-Route tags",
				log.KV("subscription", k), log.KVErr(err))
		}
		if sc.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.preprocessor); err != nil {
			lg.Fatal("preprocessor construction error",
				log.KV("subscription", k), log.KVErr(err))
		}
		procs = append(procs, sc.proc)

		sub, err := newSubscriber(sc)
		if err != nil {
			lg.Error("failed to build mqtt subscriber",
				log.KV("subscription", k), log.KVErr(err))
			closeAll(clsrs, procs)
			return
		}
		wg := clsrs.add(sub)
		if err = sub.Start(wg); err != nil {
			lg.Error("failed to start mqtt subscriber",
				log.KV("subscription", k), log.KVErr(err))
			closeAll(clsrs, procs)
			return
		}
		debugout("Started subscription %s against %d broker(s) on %d topic(s)\n",
			k, len(v.brokers), len(v.topics))
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	//close down our subscribers, this waits for every goroutine to exit
	if err := clsrs.Close(); err != nil {
		lg.Error("failed to close all subscribers", log.KVErr(err))
	}

	//close down all the preprocessors, this flushes anything they have buffered
	for _, v := range procs {
		if v != nil {
			if err := v.Close(); err != nil {
				lg.Error("failed to close processors", log.KVErr(err))
			}
		}
	}

	lg.Info("mqtt ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

// buildRoutes negotiates the tag for each configured Topic-Route up front, so
// the hot path is a glob match and nothing else.
func buildRoutes(v *subCfg, igst *ingest.IngestMuxer) (rts []runtimeRoute, err error) {
	for _, r := range v.routes {
		rt := runtimeRoute{pattern: r.pattern, glob: r.glob, drop: r.drop}
		if !r.drop {
			if rt.tag, err = igst.GetTag(r.tag); err != nil {
				return nil, fmt.Errorf("route %q tag %q: %w", r.pattern, r.tag, err)
			}
		}
		rts = append(rts, rt)
	}
	return rts, nil
}

// resolveSource picks the SRC for every entry on a subscription: the
// per-subscription override, then the global override, then the muxer's view
// of our own address.
func resolveSource(v *subCfg, cfg *cfgType, igst *ingest.IngestMuxer) net.IP {
	if v.srcOverride != nil {
		return v.srcOverride
	}
	if cfg.Source_Override != `` {
		if ip := net.ParseIP(cfg.Source_Override); ip != nil {
			return ip
		}
	}
	if src, err := igst.SourceIP(); err == nil {
		return src
	}
	return nil
}

// defaultClientID builds a stable client ID. It has to be stable across
// restarts or the broker will not hand back the durable session and queued
// QoS 1/2 messages are lost.
func defaultClientID(uuid, name string) string {
	if len(uuid) > 8 {
		uuid = uuid[:8]
	}
	return `gravwell-mqtt-` + uuid + `-` + SanitizeClientName(name)
}

func closeAll(clsrs *closers, procs []*processors.ProcessorSet) {
	if err := clsrs.Close(); err != nil {
		lg.Error("failed to close all subscribers", log.KVErr(err))
	}
	for _, v := range procs {
		if v != nil {
			if err := v.Close(); err != nil {
				lg.Error("failed to close processors", log.KVErr(err))
			}
		}
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
