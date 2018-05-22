/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
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
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/gravwell/ingest"
	"github.com/gravwell/ingest/entry"
	"github.com/gravwell/ingest/log"
	"github.com/gravwell/timegrinder"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
)

const (
	defaultConfigLoc = `/opt/gravwell/etc/kinesis_ingest.conf`
)

var (
	configLoc      = flag.String("config", defaultConfigLoc, "Location of configuration file")
	verbose        = flag.Bool("v", false, "Display verbose status updates to stdout")
	stderrOverride = flag.String("stderr", "", "Redirect stderr to a shared memory file")
	lg             *log.Logger
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
		lg.Fatal("Failed to get tags from configuration: %v", err)
	}
	conns, err := cfg.Targets()
	if err != nil {
		lg.Fatal("Failed to get backend targets from configuration: %s", err)
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
		lg.Fatal("Failed build our ingest system: %v", err)
	}
	defer igst.Close()
	debugout("Starting ingester muxer\n")
	if err := igst.Start(); err != nil {
		lg.FatalCode(0, "Failed start our ingest system: %v", err)
		return
	}

	debugout("Waiting for connections to indexers ... ")
	if err := igst.WaitForHot(cfg.Timeout()); err != nil {
		lg.FatalCode(0, "Timedout waiting for backend connections: %v\n", err)
	}
	debugout("Successfully connected to ingesters\n")

	// Set up environment variables for AWS auth, if extant
	if cfg.Global.AWS_Access_Key_ID != "" {
		os.Setenv("AWS_ACCESS_KEY_ID", cfg.Global.AWS_Access_Key_ID)
	}
	if cfg.Global.AWS_Secret_Access_Key != "" {
		os.Setenv("AWS_SECRET_ACCESS_KEY", cfg.Global.AWS_Secret_Access_Key)
	}

	// make an aws session
	sess := session.Must(session.NewSession())

	for _, stream := range cfg.KinesisStream {
		tagid, err := igst.GetTag(stream.Tag_Name)
		if err != nil {
			lg.Fatal("Can't resolve tag %v: %v", stream.Tag_Name, err)
		}

		// get a handle on kinesis
		svc := kinesis.New(sess, aws.NewConfig().WithRegion(stream.Region))

		// Get the list of shards
		shards := []*kinesis.Shard{}
		dsi := &kinesis.DescribeStreamInput{}
		dsi.SetStreamName(stream.Stream_Name)
		for {
			streamdesc, err := svc.DescribeStream(dsi)
			if err != nil {
				lg.Error("Failed to get stream description: %v", err)
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

		for i, shard := range shards {
			go func(shard *kinesis.Shard, tagid entry.EntryTag, shardid int) {
				gsii := &kinesis.GetShardIteratorInput{}
				gsii.SetShardId(*shard.ShardId)
				gsii.SetShardIteratorType(stream.Iterator_Type)
				gsii.SetStreamName(stream.Stream_Name)

				output, err := svc.GetShardIterator(gsii)
				if err != nil {
					lg.Error("error on shard #%d (%s): %v", shardid, *shard.ShardId, err)
					return
				}
				iter := *output.ShardIterator
				eChan := make(chan *entry.Entry, 2048)
				tcfg := timegrinder.Config{
					EnableLeftMostSeed: true,
				}
				tg, err := timegrinder.NewTimeGrinder(tcfg)
				if err != nil {
					stream.Parse_Time = false
				}
				if stream.Assume_Localtime {
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

				go func(c chan *entry.Entry) {
					for e := range c {
						if err := igst.WriteEntry(e); err != nil {
							lg.Error("Can't write entry: %v", err)
						}
					}
				}(eChan)
				for {
					gri := &kinesis.GetRecordsInput{}
					gri.SetShardIterator(iter)
					var res *kinesis.GetRecordsOutput
					var err error
					for {
						res, err = svc.GetRecords(gri)
						if err != nil {
							if awsErr, ok := err.(awserr.Error); ok {
								// process SDK error
								if awsErr.Code() == kinesis.ErrCodeProvisionedThroughputExceededException {
									lg.Warn("Throughput exceeded, trying again")
									time.Sleep(500 * time.Millisecond)
								} else {
									lg.Error("%s: %s", awsErr.Code(), awsErr.Message())
									time.Sleep(100 * time.Millisecond)
								}
							} else {
								lg.Error("unknown error: %v", err)
							}
						} else {
							break
						}
					}

					iter = *res.NextShardIterator
					for _, r := range res.Records {
						ent := &entry.Entry{
							Data: r.Data,
							Tag:  tagid,
							SRC:  src,
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
						eChan <- ent
					}
				}
			}(shard, tagid, i)
		}
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
