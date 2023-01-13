/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	"sync"
	"syscall"
	"time"

	"github.com/Azure/azure-amqp-common-go/v3/sas"
	eventhubs "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/Azure/azure-event-hubs-go/v3/persist"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config/validate"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/ingesters/version"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/azure_event_hubs.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/azure_event_hubs.conf.d`
	appName           = `AzureEventHubs`
)

var (
	configLoc      = flag.String("config-file", defaultConfigLoc, "Location of configuration file")
	confdLoc       = flag.String("config-overlays", defaultConfigDLoc, "Location for configuration overlay files")
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
			log.PrintOSInfo(fout)
			//file created, dup it
			if err := syscall.Dup3(int(fout.Fd()), int(os.Stderr.Fd()), 0); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to dup2 stderr: %v\n", err)
				fout.Close()
			}
		}
	}
	validate.ValidateConfig(GetConfig, *configLoc, *confdLoc)
}

func main() {
	debug.SetTraceback("all")
	cfg, err := GetConfig(*configLoc, *confdLoc)
	if err != nil {
		lg.Fatal("failed to get configuration", log.KV("path", *configLoc), log.KVErr(err))
	}

	if len(cfg.Global.Log_File) > 0 {
		fout, err := os.OpenFile(cfg.Global.Log_File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			lg.FatalCode(0, "failed to open log file", log.KV("path", cfg.Global.Log_File), log.KVErr(err))
		}
		if err = lg.AddWriter(fout); err != nil {
			lg.Fatal("failed to add a writer", log.KVErr(err))
		}
		if len(cfg.Global.Log_Level) > 0 {
			if err = lg.SetLevelString(cfg.Global.Log_Level); err != nil {
				lg.FatalCode(0, "invalid Log Level", log.KV("loglevel", cfg.Global.Log_Level), log.KVErr(err))
			}
		}
	}

	// Basic ingester standup stuff
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
		Logger:             lg,
		IngesterName:       appName,
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
	if cfg.Global.SelfIngest() {
		lg.AddRelay(igst)
	}
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

	// Here's where we start setting up Event Hubs stuff.
	// Create our context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up the disk persistence object
	diskPersist, err := persist.NewFilePersister(cfg.Global.State_Store_Location)
	if err != nil {
		lg.FatalCode(0, "failed to set up state persistence", log.KVErr(err))
	}
	// Just to make sure: chmod the directory, because the code wants to set it 777 (ugh)
	if err := os.Chmod(cfg.Global.State_Store_Location, 0700); err != nil {
		lg.FatalCode(0, "failed to set permissions on state directory", log.KVErr(err))
	}
	// Now set up the *memory* persister which we'll actually hand in to the hub object.
	// This saves on disk writes and keeps performance up.
	memPersist := persist.NewMemoryPersister()

	// These are the handlers listening to each individual partition
	var listeners []*eventhubs.ListenerHandle
	// this is where we keep track of what we're receiving on
	var readers []readerInfo

	// This little goroutine tries to keep persistence updated in case of catastrophic
	// failure, without totally smashing the disk like it would if we allowed an update
	// on every entry read.
	quitSig := make(chan bool)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		last := make(map[string]persist.Checkpoint)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-quitSig:
				return
			case <-ticker.C:
				for _, r := range readers {
					// read it from the memory persister
					checkpoint, err := memPersist.Read(r.namespace, r.hub, r.consumerGroup, r.partitionID)
					if err != nil {
						lg.Error("Failed to read checkpoint", log.KVErr(err))
						continue
					}
					// See if it's any different
					if prev, ok := last[r.key()]; ok {
						if prev.Offset == checkpoint.Offset {
							// no change, skip
							continue
						}
					}
					last[r.key()] = checkpoint
					// and write it to disk
					if err := diskPersist.Write(r.namespace, r.hub, r.consumerGroup, r.partitionID, checkpoint); err != nil {
						lg.Error("Failed to write checkpoint to disk", log.KVErr(err))
					}
				}
			}
		}
	}()

	for k, def := range cfg.EventHub {
		// We can parallelize the connections to the individual hubs.
		wg.Add(1)
		go func(hubname string, hubDef eventHubConf) {
			defer wg.Done()
			// Shadow the logger with one that always appends the hub info
			lg := log.NewLoggerWithKV(lg,
				log.KV("hub", hubname),
				log.KV("tag", hubDef.Tag_Name),
			)
			tagid, err := igst.GetTag(hubDef.Tag_Name)
			if err != nil {
				lg.Fatal("failed to resolve tag", log.KVErr(err))
			}

			procset, err := cfg.Preprocessor.ProcessorSet(igst, hubDef.Preprocessor)
			if err != nil {
				lg.Fatal("preprocessor construction failed", log.KVErr(err))
			}
			defer procset.Close()

			// Set up authentication
			provider, err := sas.NewTokenProvider(sas.TokenProviderWithKey(hubDef.Token_Name, hubDef.Token_Key))
			if err != nil {
				lg.Fatal("failed to get token provider", log.KVErr(err))
			}

			// Connect to the hub. We do this synchronously so we can bail out easier if one is misconfigured.
			hub, err := eventhubs.NewHub(hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, provider, eventhubs.HubWithOffsetPersistence(memPersist))
			if err != nil {
				lg.Fatal("failed to connect to hub", log.KVErr(err))
			}
			defer func() {
				cctx, _ := context.WithTimeout(ctx, 2*time.Second)
				if err := hub.Close(cctx); err != nil {
					lg.Error("failed to close event hub", log.KVErr(err))
				}
			}()
			lg.Info("connected to event hub")

			// stats stuff
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
						lg.Info("ingest stats", log.KV("eps", cdiff), log.KV("bps", sdiff), log.KV("bytes", oldsize))
					}
				}()
			}

			// configure time handling
			tcfg := timegrinder.Config{
				EnableLeftMostSeed: true,
			}
			tg, err := timegrinder.NewTimeGrinder(tcfg)
			if err != nil {
				hubDef.Parse_Time = false
			}
			if hubDef.Assume_Local_Timezone {
				tg.SetLocalTime()
			}
			if hubDef.Timezone_Override != `` {
				err = tg.SetTimezone(hubDef.Timezone_Override)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to set timezone to %v: %v\n", hubDef.Timezone_Override, err)
					return
				}
			}

			// configure the SRC field
			var src net.IP
			if cfg.Global.Source_Override != `` {
				// global override
				src = net.ParseIP(cfg.Global.Source_Override)
				if src == nil {
					lg.Fatal("Global Source-Override is invalid", log.KV("override", cfg.Global.Source_Override))
				}
			}

			// This function gets called whenever an entry is received from an Events Hub partition.
			// It packages the entry, extracts an appropriate timestamp, and sends it to the indexer.
			callback := func(ctx context.Context, msg *eventhubs.Event) error {
				ent := &entry.Entry{
					Data: msg.Data,
					Tag:  tagid,
					SRC:  src,
				}
				size += uint64(len(msg.Data))
				if hubDef.Parse_Time == false {
					if msg.SystemProperties != nil && msg.SystemProperties.EnqueuedTime != nil {
						ent.TS = entry.FromStandard(*msg.SystemProperties.EnqueuedTime)
					} else {
						ent.TS = entry.Now()
					}
				} else {
					ts, ok, err := tg.Extract(msg.Data)
					if !ok || err != nil {
						//  failed to extract, use the publishtime
						hubDef.Parse_Time = false
						if msg.SystemProperties != nil && msg.SystemProperties.EnqueuedTime != nil {
							ent.TS = entry.FromStandard(*msg.SystemProperties.EnqueuedTime)
						} else {
							ent.TS = entry.Now()
						}
					} else {
						ent.TS = entry.FromStandard(ts)
					}
				}
				if err := procset.Process(ent); err != nil {
					lg.Error("failed to process entry", log.KVErr(err))
				}
				count++
				return nil
			}

			// get info about partitions in the hub
			info, err := hub.GetRuntimeInformation(ctx)
			if err != nil {
				lg.Fatal("failed to get runtime info", log.KVErr(err))
			}

			// Launch a listener for each partition in the hub
			// Calling Receive takes a while, but we can't really parallelize it because the first thing
			// Receive does is lock a mutex in the Hub -- one way or another, it's basically serial.
			for _, partitionID := range info.PartitionIDs {
				// ask where to start from
				checkpoint, err := diskPersist.Read(hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, partitionID)
				if err != nil {
					// set a default, we will check user setting next
					checkpoint = persist.NewCheckpointFromStartOfStream()
				}
				if checkpoint.Offset == persist.StartOfStream && hubDef.Initial_Checkpoint == "end" {
					checkpoint = persist.NewCheckpointFromEndOfStream()
				}
				handle, err := hub.Receive(
					ctx,
					partitionID,
					callback,
					eventhubs.ReceiveWithStartingOffset(checkpoint.Offset),
				)
				if err != nil {
					lg.Error("failed to start event hub partition receiver", log.KVErr(err))
					return
				}
				listeners = append(listeners, handle)
				readers = append(readers, readerInfo{hubDef.Event_Hubs_Namespace, hubDef.Event_Hub, hubDef.Consumer_Group, partitionID})
				lg.Info("started receiver for partition", log.KV("partition", partitionID))
			}
			<-quitSig
		}(k, *def)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()

	lg.Info("exiting")
	// Tell every event handler to close
	for _, h := range listeners {
		cctx, _ := context.WithTimeout(ctx, 2*time.Second)
		h.Close(cctx)
	}

	// Tell our goroutines to bail out
	close(quitSig)

	wg.Wait()
	lg.Info("all goroutines done")

	// Write out persistence info one last time by hand.
	for _, r := range readers {
		// read it from the memory persister
		checkpoint, err := memPersist.Read(r.namespace, r.hub, r.consumerGroup, r.partitionID)
		if err != nil {
			lg.Error("Failed to read checkpoint", log.KVErr(err))
			continue
		}
		// and write it to disk
		if err := diskPersist.Write(r.namespace, r.hub, r.consumerGroup, r.partitionID, checkpoint); err != nil {
			lg.Error("Failed to write checkpoint to disk", log.KVErr(err))
		}
	}
	lg.Info("state saved, exiting")
}

func debugout(format string, args ...interface{}) {
	if !*verbose {
		return
	}
	fmt.Printf(format, args...)
}

type readerInfo struct {
	namespace     string
	hub           string
	consumerGroup string
	partitionID   string
}

func (r readerInfo) key() string {
	return fmt.Sprintf("%s|%s|%s|%s", r.namespace, r.hub, r.consumerGroup, r.partitionID)
}
