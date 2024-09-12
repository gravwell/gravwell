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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v4/debug"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/base"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
	"github.com/gravwell/gravwell/v4/sqs_common"
	"github.com/gravwell/gravwell/v4/timegrinder"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
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
	defer igst.Close()
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

		// make an aws session
		sess, err := session.NewSession(&aws.Config{
			Credentials: c,
			Region:      aws.String(stream.Region),
			Endpoint:    aws.String(stream.Endpoint),
		})
		if err != nil {
			lg.Fatal("creating session", log.KVErr(err))
		}

		// get a handle on kinesis
		svc := kinesis.New(sess, aws.NewConfig().WithRegion(stream.Region))

		// Get the list of shards
		shards := []*kinesis.Shard{}
		dsi := &kinesis.DescribeStreamInput{}
		dsi.SetStreamName(stream.Stream_Name)
		count := 0
		for {
			streamdesc, err := svc.DescribeStream(dsi)
			if err != nil {
				count++
				lg.Error("failed to get stream description", log.KV("stream", stream.Stream_Name), log.KVErr(err))
				if count >= 5 {
					// give up and LOUDLY quit
					lg.Fatal("giving up fetch stream description for stream after 5 attempts, exiting.", log.KV("stream", stream.Stream_Name))
				}
				time.Sleep(1 * time.Second)
				continue
			}
			newshards := streamdesc.StreamDescription.Shards
			shards = append(shards, newshards...)
			if *streamdesc.StreamDescription.HasMoreShards {
				dsi.SetExclusiveStartShardId(*(newshards[len(newshards)-1].ShardId))
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
								lg.Infof("%v", string(jr))
							}
						} else {
							lg.Info("stream stats",
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
				lg.Info("shard appears closed, skipping", log.KV("shard", *shard.ShardId), log.KV("stream", stream.Stream_Name))
				continue
			}
			//get timegrinder stood up
			tcfg := timegrinder.Config{
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

			go func(stream streamDef, shard kinesis.Shard, tagid entry.EntryTag, shardid int, tg *timegrinder.TimeGrinder) {
				wg.Add(1)
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
					gsii := &kinesis.GetShardIteratorInput{}
					gsii.SetShardId(*shard.ShardId)
					gsii.SetStreamName(stream.Stream_Name)
					seqnum := stateMan.GetSequenceNum(stream.Stream_Name, *shard.ShardId)
					if seqnum == `` {
						// we don't have a previous state
						debugout("No previous sequence number for stream %v shard %v, defaulting to %v\n", stream.Stream_Name, *shard.ShardId, stream.Iterator_Type)
						gsii.SetShardIteratorType(stream.Iterator_Type)
					} else {
						gsii.SetShardIteratorType(`AFTER_SEQUENCE_NUMBER`)
						gsii.SetStartingSequenceNumber(seqnum)
					}

					output, err := svc.GetShardIterator(gsii)
					if err != nil {
						lg.Error("error on shard", log.KV("number", shardid), log.KV("stream", stream.Stream_Name), log.KV("shard", *shard.ShardId), log.KVErr(err))
						time.Sleep(5 * time.Second)
						continue
					}
					if output.ShardIterator == nil {
						// this is weird, we are going to bail out
						lg.Error("got nil initial shard iterator, sleeping and retrying")
						time.Sleep(5 * time.Second)
						continue
					}
					iter := *output.ShardIterator

					var lastSeqNum string
					for running {
						gri := &kinesis.GetRecordsInput{}
						gri.SetLimit(5000)
						gri.SetShardIterator(iter)
						var res *kinesis.GetRecordsOutput
						var err error
						for {
							res, err = svc.GetRecords(gri)
							if res != nil {
								if res.NextShardIterator != nil {
									iter = *res.NextShardIterator
								}
							}
							if err != nil {
								if awsErr, ok := err.(awserr.Error); ok {
									// process SDK error
									if awsErr.Code() == kinesis.ErrCodeProvisionedThroughputExceededException {
										lg.Warn("throughput exceeded, trying again", log.KV("shard", *shard.ShardId), log.KV("stream", stream.Stream_Name))
										time.Sleep(500 * time.Millisecond)
									} else if awsErr.Code() == kinesis.ErrCodeExpiredIteratorException {
										lg.Info("Iterator expired, re-initializing", log.KV("shard", *shard.ShardId), log.KV("stream", stream.Stream_Name))
										time.Sleep(100 * time.Millisecond)
										continue reconnectLoop
									} else {
										lg.Error("answer error", log.KV("code", awsErr.Code()), log.KV("message", awsErr.Message()), log.KV("shard", *shard.ShardId), log.KV("stream", stream.Stream_Name))
										time.Sleep(500 * time.Millisecond)
									}
								} else {
									lg.Error("unknown error", log.KVErr(err))
								}
							} else {
								// if we got no records, chill for a sec before we hit it again
								if len(res.Records) == 0 {
									time.Sleep(100 * time.Millisecond)
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
							if stream.Parse_Time == false {
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
								lg.Error("Failed to handle entry", log.KVErr(err))
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
						lg.Error("Failed to close processor set", log.KVErr(err))
					}
					// if we get to this point, exit the for loop
					break
				}
			}(*stream, *shard, tagid, i, tgr)
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

func debugout(format string, args ...interface{}) {
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
	stateFile.Read(&sm.states)
	return &sm
}

func (s *stateman) Start() {
	go func() {
		for {
			select {
			case <-time.After(15 * time.Second):
				s.Flush()
			}
		}
	}()
}

func (s *stateman) Close() {
	s.Flush()
}

func (s *stateman) Flush() {
	s.Lock()
	defer s.Unlock()
	s.stateFile.Write(s.states)
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
