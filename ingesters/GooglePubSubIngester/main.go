/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
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
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"cloud.google.com/go/pubsub"
	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/pubsub_ingest.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/pubsub_ingest.conf.d`
	appName           = `pubsub`
	ingesterName      = "GooglePubSub"
)

var (
	lg      *log.Logger
	debugOn bool
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
		fmt.Fprintf(os.Stderr, "failed to assign configuration %v %v\n", err, cfg == nil)
		return
	}
	debugOn = ib.Verbose
	lg = ib.Logger

	igst, err := ib.GetMuxer()
	if err != nil {
		ib.Logger.FatalCode(0, "failed to get ingest connection", log.KVErr(err))
		return
	}
	defer igst.Close()
	ib.AnnounceStartup()

	exitCtx, exitFn := context.WithCancel(context.Background())

	debugout("Started ingester muxer\n")

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

	var ok bool
	for _, psv := range cfg.PubSub {
		tagid, err := igst.GetTag(psv.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("tag", psv.Tag_Name), log.KVErr(err))
		}

		procset, err := cfg.Preprocessor.ProcessorSet(igst, psv.Preprocessor)
		if err != nil {
			lg.Fatal("preprocessor construction failed", log.KVErr(err))
		}

		// Get the subscription, creating if needed
		subname := psv.Subscription_Name
		if subname == `` {
			subname = fmt.Sprintf("ingest_%s", psv.Topic_Name)
		}
		sub := client.Subscription(subname)
		if ok, err = sub.Exists(ctx); err != nil {
			lg.Fatal("error checking subscription", log.KVErr(err))
		} else if !ok {
			//Subscription does not exist, attempt to create it
			// this may fail due to permissions

			// get the topic
			topic := client.Topic(psv.Topic_Name)
			ok, err := topic.Exists(ctx)
			if err != nil {
				lg.Fatal("error checking topic", log.KVErr(err))
			}
			if !ok {
				lg.Fatal("topic does not exist", log.KV("topic", psv.Topic_Name))
			}

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

		//fire of a verbose ticker for debugging and stats output
		if debugOn {
			go func() {
				for {
					time.Sleep(1 * time.Second)
					tmpcount := count
					tmpsize := size
					cdiff := tmpcount - oldcount
					sdiff := tmpsize - oldsize
					oldcount = tmpcount
					oldsize = tmpsize
					lg.Info("ingest stats", log.KV("eps", cdiff), log.KV("bps", sdiff), log.KV("bytes", oldsize))
				}
			}()
		}

		go func(sub *pubsub.Subscription, tagid entry.EntryTag, ps *pubsubconf) {
			eChan := make(chan *entry.Entry, 2048)
			go func(c chan *entry.Entry) {
				for e := range c {
					if err := procset.ProcessContext(e, exitCtx); err != nil {
						lg.Error("failed to process entry", log.KVErr(err))
					}
					count++
				}
				if err := procset.Close(); err != nil {
					lg.Error("failed to close processor", log.KVErr(err))
				}
			}(eChan)
			var window timegrinder.TimestampWindow
			window, err = cfg.Global.GlobalTimestampWindow()
			if err != nil {
				return
			}
			tcfg := timegrinder.Config{
				TSWindow:           window,
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

			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			for {
				callback := func(ctx context.Context, msg *pubsub.Message) {
					ent := &entry.Entry{
						Data: msg.Data,
						Tag:  tagid,
						SRC:  src,
					}
					size += uint64(len(msg.Data))
					if !ps.Parse_Time {
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
					select {
					case eChan <- ent:
						msg.Ack()
					case <-ctx.Done():
					}
				}
				if err := sub.Receive(cctx, callback); err != nil {
					lg.Error("receive failed", log.KVErr(err))
				}
			}
		}(sub, tagid, psv)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()

	exitFn()
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}
