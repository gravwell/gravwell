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
	"log"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/timegrinder"

	"cloud.google.com/go/pubsub"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/pubsub_ingest.conf`
)

var (
	configLoc      = flag.String("config", defaultConfigLoc, "Location of configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
)

func init() {
	flag.Parse()
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
	}
}

func main() {
	cfg, err := GetConfig(*configLoc)
	if err != nil {
		log.Fatal("Failed to get configuration: ", err)
	}

	tags, err := cfg.Tags()
	if err != nil {
		log.Fatal("Failed to get tags from configuration: ", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		log.Fatal("Failed to get backend targets from configuration: ", err)
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
		fmt.Fprintf(os.Stderr, "Failed build our ingest system: %v\n", err)
		return
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed start our ingest system: %v\n", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		fmt.Fprintf(os.Stderr, "Timedout waiting for backend connections: %v\n", err)
		return
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
		log.Fatalf("Couldn't create pubsub client: %v\n", err)
		return
	}

	for _, ps := range cfg.PubSub {
		tagid, err := igst.GetTag(ps.Tag_Name)
		if err != nil {
			log.Fatalf("Can't resolve tag %v: %v", ps.Tag_Name, err)
		}

		// get the topic
		topic := client.Topic(ps.Topic_Name)
		ok, err := topic.Exists(ctx)
		if err != nil {
			log.Fatalf("Error checking topic: %v", err)
		}
		if !ok {
			log.Fatalf("Topic %v doesn't exist", ps.Topic_Name)
		}

		// Get the subscription, creating if needed
		subname := fmt.Sprintf("ingest_%s", ps.Topic_Name)
		sub := client.Subscription(subname)
		ok, err = sub.Exists(ctx)
		if err != nil {
			log.Fatalf("Error checking subscription existence: %v", err)
		}
		if !ok {
			// doesn't exist, try creating it
			sub, err = client.CreateSubscription(ctx, subname, pubsub.SubscriptionConfig{
				Topic:       topic,
				AckDeadline: 10 * time.Second,
			})
			if err != nil {
				log.Fatalf("Error creating subscription: %v", err)
			}
		}

		var count uint64
		var oldcount uint64

		if *verbose {
			go func() {
				for {
					time.Sleep(1 * time.Second)
					t := count
					diff := t - oldcount
					oldcount = t
					log.Printf("%d entries per second", diff)
				}
			}()
		}

		go func(sub *pubsub.Subscription, tagid entry.EntryTag) {
			eChan := make(chan *entry.Entry, 2048)
			go func(c chan *entry.Entry) {
				for e := range c {
					if err := igst.WriteEntry(e); err != nil {
						log.Printf("Can't write entry: %v", err)
					}
					count++
				}
			}(eChan)
			tg, err := timegrinder.NewTimeGrinder()
			if err != nil {
				ps.Parse_Time = false
			}
			if ps.Assume_Localtime {
				tg.SetLocalTime()
			}

			for {
				callback := func(ctx context.Context, msg *pubsub.Message) {
					ent := &entry.Entry{
						Data: msg.Data,
						Tag:  tagid,
					}
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
				cctx, _ := context.WithCancel(ctx)
				err := sub.Receive(cctx, callback)
				if err != nil {
					log.Printf("Receive error: %v", err)
				}
			}
		}(sub, tagid)
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
