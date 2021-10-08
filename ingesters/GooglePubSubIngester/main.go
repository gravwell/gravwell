/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"net"
	"os"
	"path"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"

	"cloud.google.com/go/pubsub"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/pubsub_ingest.conf`
	appName          = `pubsub`
)

var (
	configLoc      = flag.String("config-file", defaultConfigLoc, "Location of configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	ver            = flag.Bool("version", false, "Print the version information and exit")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	lg             *log.Logger
)

func init() {
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
			lg.Fatal("failed to dup stderr", log.KVErr(err))
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
	validate.ValidateConfig(GetConfig, *configLoc)
}

func main() {
	debug.SetTraceback("all")
	cfg, err := GetConfig(*configLoc)
	if err != nil {
		lg.Fatal("failed to get configuration", log.KV("file", *configLoc), log.KVErr(err))
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("file", cfg.Global.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("log-level", cfg.Global.Log_Level), log.KVErr(err))
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("failed to get tags from configuration", log.KVErr(err))
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("failed to get backend targets from configuration", log.KVErr(err))
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	lmt, err := cfg.Global.RateLimit()
	if err != nil {
		lg.FatalCode(0, "failed to get rate limit from configuration", log.KVErr(err))
		return
	}
	debugout("Rate limiting connection to %d bps\n", lmt)

	//fire up the ingesters
	id, ok := cfg.Global.IngesterUUID()
	if !ok {
		lg.FatalCode(0, "could not read ingester UUID")
	}
	ingestConfig := ingest.UniformMuxerConfig{
		IngestStreamConfig: cfg.Global.IngestStreamConfig,
		Destinations:       conns,
		Tags:               tags,
		Auth:               cfg.Secret(),
		LogLevel:           cfg.LogLevel(),
		Logger:             lg,
		IngesterName:       "GooglePubSub",
		IngesterVersion:    version.GetVersion(),
		IngesterUUID:       id.String(),
		IngesterLabel:      cfg.Global.Label,
		RateLimitBps:       lmt,
		CacheDepth:         cfg.Global.Cache_Depth,
		CachePath:          cfg.Global.Ingest_Cache_Path,
		CacheSize:          cfg.Global.Max_Ingest_Cache,
		CacheMode:          cfg.Global.Cache_Mode,
		LogSourceOverride:  net.ParseIP(cfg.Global.Log_Source_Override),
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("failed build our ingest system", log.KVErr(err))
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("failed start our ingest system", log.KVErr(err))
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "timed out waiting for backend connections", log.KV("timeout", cfg.Timeout()), log.KVErr(err))
	}
	debugout("Successfully connected to ingesters\n")

	// prepare the configuration we're going to send upstream
	err = igst.SetRawConfiguration(cfg)
	if err != nil {
		lg.FatalCode(0, "failed to set configuration for ingester state messages", log.KVErr(err))
	}

	// Set up environment variables for AWS auth, if extant
	if cfg.Global.Google_Credentials_Path != "" {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.Global.Google_Credentials_Path)
	}

	// make a client
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, cfg.Global.Project_ID)
	if err != nil {
		lg.Fatal("failed to create pubsub client", log.KVErr(err))
		return
	}

	for _, psv := range cfg.PubSub {
		tagid, err := igst.GetTag(psv.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("tag", psv.Tag_Name), log.KVErr(err))
		}

		procset, err := cfg.Preprocessor.ProcessorSet(igst, psv.Preprocessor)
		if err != nil {
			lg.Fatal("preprocessor construction failed", log.KVErr(err))
		}

		// get the topic
		topic := client.Topic(psv.Topic_Name)
		ok, err := topic.Exists(ctx)
		if err != nil {
			lg.Fatal("error checking topic", log.KVErr(err))
		}
		if !ok {
			lg.Fatal("topic does not exist", log.KV("topic", psv.Topic_Name))
		}

		// Get the subscription, creating if needed
		subname := fmt.Sprintf("ingest_%s", psv.Topic_Name)
		sub := client.Subscription(subname)
		ok, err = sub.Exists(ctx)
		if err != nil {
			lg.Fatal("error checking subscription", log.KVErr(err))
		}
		if !ok {
			// doesn't exist, try creating it
			sub, err = client.CreateSubscription(ctx, subname, pubsub.SubscriptionConfig{
				Topic:       topic,
				AckDeadline: 10 * time.Second,
			})
			if err != nil {
				lg.Fatal("error creating subscription", log.KVErr(err))
			}
		}

		var count, size uint64
		var oldcount, oldsize uint64

		if *verbose {
			go func() {
				for {
					time.Sleep(1 * time.Second)
					tmpcount := count
					tmpsize := size
					cdiff := tmpcount - oldcount
					sdiff := tmpsize - oldsize
					oldcount = tmpcount
					oldsize = tmpsize
					lg.Info("ingest stats", log.KV("eps", cdiff), log.KV("bps", sdiff), log.KV("size", oldsize))
				}
			}()
		}

		go func(sub *pubsub.Subscription, tagid entry.EntryTag, ps *pubsubconf) {
			eChan := make(chan *entry.Entry, 2048)
			go func(c chan *entry.Entry) {
				for e := range c {
					if err := procset.Process(e); err != nil {
						lg.Error("failed to process entry", log.KVErr(err))
					}
					count++
				}
				if err := procset.Close(); err != nil {
					lg.Error("failed to close processor", log.KVErr(err))
				}
			}(eChan)
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
			}
			tg, err := timegrinder.NewTimeGrinder(tcfg)
			if err != nil {
				ps.Parse_Time = false
			}
			if ps.Assume_Local_Timezone {
				tg.SetLocalTime()
			}
			if ps.Timezone_Override != `` {
				err = tg.SetTimezone(ps.Timezone_Override)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to set timezone to %v: %v\n", ps.Timezone_Override, err)
					return
				}
			}

			var src net.IP
			if cfg.Global.Source_Override != `` {
				// global override
				src = net.ParseIP(cfg.Global.Source_Override)
				if src == nil {
					lg.Fatal("Global Source-Override is invalid", log.KV("override", cfg.Global.Source_Override))
				}
			}

			for {
				callback := func(ctx context.Context, msg *pubsub.Message) {
					ent := &entry.Entry{
						Data: msg.Data,
						Tag:  tagid,
						SRC:  src,
					}
					size += uint64(len(msg.Data))
					if ps.Parse_Time == false {
						ent.TS = entry.FromStandard(msg.PublishTime)
					} else {
						ts, ok, err := tg.Extract(msg.Data)
						if !ok || err != nil {
							// failed to extract, use the publishtime
							ps.Parse_Time = false
							ent.TS = entry.FromStandard(msg.PublishTime)
						} else {
							ent.TS = entry.FromStandard(ts)
						}
					}
					eChan <- ent
					msg.Ack()
				}
				cctx, cancel := context.WithCancel(ctx)
				defer cancel()
				if err := sub.Receive(cctx, callback); err != nil {
					lg.Error("receive failed", log.KVErr(err))
				}
			}
		}(sub, tagid, psv)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()
}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}
