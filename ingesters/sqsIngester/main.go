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
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const (
	defaultConfigLoc      = `/opt/gravwell/etc/sqs.conf`
	defaultConfigDLoc     = `/opt/gravwell/etc/sqs.conf.d`
	ingesterName          = `sqsIngester`
	appName               = `amazonsqs`
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

	v    bool
	lg   *log.Logger
	igst *ingest.IngestMuxer
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

func init() {
	flag.Parse()
	if *ver {
		version.PrintVersion(os.Stdout)
		ingest.PrintVersion(os.Stdout)
		os.Exit(0)
	}
	validate.ValidateConfig(GetConfig, *confLoc, *confdLoc)
	var fp string
	var err error
	if *stderrOverride != `` {
		fp = filepath.Join(`/dev/shm/`, *stderrOverride)
	}
	cb := func(w io.Writer) {
		version.PrintVersion(w)
		ingest.PrintVersion(w)
		log.PrintOSInfo(w)
	}
	if lg, err = log.NewStderrLoggerEx(fp, cb); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get stderr logger: %v\n", err)
		os.Exit(-1)
	}
	lg.SetAppname(appName)

	v = *verbose
}

func main() {
	debug.SetTraceback("all")
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

	tags, err := cfg.Tags()
	if err != nil {
		lg.FatalCode(0, "failed to get tags from configuration", log.KVErr(err))
		return
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.FatalCode(0, "failed to get backend targets from configuration", log.KVErr(err))
		return
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	debugout("INSECURE skip TLS certificate verification: %v\n", cfg.InsecureSkipTLSVerification())
	id, ok := cfg.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "could not read ingester UUID")
	}
	igCfg := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		VerifyCert:         !cfg.InsecureSkipTLSVerification(),
		IngesterName:       ingesterName,
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Label,
		RateLimitBps:       lmt,
		Logger:             lg,
		CacheDepth:         cfg.Cache_Depth,
		CachePath:          cfg.Ingest_Cache_Path,
		CacheSize:          cfg.Max_Ingest_Cache,
		CacheMode:          cfg.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Log_Source_Override),
	}
	igst, err = ingest.NewUniformMuxer(igCfg)
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
		lg.FatalCode(0, "failed to set configuration for ingester state messages")
	}

	var wg sync.WaitGroup
	done := make(chan bool)

	ctx, cancel := context.WithCancel(context.Background())

	// make sqs connections
	for k, v := range cfg.Queue {
		var src net.IP

		if v.Source_Override != `` {
			src = net.ParseIP(v.Source_Override)
			if src == nil {
				lg.FatalCode(0, "listener invalid source override, is not an IP address", log.KV("listener", k), log.KV("sourceoverride", v.Source_Override))
			}
		} else if cfg.Source_Override != `` {
			// global override
			src = net.ParseIP(cfg.Source_Override)
			if src == nil {
				lg.FatalCode(0, "Global Source-Override is invalid")
			}
		}

		//get the tag for this listener
		tag, err := igst.GetTag(v.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("tag", v.Tag_Name), log.KV("listener", k), log.KVErr(err))
		}

		hcfg := &handlerConfig{
			queue:            v.Queue_URL,
			region:           v.Region,
			akid:             v.AKID,
			secret:           v.Secret,
			tag:              tag,
			ignoreTimestamps: v.Ignore_Timestamps,
			setLocalTime:     v.Assume_Local_Timezone,
			timezoneOverride: v.Timezone_Override,
			formatOverride:   v.Timestamp_Format_Override,
			src:              src,
			wg:               &wg,
			done:             done,
			ctx:              ctx,
		}

		if hcfg.proc, err = cfg.Preprocessor.ProcessorSet(igst, v.Preprocessor); err != nil {
			lg.Fatal("preprocessor failure", log.KVErr(err))
		}

		wg.Add(1)
		go queueRunner(hcfg)
	}

	debugout("Running\n")

	//listen for signals so we can close gracefully
	utils.WaitForQuit()

	// stop outstanding writes in 1 second while we wait
	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	// wait for graceful shutdown
	close(done)
	wg.Wait()

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

func queueRunner(hcfg *handlerConfig) {
	defer hcfg.wg.Done()

	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(hcfg.region),
		Credentials: credentials.NewStaticCredentials(hcfg.akid, hcfg.secret, ""),
	}))

	svc := sqs.New(sess)

	c := make(chan *sqs.ReceiveMessageOutput)
	for {
		// aws uses string pointers, so we have to decalre it on the
		// stack in order to take it's reference... why aws, why......
		an := "SentTimestamp"
		req := &sqs.ReceiveMessageInput{
			AttributeNames: []*string{&an},
		}

		req = req.SetQueueUrl(hcfg.queue)
		err := req.Validate()
		if err != nil {
			lg.Error("sqs request validation failed", log.KVErr(err))
			return
		}

		var out *sqs.ReceiveMessageOutput
		go func() {
			o, err := svc.ReceiveMessage(req)
			if err != nil {
				lg.Error("sqs receive message error", log.KVErr(err))
				c <- nil
			}
			c <- o
		}()

		select {
		case out = <-c:
			if out == nil {
				return
			}
		case <-hcfg.done:
			return
		}

		// we may have multiple packed messages
		for _, v := range out.Messages {
			msg := []byte(*v.Body)

			var ts entry.Timestamp
			if !hcfg.ignoreTimestamps {
				// grab the timestamp from SQS
				t, mok := v.Attributes["SentTimestamp"]
				if !mok {
					lg.Error("SQS did not provide timestamp for message", log.KV("attributes", v.Attributes))
				} else {
					ut, err := strconv.ParseInt(*t, 10, 64)
					if err != nil {
						lg.Error("failed parseint on unix time", log.KV("value", *t), log.KVErr(err))
					} else {
						ts = entry.UnixTime(ut/1000, 0)
					}
				}
			} else {
				ts = entry.Now()
			}

			ent := &entry.Entry{
				SRC:  hcfg.src,
				TS:   ts,
				Tag:  hcfg.tag,
				Data: msg,
			}

			if err = hcfg.proc.ProcessContext(ent, hcfg.ctx); err != nil {
				lg.Error("failed to ingest entry", log.KVErr(err))
				return
			}
		}
	}
}
