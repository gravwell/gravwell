/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/entry"
	"github.com/gravwell/gravwell/v3/ingest/log"
	"github.com/gravwell/gravwell/v3/ingest/processors"
	"github.com/gravwell/gravwell/v3/ingest/processors/tags"
)

const (
	ipv4Len          = 4
	ipv6Len          = 16
	currKafkaVersion = `2.1.1`
	minTLSVersion    = tls.VersionTLS12
)

type closer interface {
	Close() error
}

type closers struct {
	mtx sync.Mutex
	wg  sync.WaitGroup
	set []closer
}

func newClosers() *closers {
	return &closers{}
}

func (c *closers) add(v closer) (wg *sync.WaitGroup) {
	if v == nil {
		return nil
	}
	c.mtx.Lock()
	c.set = append(c.set, v)
	wg = &c.wg
	c.mtx.Unlock()
	return
}

func (c *closers) Close() (err error) {
	c.mtx.Lock()
	for _, v := range c.set {
		err = appendError(err, v.Close())
	}
	c.wg.Wait()
	c.set = nil
	c.mtx.Unlock()
	return
}

func appendError(base, next error) error {
	if next == nil {
		return base
	} else if base == nil {
		return next
	}
	return fmt.Errorf("%v %v", base, next)
}

type kafkaConsumer struct {
	kafkaConsumerConfig
	mtx      sync.Mutex
	started  bool
	ctx      context.Context
	cf       context.CancelFunc
	count    uint
	size     uint
	memberId string
	src      net.IP
}

type kafkaConsumerConfig struct {
	consumerCfg
	defTag entry.EntryTag
	igst   *ingest.IngestMuxer
	lg     *log.Logger
	pproc  *processors.ProcessorSet
	tgr    *tags.Tagger
}

func newKafkaConsumer(cfg kafkaConsumerConfig) (kc *kafkaConsumer, err error) {
	if cfg.igst == nil {
		err = errors.New("nil ingest connection")
	} else if cfg.lg == nil {
		err = errors.New("nil logger")
	} else {
		kc = &kafkaConsumer{
			kafkaConsumerConfig: cfg,
		}
		kc.ctx, kc.cf = context.WithCancel(context.Background())
	}
	return
}

func (kc *kafkaConsumer) Start(wg *sync.WaitGroup) (err error) {
	kc.mtx.Lock()
	if kc.started {
		err = errors.New("already started")
	} else if kc.ctx == nil || kc.cf == nil {
		err = errors.New("closer context is nil, already closed")
	} else {
		cfg := sarama.NewConfig()
		if cfg.Version, err = sarama.ParseKafkaVersion(currKafkaVersion); err != nil {
			return
		}
		cfg.Consumer.Group.Rebalance.GroupStrategies = kc.strats
		cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

		if kc.useTLS {
			cfg.Net.TLS.Enable = true
			cfg.Net.TLS.Config = &tls.Config{
				MinVersion: minTLSVersion,
			}
			if kc.skipVerify {
				cfg.Net.TLS.Config.InsecureSkipVerify = true
			}
		}
		if err = kc.auth.SetAuth(cfg); err != nil {
			return
		}

		var clnt sarama.ConsumerGroup
		if clnt, err = sarama.NewConsumerGroup(kc.leader, kc.group, cfg); err != nil {
			return
		}
		wg.Add(1)
		kc.started = true
		go kc.routine(clnt, wg)
	}
	kc.mtx.Unlock()
	return
}

// close the connection
func (kc *kafkaConsumer) Close() (err error) {
	if kc == nil {
		err = errors.New("nil consumer")
	} else {
		kc.mtx.Lock()
		if kc.cf == nil {
			err = errors.New("nil closer conn, routine closed")
		} else {
			kc.cf()
			kc.cf = nil
		}
		kc.mtx.Unlock()
	}
	return
}

func (kc *kafkaConsumer) routine(client sarama.ConsumerGroup, wg *sync.WaitGroup) {
	defer wg.Done()
	var i int
	for {
		i++
		kc.lg.Info("consumer start", log.KV("attempt", i))
		if err := client.Consume(kc.ctx, []string{kc.topic}, kc); err != nil {
			kc.lg.Error("consumer error", log.KVErr(err))
			break
		}
		if kc.ctx.Err() != nil {
			break
		}
	}
}

// Setup can handle setup and gets a chance to fire up internal state prior to starting
func (kc *kafkaConsumer) Setup(cgs sarama.ConsumerGroupSession) (err error) {
	kc.lg.Info("Kafka consumer starting", log.KV("consumer", cgs.MemberID()))
	//update our member id and reset the count
	//also get a local handle on the ingest muxer and wait for a hot connection
	kc.mtx.Lock()
	kc.memberId = cgs.MemberID()
	kc.count = 0
	kc.size = 0
	igst := kc.igst
	kc.mtx.Unlock()
	kc.lg.Info("Kafka consumer waiting for hot ingester", log.KV("consumer", cgs.MemberID()))
	if err = igst.WaitForHotContext(kc.ctx, 0); err == nil {
		if ip, err := igst.SourceIP(); err == nil {
			kc.src = ip
		} else {
			kc.src = nil
		}
		kc.lg.Info("kafka consumer getting source", log.KV("consumer", cgs.MemberID()), log.KV("source", kc.src))
	}
	return
}

// Cleanup executes at the end of a session, this a chance to clean up and sync our ingester
func (kc *kafkaConsumer) Cleanup(cgs sarama.ConsumerGroupSession) (err error) {
	mid := cgs.MemberID()
	kc.lg.Info("kafka consumer cleaning up", log.KV("consumer", mid))
	//get a local handle on the ingest muxer
	kc.mtx.Lock()
	igst := kc.igst
	kc.mtx.Unlock()

	if igst != nil {
		igst.Info("kafka consumer stats",
			log.KV("consumer", mid),
			log.KV("group", kc.group),
			log.KV("member", kc.memberId),
			log.KV("count", kc.count),
			log.KV("size", kc.size))
		if err = igst.SyncContext(kc.ctx, 0); err != nil {
			kc.lg.Info("consumer cleanup sync failed", log.KV("consumer", mid), log.KVErr(err))
			//failing to sync should not return an error
			// the sarama library treats an error coming off of Cleanup as fatal and won't restart the worker
			// as a result any network bump that coincides with Kafka telling us to GTFO will cause an error here
			// which we don't want.  Older versions of kafka (2.X line) seem to boot clients when new clients in the same
			// consumer group connect as some sort of rebalance strategy or something.  So we need to basically always complete
			// cleanup with no error or the consumer won't reconnect
			err = nil
		} else {
			kc.lg.Info("Consumer cleanup complete", log.KV("consumer", mid))
		}
	}
	return
}

// ConsumeClaim actually eats entries from the session and writes them into our ingester
func (kc *kafkaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (err error) {
	//README the ConsumeClaim function is running in a go routine
	//it is entirely possible for multiple of these routines to be running at a time

	if claim.Topic() != kc.topic {
		return errors.New("Claim routine got the wrong topic")
	}

	tckr := time.NewTicker(time.Second)
	defer tckr.Stop()
	rch := claim.Messages()

	var currTS int64
	batch := make([]*sarama.ConsumerMessage, 0, kc.batchSize)

	kc.lg.Info("consumer started", log.KV("consumer", kc.memberId), log.KV("group", kc.group))
	var reason string
loop:
	for {
		select {
		case msg, ok := <-rch:
			if !ok {
				reason = `consumer group claim closed`
				break loop
			} else if msg == nil {
				continue
			}
			ts := msg.Timestamp.Unix()
			if currTS != ts && len(batch) > 0 {
				//flush the existing batch
				if err = kc.flush(session, batch); err != nil {
					kc.lg.Error("failed to write entries", log.KV("count", len(batch)), log.KVErr(err))
					reason = `consumer write failed on timestamp transition`
					break loop
				}
				batch = batch[0:0]
			}
			batch = append(batch, msg)
			currTS = ts
			//check if we hit capacity
			if len(batch) == cap(batch) {
				//flush the existing batch
				if err = kc.flush(session, batch); err != nil {
					kc.lg.Error("failed to write entries", log.KV("count", len(batch)), log.KVErr(err))
					reason = `consumer write failed on max-capacity write`
					break loop
				}
				currTS = 0
				batch = batch[0:0]
			}
		case <-tckr.C:
			if len(batch) > 0 {
				//flush the existing batch
				if err = kc.flush(session, batch); err != nil {
					kc.lg.Error("failed to write entries", log.KV("count", len(batch)), log.KVErr(err))
					reason = `consumer write failed on ticker`
					break loop
				}
				currTS = 0
				batch = batch[0:0]
			}
		}
	}
	//add the reason for exiting and an error if there is one, typically its just context cancelled but... maybe its something else
	if err != nil {
		kc.lg.Info("consumer exited with error",
			log.KV("consumer", kc.memberId),
			log.KV("group", kc.group),
			log.KV("exit-reason", reason),
			log.KVErr(err),
		)
	} else {
		kc.lg.Info("consumer exited",
			log.KV("consumer", kc.memberId),
			log.KV("group", kc.group),
			log.KV("exit-reason", reason),
		)
	}
	return
}

func (kc *kafkaConsumer) resolveTag(tn string) (tag entry.EntryTag, ok bool, err error) {
	if tag, err = kc.tgr.Negotiate(tn); err != nil {
		return
	}
	//don't have it, so check if its ok
	ok = kc.tgr.Allowed(tag)
	return
}

func (kc *kafkaConsumer) flush(session sarama.ConsumerGroupSession, msgs []*sarama.ConsumerMessage) (err error) {
	var sz uint
	var cnt uint
	for _, m := range msgs {
		ent := &entry.Entry{
			TS:   entry.FromStandard(m.Timestamp),
			Data: m.Value,
		}
		if kc.ignoreTS {
			ent.TS = entry.Now()
		} else if kc.extractTS && kc.tg != nil {
			var hts time.Time
			var ok bool
			if hts, ok, err = kc.tg.Extract(ent.Data); err != nil {
				kc.lg.Warn("catastrophic timegrinder error", log.KVErr(err))
			} else if ok {
				ent.TS = entry.FromStandard(hts)
			}
			// if not ok, we'll just use the timestamp
		}
		if ent.Tag, ent.SRC, err = kc.resolveSourceAndTag(m); err != nil {
			return
		}
		if err = kc.pproc.ProcessContext(ent, kc.ctx); err != nil {
			return
		}
		sz += uint(ent.Size())
		cnt++
	}
	if kc.sync {
		if err = kc.igst.SyncContext(kc.ctx, 0); err != nil {
			return
		}
	}
	//commit the messages
	for i := range msgs {
		session.MarkMessage(msgs[i], ``)
	}
	kc.count += cnt
	kc.size += sz
	return
}

func (kc *kafkaConsumer) resolveSourceAndTag(m *sarama.ConsumerMessage) (tag entry.EntryTag, ip net.IP, err error) {
	//short circuit out
	if m == nil {
		ip = kc.src
		tag = kc.defTag
		return
	}
	var tagHit bool

	for _, rh := range m.Headers {
		if string(rh.Key) == kc.srcKey {
			ip = kc.extractSrc(rh.Value)
		} else if string(rh.Key) == kc.tagKey {
			if tag, tagHit, err = kc.resolveTag(string(rh.Value)); err != nil {
				return
			}
		}
	}
	//if if we still missed, just use the src
	if ip == nil {
		ip = kc.src
	}
	if !tagHit {
		tag = kc.defTag
	}
	return
}

func (kc *kafkaConsumer) extractSrc(v []byte) (ip net.IP) {
	if kc.srcBin && (len(v) == ipv4Len || len(v) == ipv6Len) {
		ip = net.IP(v)
	} else {
		ip = net.ParseIP(string(v))
	}
	return
}
