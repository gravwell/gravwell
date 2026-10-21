/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// This is a simple hosted ingester tester for use in developing hosted ingesters.
// This test utility does not contain all the isolation and complete runtimes of the
// full hosted ingester system.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/hosted/plugins"
	"github.com/gravwell/gravwell/v4/hosted/storage"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/hosted_runner.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/hosted_runner.conf.d`
	ingesterName      = `hosted-runner`
	appName           = `hosted-runner`

	exitSyncTimeout = time.Minute
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
		return
	} else if err = ib.AssignConfig(&cfg); err != nil || cfg == nil {
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v\n", err)
		return
	}

	lg := ib.Logger
	guid, ok := cfg.IngesterUUID()
	if !ok {
		ib.Logger.FatalCode(0, "could not read ingester UUID")
	}

	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	var dyn dynamic.Manager
	if cfg.Dynamic.Enabled() {
		// ingest/config still hands back a github.com/google/uuid value.  Both types are
		// [16]byte, so this conversion is exact and checked at compile time, unlike a
		// round trip through a string.  It goes away when that package moves too.
		g := uuid.UUID(guid)
		if dyn, err = dynamic.NewDynamicConfigManager(ctx, cfg.Dynamic, g, ib.Logger); err != nil {
			ib.Logger.FatalCode(0, "failed to load dynamic manager", log.KVErr(err))
		}
	} else {
		dyn = dynamic.NewNil()
	}
	defer dyn.Close()

	// Declare what this build can run before loading what has been deployed to it.  Load
	// checks each dynamic configuration against the plugin that would have to run it, and
	// names the runner it is reporting on using the kinds registered here, so registering
	// first is what makes both of those work on the very first start.
	//
	// Fatal only when dynamic configuration is on.  With it off the registration lands in
	// the NopManager and nothing reads it, so a plugin whose config cannot be described
	// must not stop an ingester that never asked for it.
	if err = registerDynamicPluginTypes(dyn); err != nil {
		if cfg.Dynamic.Enabled() {
			ib.Logger.FatalCode(0, "failed to load dynamic plugin config types", log.KVErr(err))
		}
		ib.Logger.Error("failed to load dynamic plugin config types, dynamic configuration is disabled", log.KVErr(err))
	}

	// A dynamic configuration that will not load is skipped and reported upstream rather
	// than being fatal.  These arrive from a webserver, so one bad edit would otherwise
	// stop every ingester it reached from starting, and keep them stopped: the ingester
	// cannot get far enough to tell anyone why, and the file is still there on the next
	// boot.  A failure here is the storage directory itself being unusable, which is a
	// deployment problem that skipping a file does not fix.
	//
	// cfg, not &cfg.  cfg is already a *cfgType and the loader needs a pointer to the
	// struct, a pointer to the pointer is refused.
	if err = dyn.Load(cfg); err != nil {
		ib.Logger.FatalCode(0, "failed to load dynamic configurations", log.KVErr(err))
	}

	if err = dyn.Start(); err != nil {
		ib.Logger.FatalCode(0, "failed to start dynamic configuration client", log.KVErr(err))
	}

	// Check that we have configured ingesters.
	//
	// Running none is only an error when nothing can ever hand us one.  With dynamic
	// configuration enabled a webserver deploys runners at runtime, so a fresh install
	// legitimately starts with zero and picks them up on the first sync -- and exiting
	// here would mean the one deployment that most needs to wait for its configuration is
	// the only one that never lives long enough to receive it.  Everything below copes
	// with an empty set: createRunners builds nothing, startIngesters starts nothing, and
	// the reload path a dynamic update drives is what brings the first runner up.
	if c := cfg.IngesterCount(); c > 0 {
		ib.Logger.Info("starting", log.KV("hosted-count", c))
	} else if cfg.Dynamic.Enabled() {
		ib.Logger.Info("starting with no runners configured, waiting for dynamic configuration",
			log.KV("webservers", len(cfg.Dynamic.Webserver)))
	} else {
		ib.Logger.FatalCode(0, "no hosted ingesters configured")
		return
	}

	// get the state manager up and rolling
	sh, err := storage.OpenBoltHandler(cfg.State.Path, cfg.State.Sync)
	if err != nil {
		ib.Logger.FatalCode(0, "failed to open state handler", log.KVErr(err))
		return
	}

	// get the ingest connection
	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()

	ib.AnnounceStartup()
	lg.Info("Ingester running")

	rm, err := newRuntimeManager(igst, sh, lg)
	if err != nil {
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(0, "failed to create runtime manager", log.KVErr(err))
	}

	// Fire up native hosted ingesters first
	if err = rm.createRunners(cfg, ib); err != nil {
		rm.stop()  // best effort close
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(1, "failed to create ingesters", log.KVErr(err))
	}

	// ingesters exist, fire them up
	if err = rm.startIngesters(); err != nil {
		rm.stop()  // best effort close
		sh.Close() // ignore return, but no writes should have occurred
		ib.Logger.FatalCode(2, "failed to start ingesters", log.KVErr(err))
	}

	//listen for signals so we can close gracefully
	sig := utils.GetQuitChannel()
	hup := utils.GetSighupChannel()
	tckr := time.NewTicker(time.Minute)
	defer tckr.Stop()

	// reload rebuilds the configuration and hands it to the runtime manager.  A SIGHUP and
	// a dynamic config update are the same operation arriving from two different places, so
	// both drive this rather than each carrying their own copy of it.
	reload := func() {
		lg.Info("reloading configuration")
		var newCfg *cfgType
		var err error // deliberately shadows, a failed reload is not fatal to the process
		if err = ib.ReloadConfig(&newCfg); err != nil {
			lg.Error("failed to reload config", log.KVErr(err))
			return // abort the reload
		} else if newCfg == nil {
			lg.Error("config reload produced no configuration")
			return // abort the reload
		} else if err = dyn.Load(newCfg); err != nil {
			// newCfg, not &newCfg, the overlay loader needs a pointer to the struct
			lg.Error("failed to reload dynamic config", log.KVErr(err))
			return // abort the reload
		}

		if err = rm.reloadIngesters(newCfg); err != nil {
			//hand the config into the run manager and tell it to reload
			lg.Error("failed to reload ingesters", log.KVErr(err))
		} else {
			lg.Info("configuration reload complete")
		}
	}

exitLoop:
	for {
		select {
		case <-sig:
			lg.Info("ingester shutting down")
			break exitLoop
		case <-dyn.Signal():
			// a dynamic config update is treated the exact same as a SIGHUP
			reload()
		case <-hup:
			reload()
		case <-tckr.C:
			// go check on all ingesters and see if we should try to restart one that has died
			rm.startIngesters()
		}
	}

	if err = rm.stop(); err != nil {
		ib.Logger.Error("failed to close ingesters", log.KVErr(err))
	} else if err = sh.Close(); err != nil {
		ib.Logger.Error("failed to close state handler", log.KVErr(err))
	}

	// go shutdown everything
	ib.AnnounceShutdown()
	if err = igst.Sync(exitSyncTimeout); err != nil {
		ib.Logger.Error("failed to sync ingest muxer", log.KVErr(err))
	} else if err = igst.Close(); err != nil {
		ib.Logger.Error("failed to close ingest muxer", log.KVErr(err))
	}
}

func stackCloseErrors(curr, next error, name string, guid uuid.UUID) error {
	next = fmt.Errorf("failed to close %s (%v) %w", name, guid, next)
	if curr == nil {
		return next
	}
	return errors.Join(curr, next)
}

// registerDynamicPluginTypes registers all the dynamic plugin config types with the Dynamic Config manager
func registerDynamicPluginTypes(dm dynamic.Manager) (err error) {
	if dm == nil {
		return errors.New("nil dynamic config manager")
	}
	// the plugins package derives this from its own config set, a new plugin needs no
	// change here
	var kinds []plugins.PluginKind
	if kinds, err = plugins.Kinds(); err != nil {
		return fmt.Errorf("failed to enumerate dynamic config types %w", err)
	}
	for _, pk := range kinds {
		if err = dm.RegisterKind(pk.Kind, pk.Singleton, pk.Config); err != nil {
			return fmt.Errorf("failed to register dynamic config type %s %w", pk.Kind, err)
		}
	}
	return
}
