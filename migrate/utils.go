/*************************************************************************
 * Copyright 2017 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	appName = `migrate`
)

func getIngestConnection(cfg *cfgType, lg *log.Logger) *ingest.IngestMuxer {
	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
	}

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
	}
	lg.Info("Rate limiting connection", log.KV("bps", lmt))

	//fire up the ingesters
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "Couldn't read ingester UUID")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		IngesterName:       appName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		igst.Close()
		lg.Fatal("failed start our ingest system", log.KVErr(err))
	}
	lg.Infof("Waiting for connections to ingesters")

	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		igst.Close()
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		igst.Close()
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

	if cfg.Source_Override != "" {
		// global override
		if net.ParseIP(cfg.Source_Override) == nil {
			igst.Close()
			lg.Fatal("Global Source-Override is invalid", log.KV("sourceoverride", cfg.Source_Override))
		}
	}
	return igst
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}

type statusUpdate struct {
	count uint64
	size  uint64
}

func statusEater(sc <-chan statusUpdate) {
	for range sc {
	}
}

func statusPrinter(sc <-chan statusUpdate) {
	var totalCount, totalBytes uint64
	var lastCount, lastBytes uint64
	tckr := time.NewTicker(3 * time.Second)
	defer tckr.Stop()
	defer fmt.Printf("\n\n")

	ts := time.Now()
	for {
		select {
		case <-tckr.C:
			dur := time.Since(ts)
			diffCount := totalCount - lastCount
			diffBytes := totalBytes - lastBytes

			fmt.Printf("\r%s %s   %s %s                 ",
				ingest.HumanEntryRate(diffCount, dur),
				ingest.HumanRate(diffBytes, dur),
				ingest.HumanSize(totalBytes),
				ingest.HumanCount(totalCount))
			ts = time.Now()
			lastCount = totalCount
			lastBytes = totalBytes
		case ud, ok := <-sc:
			totalCount += ud.count
			totalBytes += ud.size
			if !ok {
				return
			}
		}
	}
}

func checkSig(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
	}
	return false
}

type discard struct {
}

func (d *discard) Close() error                { return nil }
func (d *discard) Write(b []byte) (int, error) { return len(b), nil }
