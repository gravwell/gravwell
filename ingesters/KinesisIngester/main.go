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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	// Embed tzdata so that we don't rely on potentially broken timezone DBs on the host
	_ "time/tzdata"

	"github.com/gravwell/gravwell/v3/debug"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingesters/base"
	"github.com/gravwell/gravwell/v3/ingesters/utils"
	"github.com/gravwell/gravwell/v3/sqs_common"
	"github.com/gravwell/gravwell/v3/timegrinder"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

const (
	defaultConfigLoc         = `/opt/gravwell/etc/kinesis_ingest.conf`
	defaultConfigDLoc        = `/opt/gravwell/etc/kinesis_ingest.conf.d`
	appName           string = `kinesis`
)

var (
	lg      *log.Logger
	debugOn bool
)

func main() {
	go debug.HandleDebugSignals(appName)

	var wg sync.WaitGroup
	var cfg *cfgType
	running := true

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
	defer func() {
		if err := igst.Close(); err != nil {
			_ = ib.Logger.Error("failed to close muxer", log.KVErr(err))
		}
	}()
	ib.AnnounceStartup()

	debugout("Started ingester muxer\n")

	// Get the state file
	stateFile, err := utils.NewState(cfg.Global.State_Store_Location, 0600)
	if err != nil {
		lg.Fatal("failed to open state file", log.KV("path", cfg.Global.State_Store_Location), log.KVErr(err))
	}

	stateMan := NewStateman(stateFile)
	stateMan.Start()
	defer stateMan.Close()

	c, err := sqs_common.GetCredentials(cfg.Global.Credentials_Type, cfg.Global.AWS_Access_Key_ID, cfg.Global.AWS_Secret_Access_Key)
	if err != nil {
		lg.Fatal("obtaining credentials", log.KVErr(err))
	}

	dieChan := make(chan bool)

	ctx, cancel := context.WithCancel(context.Background())

	for _, stream := range cfg.KinesisStream {
		tagid, err := igst.GetTag(stream.Tag_Name)
		if err != nil {
			lg.Fatal("failed to resolve tag", log.KV("tag", stream.Tag_Name), log.KV("stream", stream.Stream_Name), log.KVErr(err))
		}

		loadOpts := []func(*config.LoadOptions) error{
			config.WithRegion(stream.Region),
		}
		if c != nil {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(c))
		}
		awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
		if err != nil {
			lg.Fatal("creating aws config", log.KVErr(err))
		}

		var kinesisOpts []func(*kinesis.Options)
		if stream.Endpoint != "" {
			kinesisOpts = append(kinesisOpts, func(o *kinesis.Options) {
				o.BaseEndpoint = new(stream.Endpoint)
			})
		}

		svc := kinesis.NewFromConfig(awsCfg, kinesisOpts...)

		// Get the list of shards
		shards := []types.Shard{}
		dsi := &kinesis.DescribeStreamInput{
			StreamName: new(stream.Stream_Name),
		}
		count := 0
		for {
			streamdesc, err := svc.DescribeStream(ctx, dsi)
			if err != nil {
				count++
				_ = lg.Error("failed to get stream description", log.KV("stream", stream.Stream_Name), log.KVErr(err))
				if count >= 5 {
					// give up and LOUDLY quit
					lg.Fatal("giving up fetch stream description for stream after 5 attempts, exiting.", log.KV("stream", stream.Stream_Name))
				}
				utils.QuitableSleep(ctx, 1*time.Second)
				continue
			}
			newshards := streamdesc.StreamDescription.Shards
			shards = append(shards, newshards...)
			if *streamdesc.StreamDescription.HasMoreShards {
				dsi.ExclusiveStartShardId = newshards[len(newshards)-1].ShardId
			} else {
				break
			}
		}
		debugout("Read %d shards from stream %s\n", len(shards), stream.Stream_Name)

		// Now start up the metrics reporter
		var metricsTrackers []*shardMetrics
		if stream.Metrics_Interval > 0 {
			go func(stream streamDef) {
				for {
					select {
					case <-dieChan:
						return
					case <-time.After(time.Duration(stream.Metrics_Interval) * time.Second):
						report := metricsReport{StreamName: stream.Stream_Name, ShardCount: len(shards)}
						for i := range metricsTrackers {
							l, b, e, r := metricsTrackers[i].ReadAndReset()
							report.AverageLag += l
							report.CompressedDataSize += b
							report.EntryDataSize += e
							report.KinesisRequests += r
						}
						report.AverageLag = report.AverageLag / int64(len(metricsTrackers))
						if stream.JSON_Metrics {
							jr, err := json.Marshal(report)
							if err == nil {
								_ = lg.Infof("%v", string(jr))
							}
						} else {
							_ = lg.Info("stream stats",
								log.KV("stream", stream.Stream_Name),
								log.KV("shards", len(shards)),
								log.KV("delay", report.AverageLag),
								log.KV("compressedsize", report.CompressedDataSize),
								log.KV("requestcount", report.KinesisRequests),
								log.KV("size", report.EntryDataSize))
						}
					}
				}
			}(*stream)
		}

		for i, shard := range shards {
			// Detect and skip closed shards
			if shard.SequenceNumberRange != nil && shard.SequenceNumberRange.EndingSequenceNumber != nil {
				_ = lg.Info("shard appears closed, skipping", log.KV("shard", *shard.ShardId), log.KV("stream", stream.Stream_Name))
				continue
			}
			//get timegrinder stood up
			var window timegrinder.TimestampWindow
			window, err = cfg.Global.GlobalTimestampWindow()
			if err != nil {
				return
			}
			tcfg := timegrinder.Config{
				TSWindow:           window,
				EnableLeftMostSeed: true,
			}
			tgr, err := timegrinder.NewTimeGrinder(tcfg)
			if err != nil {
				lg.FatalCode(0, "failed to create timegrinder", log.KVErr(err))
				return
			} else if err := cfg.TimeFormat.LoadFormats(tgr); err != nil {
				lg.FatalCode(0, "failed to load custom time formats", log.KVErr(err))
				return
			}
			if stream.Assume_Local_Timezone {
				tgr.SetLocalTime()
			}
			if stream.Timezone_Override != `` {
				if err = tgr.SetTimezone(stream.Timezone_Override); err != nil {
					lg.FatalCode(0, "failed to set timezone", log.KV("timezone", stream.Timezone_Override), log.KVErr(err))
					return
				}
			}

			wg.Add(1)
			go func(stream streamDef, shard types.Shard, tagid entry.EntryTag, shardid int, tg *timegrinder.TimeGrinder) {
				defer wg.Done()
				// set up timegrinder and other long-lived stuff
				var src net.IP
				if cfg.Global.Source_Override != `` {
					// global override
					src = net.ParseIP(cfg.Global.Source_Override)
					if src == nil {
						lg.Fatal("Global Source-Override is invalid")
					}
				}

				// one processor set per shard
				procset, err := cfg.Preprocessor.ProcessorSet(igst, stream.Preprocessor)
				if err != nil {
					lg.Fatal("preprocessor construction error", log.KVErr(err))
				}

				// make the shardMetrics and add it to the array
				tracker := shardMetrics{}
				metricsTrackers = append(metricsTrackers, &tracker)
				if stream.Metrics_Interval == 0 {
					// disable it
					tracker.Disabled = true
				}

			reconnectLoop:
				for {
					gsii := &kinesis.GetShardIteratorInput{
						ShardId:    new(*shard.ShardId),
						StreamName: new(stream.Stream_Name),
					}
					seqnum := stateMan.GetSequenceNum(stream.Stream_Name, *shard.ShardId)
					if seqnum == `` {
						// we don't have a previous state
						debugout("No previous sequence number for stream %v shard %v, defaulting to %v\n", stream.Stream_Name, *shard.ShardId, stream.Iterator_Type)
						gsii.ShardIteratorType = types.ShardIteratorType(stream.Iterator_Type)
					} else {
						gsii.ShardIteratorType = types.ShardIteratorTypeAfterSequenceNumber
						gsii.StartingSequenceNumber = new(seqnum)
					}

					output, err := svc.GetShardIterator(ctx, gsii)
					if err != nil {
						_ = lg.Error("error on shard", log.KV("number", shardid), log.KV("stream", stream.Stream_Name), log.KV("shard", *shard.ShardId), log.KVErr(err))
						utils.QuitableSleep(ctx, 5*time.Second)
						continue
					}
					if output.ShardIterator == nil {
						// this is weird, we are going to bail out
						_ = lg.Error("got nil initial shard iterator, sleeping and retrying")
						utils.QuitableSleep(ctx, 5*time.Second)
						continue
					}
					iter := *output.ShardIterator

					var lastSeqNum string
					for running {
						gri := &kinesis.GetRecordsInput{
							Limit:         new(int32(5000)),
							ShardIterator: new(iter),
						}
						var res *kinesis.GetRecordsOutput
						var err error
						for {
							res, err = svc.GetRecords(ctx, gri)
							if res != nil {
								if res.NextShardIterator != nil {
									iter = *res.NextShardIterator
								}
							}
							if err != nil {
								var throughputErr *types.ProvisionedThroughputExceededException
								var iteratorErr *types.ExpiredIteratorException
								switch {
								case errors.As(err, &throughputErr):
									_ = lg.Warn("throughput exceeded, trying again", log.KV("shard", *shard.ShardId), log.KV("stream",
										stream.Stream_Name))
									utils.QuitableSleep(ctx, 500*time.Millisecond)
								case errors.As(err, &iteratorErr):
									_ = lg.Info("Iterator expired, re-initializing", log.KV("shard", *shard.ShardId), log.KV("stream",
										stream.Stream_Name))
									utils.QuitableSleep(ctx, 100*time.Millisecond)
									continue reconnectLoop
								default:
									_ = lg.Error("answer error", log.KVErr(err), log.KV("shard", *shard.ShardId), log.KV("stream",
										stream.Stream_Name))
									utils.QuitableSleep(ctx, 500*time.Millisecond)
								}
							} else {
								// if we got no records, chill for a sec before we hit it again
								if len(res.Records) == 0 {
									utils.QuitableSleep(ctx, 100*time.Millisecond)
								}
								break
							}
						}

						var entrySize int
						for _, r := range res.Records {
							lastSeqNum = *r.SequenceNumber
							ent := &entry.Entry{
								Tag:  tagid,
								SRC:  src,
								Data: r.Data,
							}
							if !stream.Parse_Time {
								ent.TS = entry.FromStandard(*r.ApproximateArrivalTimestamp)
							} else {
								ts, ok, err := tg.Extract(ent.Data)
								if !ok || err != nil {
									// something went wrong, switch to using kinesis timestamps
									stream.Parse_Time = false
									ent.TS = entry.FromStandard(*r.ApproximateArrivalTimestamp)
								} else {
									ent.TS = entry.FromStandard(ts)
								}
							}
							if err = procset.ProcessContext(ent, ctx); err != nil {
								_ = lg.Error("Failed to handle entry", log.KVErr(err))
							}
							entrySize += int(ent.Size())
						}
						tracker.Update(res, entrySize)
						// Now update the most recent sequence number
						if lastSeqNum != `` {
							stateMan.UpdateSequenceNum(stream.Stream_Name, *shard.ShardId, lastSeqNum)
						}
					}
					if err = procset.Close(); err != nil {
						_ = lg.Error("Failed to close processor set", log.KVErr(err))
					}
					// if we get to this point, exit the for loop
					break
				}
			}(*stream, shard, tagid, i, tgr)
		}
	}

	utils.WaitForQuit()
	ib.AnnounceShutdown()

	running = false
	close(dieChan)

	go func() {
		time.Sleep(time.Second)
		cancel()
	}()

	wg.Wait()
}

func debugout(format string, args ...any) {
	if debugOn {
		fmt.Printf(format, args...)
	}
}

type metricsReport struct {
	StreamName         string
	ShardCount         int
	AverageLag         int64
	CompressedDataSize uint64
	EntryDataSize      uint64
	KinesisRequests    uint64
}

type shardMetrics struct {
	sync.Mutex
	Disabled     bool
	millisbehind int64
	datasize     uint64
	entrysize    uint64
	requests     uint64
}

func (s *shardMetrics) Update(res *kinesis.GetRecordsOutput, entrySize int) {
	s.Lock()
	defer s.Unlock()
	if s.Disabled {
		return
	}
	s.millisbehind = *res.MillisBehindLatest
	s.requests++
	var dsize int
	for i := range res.Records {
		dsize += len(res.Records[i].Data)
	}
	s.datasize += uint64(dsize)
	s.entrysize += uint64(entrySize)
}

func (s *shardMetrics) ReadAndReset() (millis int64, datasize uint64, entrysize uint64, requests uint64) {
	s.Lock()
	defer s.Unlock()
	millis = s.millisbehind
	datasize = s.datasize
	s.datasize = 0
	entrysize = s.entrysize
	s.entrysize = 0
	requests = s.requests
	s.requests = 0
	return
}

type stateman struct {
	sync.Mutex
	states    map[string]map[string]string // map of stream name to shard name to sequence number
	stateFile *utils.State
}

func NewStateman(stateFile *utils.State) *stateman {
	sm := stateman{
		states:    make(map[string]map[string]string),
		stateFile: stateFile,
	}
	_ = stateFile.Read(&sm.states)
	return &sm
}

func (s *stateman) Start() {
	go func() {
		tckr := time.NewTicker(15 * time.Second)
		defer tckr.Stop()
		for range tckr.C {
			s.Flush()
		}
	}()
}

func (s *stateman) Close() {
	s.Flush()
}

func (s *stateman) Flush() {
	s.Lock()
	defer s.Unlock()
	_ = s.stateFile.Write(s.states)
}

func (s *stateman) UpdateSequenceNum(stream, shard, seq string) {
	s.Lock()
	defer s.Unlock()

	_, ok := s.states[stream]
	if !ok {
		// initialize the stream
		s.states[stream] = make(map[string]string)
	}
	s.states[stream][shard] = seq
}

func (s *stateman) GetSequenceNum(stream, shard string) string {
	s.Lock()
	defer s.Unlock()

	_, ok := s.states[stream]
	if !ok {
		// initialize the stream
		s.states[stream] = make(map[string]string)
	}
	return s.states[stream][shard]
}
