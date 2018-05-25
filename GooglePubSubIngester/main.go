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
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/ingesters/version"
	"github.com/gravwell/timegrinder"

	"cloud.google.com/go/pubsub"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/pubsub_ingest.conf`
)

var (
	configLoc      = flag.String("config", defaultConfigLoc, "Location of configuration file")
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
	if *stderrOverride != `` {
		fp := path.Join(`/dev/shm/`, *stderrOverride)
		fout, err := os.Create(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create %s: %v\n", fp, err)
		} else {
			//file created, dup it
			if err := syscall.Dup2(int(fout.Fd()), int(os.Stderr.Fd())); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
		version.PrintVersion(fout)
		ingest.PrintVersion(fout)
	}
	lg = log.New(os.Stderr) // DO NOT close this, it will prevent backtraces from firing
}

func main() {
	cfg, err := GetConfig(*configLoc)
	if err != nil {
		lg.Fatal("Failed to get configuration: ", err)
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "Failed to open log file %s: %v", cfg.Global.Log_File, err)
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("Failed to add a writer: %v", err)
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "Invalid Log Level \"%s\": %v", cfg.Global.Log_Level, err)
			}
		}
	}

	tags, err := cfg.Tags()
	if err != nil {
		lg.Fatal("Failed to get tags from configuration: ", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: ", err)
	}
	debugout("Handling %d tags over %d targets\n", len(tags), len(conns))

	//fire up the ingesters
	ingestConfig := ingest.UniformMuxerConfig{
		Destinations: conns,
		Tags:         tags,
		Auth:         cfg.Secret(),
		LogLevel:     cfg.LogLevel(),
	}
	if cfg.CacheEnabled() {
		ingestConfig.EnableCache = true
		ingestConfig.CacheConfig.FileBackingLocation = cfg.CachePath()
	}
	igst, err := ingest.NewUniformMuxer(ingestConfig)
	if err != nil {
		lg.Fatal("Failed build our ingest system: %v\n", err)
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.Fatal("Failed start our ingest system: %v\n", err)
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timed out waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	// Set up environment variables for AWS auth, if extant
	if cfg.Global.Google_Credentials_Path != "" {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.Global.Google_Credentials_Path)
	}

	// make a client
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, cfg.Global.Project_ID)
	if err != nil {
		lg.Fatal("Couldn't create pubsub client: %v\n", err)
		return
	}

	for _, psv := range cfg.PubSub {
		tagid, err := igst.GetTag(psv.Tag_Name)
		if err != nil {
			lg.Fatal("Can't resolve tag %v: %v", psv.Tag_Name, err)
		}

		// get the topic
		topic := client.Topic(psv.Topic_Name)
		ok, err := topic.Exists(ctx)
		if err != nil {
			lg.Fatal("Error checking topic: %v", err)
		}
		if !ok {
			lg.Fatal("Topic %v doesn't exist", psv.Topic_Name)
		}

		// Get the subscription, creating if needed
		subname := fmt.Sprintf("ingest_%s", psv.Topic_Name)
		sub := client.Subscription(subname)
		ok, err = sub.Exists(ctx)
		if err != nil {
			lg.Fatal("Error checking subscription existence: %v", err)
		}
		if !ok {
			// doesn't exist, try creating it
			sub, err = client.CreateSubscription(ctx, subname, pubsub.SubscriptionConfig{
				Topic:       topic,
				AckDeadline: 10 * time.Second,
			})
			if err != nil {
				lg.Fatal("Error creating subscription: %v", err)
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
					lg.Info("%d entries per second at %d bytes per second (%d bytes total)", cdiff, sdiff, oldsize)
				}
			}()
		}

		go func(sub *pubsub.Subscription, tagid entry.EntryTag, ps *pubsubconf) {
			eChan := make(chan *entry.Entry, 2048)
			go func(c chan *entry.Entry) {
				for e := range c {
					if err := igst.WriteEntry(e); err != nil {
						lg.Error("Can't write entry: %v", err)
					}
					count++
				}
			}(eChan)
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
			}
			tg, err := timegrinder.NewTimeGrinder(tcfg)
			if err != nil {
				ps.Parse_Time = false
			}
			if ps.Assume_Localtime {
				tg.SetLocalTime()
			}

			var src net.IP
			if cfg.Global.Source_Override != `` {
				// global override
				src = net.ParseIP(cfg.Global.Source_Override)
				if src == nil {
					lg.Fatal("Global Source-Override is invalid")
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
					lg.Error("Receive failed: %v", err)
				}
			}
		}(sub, tagid, psv)
	}

	//register quit signals so we can die gracefully
	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, os.Interrupt)

	<-quitSig
}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}
