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
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/timegrinder"
)

const (
	defaultConfigLoc  = `/opt/gravwell/etc/azure_event_hubs.conf`
	defaultConfigDLoc = `/opt/gravwell/etc/azure_event_hubs.conf.d`
	appName           = `AzureEventHubs`
)

var (
	lg      *log.Logger
	debugOn bool
)

func main() {
	go debug.HandleDebugSignals(appName)

	var cfg *cfgType
	ibc := base.IngesterBaseConfig{
		IngesterName:                 appName,
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

	// This context governs every hub's Processor and partition worker. Cancelling
	// it tells every Processor.Run loop (and thus every partition receive loop) to
	// stop, and each partition worker writes one last checkpoint on its way out.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Checkpoints are stored locally on disk, one JSON file per partition, so
	// no external storage account is required. Because this is local state, if
	// you run more than one instance of this ingester against the same hub +
	// consumer group they will not coordinate with each other (see
	// fileCheckpointStore's doc comment).
	checkpointStore, err := newFileCheckpointStore(cfg.Global.Checkpoint_Storage_Location)
	if err != nil {
		lg.FatalCode(0, "failed to create checkpoint store", log.KVErr(err))
	}

	var wg sync.WaitGroup

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

			// The new SDK authenticates with a connection string or an
			// azcore.TokenCredential rather than the old sas.TokenProvider, so we
			// build a connection string from the same SAS policy name/key the
			// config already supplies.
			connStr := buildEventHubConnectionString(hubDef)

			consumerClient, err := eventhubs.NewConsumerClientFromConnectionString(connStr, "", eventhubs.DefaultConsumerGroup, nil)
			if err != nil {
				lg.Fatal("failed to connect to hub", log.KVErr(err))
			}
			defer func() {
				cctx, cf := context.WithTimeout(context.Background(), 2*time.Second)
				if err := consumerClient.Close(cctx); err != nil {
					lg.Error("failed to close event hub client", log.KVErr(err))
				}
				cf()
			}()

			lg.Info("connected to event hub")

			// stats stuff
			var count, size uint64
			var oldcount, oldsize uint64
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

			// configure time handling
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
				// failed to create a timegrinder object, do not attempt to parse the time off of hub messages
				hubDef.Parse_Time = false
				lg.Error("timegrinder error", log.KVErr(err))
			} else {
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

			// This function gets called whenever an entry is received from an Event Hub partition.
			// It packages the entry, extracts an appropriate timestamp, and sends it to the indexer.
			processEvent := func(msg *eventhubs.ReceivedEventData) {
				ent := &entry.Entry{
					Data: msg.Body,
					Tag:  tagid,
					SRC:  src,
				}
				size += uint64(len(msg.Body))
				ent.TS = entryTimestamp(&hubDef, tg, msg.Body, msg.EnqueuedTime)

				if err := procset.ProcessContext(ent, exitCtx); err != nil {
					lg.Error("failed to process entry", log.KVErr(err))
				}
				count++
			}

			// Where to start a partition that has no existing checkpoint yet.
			startPosition := startPositionFor(hubDef.Initial_Checkpoint)

			processor, err := eventhubs.NewProcessor(consumerClient, checkpointStore, &eventhubs.ProcessorOptions{
				StartPositions: eventhubs.StartPositions{Default: startPosition},
			})
			if err != nil {
				lg.Fatal("failed to create processor", log.KVErr(err))
			}

			// Launch a worker for each partition the Processor hands us. The Processor
			// itself handles partition load-balancing/ownership via the checkpoint store.
			var partWg sync.WaitGroup
			go func() {
				for {
					partClient := processor.NextPartitionClient(ctx)
					if partClient == nil {
						// Processor has stopped.
						return
					}
					partWg.Add(1)
					go func(pc *eventhubs.ProcessorPartitionClient) {
						defer partWg.Done()
						defer pc.Close(context.Background())
						runPartition(ctx, pc, processEvent, lg)
					}(partClient)
				}
			}()

			lg.Info("started processor")
			if err := processor.Run(ctx); err != nil {
				lg.Error("processor exited with error", log.KVErr(err))
			}
			partWg.Wait()
		}(k, *def)
	}

	//register quit signals so we can die gracefully
	utils.WaitForQuit()
	ib.AnnounceShutdown()
	exitFn()

	// Tell every hub's Processor (and its partition workers) to shut down; each
	// partition worker writes a final checkpoint before it returns.
	cancel()
	wg.Wait()

	lg.Info("all goroutines done")
}

// runPartition receives events from a single partition and periodically checkpoints
// progress, rather than checkpointing on every single entry.
func runPartition(ctx context.Context, pc *eventhubs.ProcessorPartitionClient, processEvent func(*eventhubs.ReceivedEventData), lg *log.KVLogger) {
	var lastEvent *eventhubs.ReceivedEventData
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	writeCheckpoint := func(ctx context.Context) {
		if lastEvent == nil {
			return
		}
		if err := pc.UpdateCheckpoint(ctx, lastEvent, nil); err != nil {
			lg.Error("failed to update checkpoint", log.KVErr(err))
			return
		}
		lastEvent = nil
	}

	for {
		select {
		case <-ctx.Done():
			cctx, cf := context.WithTimeout(context.Background(), 2*time.Second)
			writeCheckpoint(cctx)
			cf()
			return
		case <-ticker.C:
			writeCheckpoint(ctx)
		default:
		}

		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		events, err := pc.ReceiveEvents(recvCtx, 100, nil)
		recvCancel()

		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			var ehErr *eventhubs.Error
			if errors.As(err, &ehErr) && ehErr.Code == eventhubs.ErrorCodeOwnershipLost {
				return
			}
			if ctx.Err() != nil {
				return
			}
			lg.Error("failed to receive events from partition", log.KV("partition", pc.PartitionID()), log.KVErr(err))
			continue
		}

		for _, evt := range events {
			processEvent(evt)
			lastEvent = evt
		}
	}
}

func debugout(format string, args ...interface{}) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

// buildEventHubConnectionString builds a SAS connection string for the new SDK
// from the same Event-Hubs-Namespace/Token-Name/Token-Key/Event-Hub config values
// used by the old SDK's sas.TokenProvider.
func buildEventHubConnectionString(hubDef eventHubConf) string {
	return fmt.Sprintf(
		"Endpoint=sb://%s.servicebus.windows.net/;SharedAccessKeyName=%s;SharedAccessKey=%s;EntityPath=%s",
		hubDef.Event_Hubs_Namespace, hubDef.Token_Name, hubDef.Token_Key, hubDef.Event_Hub,
	)
}

// startPositionFor returns the position a partition with no existing checkpoint
// should start reading from, based on the config's Initial-Checkpoint setting.
func startPositionFor(initialCheckpoint string) eventhubs.StartPosition {
	if initialCheckpoint == "end" {
		return eventhubs.StartPosition{Latest: to.Ptr(true)}
	}
	return eventhubs.StartPosition{Earliest: to.Ptr(true)}
}

// entryTimestamp picks the timestamp for a received event: if time parsing is
// enabled it tries to extract a timestamp from the event body, falling back to
// the event's enqueued time (and disabling parsing for subsequent events on this
// hub) if extraction fails, and finally to the current time if no enqueued time
// is available at all.
func entryTimestamp(hubDef *eventHubConf, tg *timegrinder.TimeGrinder, data []byte, enqueued *time.Time) entry.Timestamp {
	if !hubDef.Parse_Time {
		return fallbackTimestamp(enqueued)
	}
	ts, ok, err := tg.Extract(data)
	if !ok || err != nil {
		// failed to extract, use the enqueued time from here on out
		hubDef.Parse_Time = false
		return fallbackTimestamp(enqueued)
	}
	return entry.FromStandard(ts)
}

func fallbackTimestamp(enqueued *time.Time) entry.Timestamp {
	if enqueued != nil {
		return entry.FromStandard(*enqueued)
	}
	return entry.Now()
}
