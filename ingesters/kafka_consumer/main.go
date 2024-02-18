/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingest/processors/tags"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/kafka.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/kafka.conf.d`
	ingesterName          = `kafka_consumer`
	appName               = `kafka`
	batchSize             = 512
	maxDataSize       int = 8 * 1024 * 1024
	initDataSize      int = 512 * 1024
)

var (
	debugOn bool
	lg      *log.Logger
)

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

	//fire up our consumers
	for k, v := range cfg.Consumers {
		kcfg := kafkaConsumerConfig{
			consumerCfg: *v,
			igst:        igst,
			lg:          lg,
		}
		v.TaggerConfig.Tags = append(v.TaggerConfig.Tags, v.defTag)
		if kcfg.tgr, err = tags.NewTagger(v.TaggerConfig, igst); err != nil {
			lg.Fatal("failed to establish a new tagger", log.KVErr(err))
		}
		if kcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.preprocessor); err != nil {
			lg.Fatal("preprocessor construction error", log.KVErr(err))
		}
		procs = append(procs, kcfg.pproc)
		kc, err := newKafkaConsumer(kcfg)
		if err != nil {
			lg.Error("failed to build kafka consumer", log.KV("consumer", k), log.KVErr(err))
			if err = clsrs.Close(); err != nil {
				lg.Error("failed to close all consumers", log.KVErr(err))
			}
			return
		}
		wg := clsrs.add(kc)
		if err = kc.Start(wg); err != nil {
			lg.Error("failed to start kafka consumer", log.KV("consumer", k), log.KVErr(err))
			if err = clsrs.Close(); err != nil {
				lg.Error("failed to close all consumers", log.KVErr(err))
			}
			return
		}
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	//close down our consumers
	if err := clsrs.Close(); err != nil {
		lg.Error("failed to close all consumers", log.KVErr(err))
	}

	//close down all the preprocessors
	for _, v := range procs {
		if v != nil {
			if err := v.Close(); err != nil {
				lg.Error("failed to close processors", log.KVErr(err))
			}
		}
	}

	lg.Info("kafka_consumer ingester exiting", log.KV("ingesteruuid", id))
	if err := igst.Sync(time.Second); err != nil {
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
