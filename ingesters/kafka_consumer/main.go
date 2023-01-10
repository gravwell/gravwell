/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingest/processors/tags"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
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
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	ver            = flag.Bool("version", false, "Print the version information and exit")

	v  bool
	lg *log.Logger
)

func handleFlags() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
	lg.SetAppname(appName)
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr", log.KVErr(err))
		} else {
			lg.AddWriter(os.NewFile(uintptr(oldstderr), "oldstderr"))
		}

		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			version.PrintVersion(fout)
			ingest.PrintVersion(fout)
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fout.Close()
				lg.Fatal("failed to dup2 stderr", log.KVErr(err))
			}
		}
	}

	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
}

func main() {
	debug.SetTraceback("all")
	handleFlags()

	cfg, err := GetConfig(*confLoc, *confdLoc)
	if err != nil {
		lg.FatalCode(0, "failed to get configuration", log.KVErr(err))
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Log_Level), log.KVErr(err))
			}
		}
	}

	ltags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
		return
	}
	debugout("Handling %d tags over %d targets\n", len(ltags), len(conns))

	//fire up the ingesters
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "could not read ingester UUID")
	}
	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration", log.KVErr(err))
		return
	}

	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               ltags,
		Auth:               cfg.Secret(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		RateLimitBps:       lmt,
		IngesterUUID:       id.String(),
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if cfg.SelfIngest() {
		lg.AddRelay(igst)
	}
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timeout waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
		return
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state message", log.KVErr(err))
	}

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
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
