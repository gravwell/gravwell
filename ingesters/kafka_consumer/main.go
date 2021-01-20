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
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
)

const (
	defaultConfigLoc     = `/opt/gravwell/etc/kafka.conf`
	ingesterName         = `kafka_consumer`
	batchSize            = 512
	maxDataSize      int = 8 * 1024 * 1024
	initDataSize     int = 512 * 1024
)

var (
	cpuprofile     = flag.String("cpuprofile", "", "write cpu profile to file")
	confLoc        = flag.String("config-file", defaultConfigLoc, "Location for configuration file")
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
	if *stderrOverride != `` {
		if oldstderr, err := syscall.Dup(int(os.Stderr.Fd())); err != nil {
			lg.Fatal("Failed to dup stderr: %v\n", err)
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
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}

	v = *verbose
	validate.ValidateConfig(GetConfig, *confLoc)
}

func main() {
	handleFlags()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			lg.Fatal("Failed to open %s for profile file: %v\n", *cpuprofile, err)
		}
		defer f.Close()
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	cfg, err := GetConfig(*confLoc)
	if err != nil {
		lg.FatalCode(0, "Failed to get configuration: %v\n", err)
		return
	}

	if len(cfg.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "Failed to get tags from configuration: %v\n", err)
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "Failed to get backend targets from configuration: %v\n", err)
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "Failed to get rate limit from configuration: %v\n", err)
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(igCfg)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
		return
	}

	defer igst.Close()
	debugout("Started ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
		return
	}
	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
		return
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "Failed to set configuration for ingester state messages\n")
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
		if kcfg.tgr, err = newTagger(v.taggerConfig, v.defTag, igst); err != nil {
			lg.Fatal("Failed to establish a new tagger: %v", err)
		}
		if kcfg.pproc, err = cfg.Preprocessor.ProcessorSet(igst, v.preprocessor); err != nil {
			lg.Fatal("Preprocessor construction error: %v", err)
		}
		procs = append(procs, kcfg.pproc)
		kc, err := newKafkaConsumer(kcfg)
		if err != nil {
			lg.Error("Failed to build kafka consumer %s: %v\n", k, err)
			if err = clsrs.Close(); err != nil {
				lg.Error("Failed to close all consumers: %v\n", err)
			}
			return
		}
		wg := clsrs.add(kc)
		if err = kc.Start(wg); err != nil {
			lg.Error("Failed to start kafka consumer %s: %v\n", k, err)
			if err = clsrs.Close(); err != nil {
				lg.Error("Failed to close all consumers: %v\n", err)
			}
			return
		}
	}

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	//close down our consumers
	if err := clsrs.Close(); err != nil {
		lg.Error("Failed to close all consumers: %v\n", err)
	}

	//close down all the preprocessors
	for _, v := range procs {
		if v != nil {
			if err := v.Close(); err != nil {
				lg.Error("Failed to close processors: %v\n", err)
			}
		}
	}

	//sync our data and close the ingester
	if err := igst.Sync(time.Second); err != nil {
		lg.Error("Failed to sync: %v\n", err)
	}
	if err := igst.Close(); err != nil {
		lg.Error("Failed to close: %v\n", err)
	}
}

func debugout(format string, args ...interface{}) {
	if !v {
		return
	}
	fmt.Printf(format, args...)
}
