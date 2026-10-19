/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// macOS unified log ingester. Runs `log stream` for each configured stream and
// ingests every event it emits as a single-line JSON entry.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/macos_unified_log.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/macos_unified_log.conf.d`
	ingesterName      = `macOS Unified Log`
	appName           = `macosunifiedlog`
)

// wgWaitTimeout bounds how long runStreams waits for every streamer goroutine to
// exit after ctx is canceled, so a preprocessor that ignores cancellation can't
// hang shutdown forever. It's a var, rather than a const, so tests can shorten it.
var wgWaitTimeout = 30 * time.Second

// buildProcessorSet constructs the ProcessorSet for a stream's preprocessor list.
// It's a package-level var, like logCommand, so tests can substitute a stub
// without needing a real preprocessor configuration.
var buildProcessorSet = func(pc processors.ProcessorConfig, igst *ingest.IngestMuxer, names []string) (processorCloser, error) {
	return pc.ProcessorSet(igst, names)
}

func main() {
	go debug.HandleDebugSignals(appName)

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
	lg := ib.Logger

	// configuration validation works anywhere, but actually streaming requires macOS
	if runtime.GOOS != `darwin` {
		lg.FatalCode(0, "the macOS unified log ingester can only run on macOS", log.KV("os", runtime.GOOS))
	} else if _, err = os.Stat(logCommand); err != nil {
		lg.FatalCode(0, "unified logging command is unavailable", log.KV("path", logCommand), log.KVErr(err))
	}

	var hostname string
	if !cfg.Global.Disable_Hostname_Attach {
		var err error
		if hostname, err = os.Hostname(); err != nil {
			lg.Warn("failed to resolve hostname, entries will not include a host field", log.KVErr(err))
		}
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		lg.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	ib.AnnounceStartup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// listen for signals so we can close gracefully
		utils.WaitForQuit()
		cancel()
	}()

	if err := runStreams(ctx, cfg, igst, hostname, lg); err != nil {
		lg.FatalCode(0, "failed to start streams", log.KVErr(err))
	}

	if err := igst.Sync(utils.ExitSyncTimeout); err != nil {
		lg.Error("failed to sync", log.KVErr(err))
	}
	ib.AnnounceShutdown()
	if err := igst.Close(); err != nil {
		lg.Error("failed to close", log.KVErr(err))
	}
}

// runStreams resolves a tag and builds a ProcessorSet for every configured stream
// before starting any of them, so a tag or preprocessor error in one stream can
// never leave an already-started stream's `log` process running unsupervised: if
// any stream fails to build, no process has been started yet and runStreams
// returns the error without starting anything.
//
// Once every stream is known to be startable, runStreams starts them all and
// blocks until ctx is canceled, then waits (with a timeout, in case a
// preprocessor ignores cancellation) for every streamer to stop before closing
// their preprocessors. If the timeout fires, a streamer goroutine is presumably
// still inside a preprocessor call, so the processor sets are left unclosed
// rather than racing that call with a concurrent Close.
func runStreams(ctx context.Context, cfg *cfgType, igst *ingest.IngestMuxer, hostname string, lg *log.Logger) error {
	var globalSrc net.IP
	if cfg.Global.Source_Override != `` {
		// already validated by the config Verify
		globalSrc = net.ParseIP(cfg.Global.Source_Override)
	}

	type streamSetup struct {
		name string
		sc   *streamConfig
		tag  entry.EntryTag
		proc processorCloser
	}
	var setups []streamSetup
	closeSetups := func() {
		for _, s := range setups {
			if err := s.proc.Close(); err != nil {
				lg.Error("failed to close preprocessors", log.KV("stream", s.name), log.KVErr(err))
			}
		}
	}
	for name, sc := range cfg.Stream {
		tag, err := igst.GetTag(sc.Tag_Name)
		if err != nil {
			closeSetups()
			return fmt.Errorf("stream %q: failed to resolve tag %q: %w", name, sc.Tag_Name, err)
		}
		proc, err := buildProcessorSet(cfg.Preprocessor, igst, sc.Preprocessor)
		if err != nil {
			// ProcessorSet can return a non-nil, partially-built set alongside
			// its error (e.g. the second of two preprocessors failed), so close
			// whatever it did manage to build rather than leaking it.
			if proc != nil {
				if cerr := proc.Close(); cerr != nil {
					lg.Error("failed to close preprocessors", log.KV("stream", name), log.KVErr(cerr))
				}
			}
			closeSetups()
			return fmt.Errorf("stream %q: preprocessor construction error: %w", name, err)
		}
		setups = append(setups, streamSetup{name: name, sc: sc, tag: tag, proc: proc})
	}

	var wg sync.WaitGroup
	for _, s := range setups {
		streamer := newStreamer(s.name, s.sc, s.tag, globalSrc, hostname, s.proc, lg)
		wg.Go(func() { streamer.run(ctx) })
	}

	<-ctx.Done()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// every streamer goroutine has returned, so nothing is still inside
		// ProcessContext and it's safe to flush and close the processor sets
		closeSetups()
	case <-time.After(wgWaitTimeout):
		// a streamer goroutine is still live, almost certainly stuck inside a
		// preprocessor's ProcessContext despite ctx being canceled. ProcessorSet
		// locks that call, so closing the processor sets now would race it.
		// Skipping the flush is strictly better than corrupting a processor's
		// state or panicking on the way out.
		lg.Error("timed out waiting for streamers to stop", log.KV("timeout", wgWaitTimeout))
	}

	return nil
}
