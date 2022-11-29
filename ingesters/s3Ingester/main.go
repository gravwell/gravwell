/*************************************************************************
 * Copyright 2020 Gravwell, Inc. All rights reserved.
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
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/s3.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/s3.conf.d`
	defaultStateLoc       = `/opt/gravwell/etc/s3.state`
	ingesterName          = `S3 Ingester`
	appName               = `s3`
	batchSize             = 512
	maxDataSize       int = 8 * 1024 * 1024
	initDataSize      int = 512 * 1024
)

type handlerConfig struct {
	queue            string
	region           string
	akid             string
	secret           string
	tag              entry.EntryTag
	ignoreTimestamps bool
	setLocalTime     bool
	timezoneOverride string
	src              net.IP
	formatOverride   string
	wg               *sync.WaitGroup
	done             chan bool
	proc             *processors.ProcessorSet
	ctx              context.Context
}

func main() {
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
	} else if ib.Cfg == nil {
		fmt.Fprintf(os.Stderr, "configuration is nil\n")
		return
	}
	cfg, ok := ib.Cfg.(*cfgType)
	if !ok {
		fmt.Fprintf(os.Stderr, "ingester base returned invalid config type %T\n", ib.Cfg)
		return
	} else if cfg == nil {
		fmt.Fprintf(os.Stderr, "configuration is nil\n")
		return
	}

	//get our object tracker state rolling
	ot, err := NewObjectTracker(cfg.State_Store_Location)
	if err != nil {
		ib.Logger.FatalCode(0, "failed to create state tracker", log.KVErr(err))
		return
	}

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()

	//build up our list of bucket readers
	var brs []*BucketReader
	for k, v := range cfg.Bucket {
		bcfg := BucketConfig{
			AuthConfig:     v.AuthConfig,
			TimeConfig:     v.TimeConfig,
			Verbose:        ib.Verbose,
			Name:           k,
			Reader:         v.Reader,
			FileFilters:    v.File_Filters,
			TagName:        v.Tag_Name,
			SourceOverride: v.Source_Override,
			Logger:         ib.Logger,
			MaxLineSize:    v.Max_Line_Size,
		}
		if bcfg.Tag, err = igst.GetTag(v.Tag_Name); err != nil {
			ib.Logger.FatalCode(0, "failed to get established tag",
				log.KV("tag", v.Tag_Name),
				log.KV("bucket", k), log.KVErr(err))
		} else if bcfg.Proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			ib.Logger.FatalCode(0, "preprocessor failure",
				log.KV("bucket", k), log.KVErr(err))
		}
		if !bcfg.Ignore_Timestamps {
			if bcfg.TG, err = cfg.newTimeGrinder(v.TimeConfig); err != nil {
				ib.Logger.FatalCode(0, "failed to create timegrinder",
					log.KV("bucket", k), log.KVErr(err))
			}
		}

		if b, err := NewBucketReader(bcfg); err != nil {
			ib.Logger.FatalCode(0, "failed to create bucket reader", log.KVErr(err))
		} else {
			brs = append(brs, b)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	ib.Debug("Running\n")

	//kick off our actually consumer routine
	if err = start(&wg, ctx, brs, ot, ib.Logger); err != nil {
		ib.Logger.Error("failed to run bucket consumers", log.KVErr(err))
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	cancel()

	// wait for graceful shutdown
	wg.Wait()

	if err := igst.Sync(time.Second); err != nil {
		ib.Logger.Error("failed to sync", log.KVErr(err))
	}
	if err := igst.Close(); err != nil {
		ib.Logger.Error("failed to close", log.KVErr(err))
	}
	if err := ot.Flush(); err != nil {
		ib.Logger.Error("failed to flush state", log.KVErr(err))
	}
}
