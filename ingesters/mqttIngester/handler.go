/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

// Reserved enumerated value names. A v5 User Property using one of these is
// skipped so that a publisher cannot shadow the metadata we attach.
const (
	evTopic       = `topic`
	evQoS         = `qos`
	evRetain      = `retain`
	evContentType = `content-type`
)

const (
	retryInitialDelay = 100 * time.Millisecond
	retryMaxDelay     = 10 * time.Second
)

var reservedEVNames = map[string]struct{}{
	evTopic:       {},
	evQoS:         {},
	evRetain:      {},
	evContentType: {},
}

// handle is the paho OnPublishReceived callback. It runs on paho's publish
// router goroutine, which is fed by a channel bounded by Receive Maximum, so
// blocking here is exactly the backpressure we want.
func (s *mqttSubscriber) handle(pr paho.PublishReceived) (bool, error) {
	var a acker
	if pr.Client != nil {
		a = pr.Client
	}
	return s.handlePublish(pr.Packet, a)
}

// handlePublish does the real work: route, build, write, then acknowledge.
//
// Every path out of here either acknowledges the message or is a shutdown.
// That is deliberate: paho acknowledges packets strictly in arrival order
// (see acksTracker.flush), so a packet we simply never ack blocks every PUBACK
// queued behind it. Once Max-In-Flight messages have piled up the broker stops
// delivering and the subscription is silently dead until something forces a
// reconnect. So transient failures are retried here rather than returned, and
// an entry that can never be written is dropped loudly instead of wedging the
// subscription.
func (s *mqttSubscriber) handlePublish(p *paho.Publish, a acker) (bool, error) {
	if p == nil {
		return false, nil
	}
	if err := s.ctx.Err(); err != nil {
		return true, err
	}

	tag, drop := s.resolveTag(p.Topic)
	if drop {
		// A Topic-Route with an empty tag means discard. The message was
		// handled, so acknowledge it rather than forcing redelivery.
		s.ack(p, a)
		return true, nil
	}

	ent := s.buildEntry(p, tag)

	// ProcessContext is not idempotent, a retry after a partial success would
	// duplicate the entry, so the write and the sync are retried separately.
	if err := s.retry(`ingest entry`, p.Topic, func() error {
		return s.proc.ProcessContext(ent, s.ctx)
	}); err != nil {
		if isPermanent(err) {
			s.lg.Error("dropping entry that can never be written",
				log.KV("subscription", s.cfg.name),
				log.KV("topic", p.Topic),
				log.KV("size", len(p.Payload)),
				log.KVErr(err))
			s.ack(p, a)
			return true, nil
		}
		return true, err // shutting down, let the broker redeliver
	}

	if s.cfg.sync && s.igst != nil {
		// Sync is idempotent so retrying it is safe.
		if err := s.retry(`sync muxer`, p.Topic, func() error {
			return s.igst.SyncContext(s.ctx, 0)
		}); err != nil {
			return true, err
		}
	}

	s.ack(p, a)
	return true, nil
}

// retry runs fn until it succeeds, the error is permanent, or we shut down.
func (s *mqttSubscriber) retry(what, topic string, fn func() error) error {
	delay := retryInitialDelay
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			if attempt > 1 {
				s.lg.Info("recovered after retry",
					log.KV("subscription", s.cfg.name),
					log.KV("operation", what),
					log.KV("attempts", attempt))
			}
			return nil
		}
		if cerr := s.ctx.Err(); cerr != nil {
			return cerr
		}
		if isPermanent(err) {
			return err
		}
		s.lg.Error("operation failed, retrying",
			log.KV("subscription", s.cfg.name),
			log.KV("operation", what),
			log.KV("topic", topic),
			log.KV("attempt", attempt),
			log.KV("retryin", delay),
			log.KVErr(err))
		if utils.QuitableSleep(s.ctx, delay) {
			return s.ctx.Err()
		}
		delay = nextDelay(delay)
	}
}

func nextDelay(d time.Duration) time.Duration {
	if d *= 2; d > retryMaxDelay {
		return retryMaxDelay
	}
	return d
}

// isPermanent reports whether retrying could ever succeed. An oversized entry
// or an unknown tag fails identically every time, so retrying it forever would
// stall the subscription on a single poison message.
func isPermanent(err error) bool {
	return errors.Is(err, ingest.ErrOversizedEntry) || errors.Is(err, ingest.ErrUnknownTag)
}

// resolveTag walks the compiled Topic-Route list in config order, first match
// wins, falling back to the subscription default tag.
func (s *mqttSubscriber) resolveTag(topic string) (tag entry.EntryTag, drop bool) {
	for _, r := range s.routes {
		if r.glob == nil || !r.glob.Match(topic) {
			continue
		}
		if r.drop {
			debugout("dropping %q via Topic-Route %q\n", topic, r.pattern)
			return 0, true
		}
		return r.tag, false
	}
	return s.defTag, false
}

func (s *mqttSubscriber) buildEntry(p *paho.Publish, tag entry.EntryTag) *entry.Entry {
	ent := &entry.Entry{
		Data: p.Payload,
		Tag:  tag,
		SRC:  s.src,
		TS:   entry.Now(),
	}
	if !s.cfg.ignoreTS && s.cfg.extractTS && s.cfg.tg != nil {
		if ts, ok, err := s.cfg.tg.Extract(p.Payload); err != nil {
			s.lg.Warn("catastrophic timegrinder error",
				log.KV("subscription", s.cfg.name), log.KVErr(err))
		} else if ok {
			ent.TS = entry.FromStandard(ts)
		}
	}
	if s.cfg.attachMeta {
		s.attachMetadata(ent, p)
	}
	return ent
}

// attachMetadata hangs the MQTT framing off the entry as enumerated values so
// that the payload itself stays byte for byte what the publisher sent.
func (s *mqttSubscriber) attachMetadata(ent *entry.Entry, p *paho.Publish) {
	s.addEV(ent, evTopic, p.Topic)
	s.addEV(ent, evQoS, p.QoS)
	s.addEV(ent, evRetain, p.Retain)
	if p.Properties == nil {
		return
	}
	if p.Properties.ContentType != `` {
		s.addEV(ent, evContentType, p.Properties.ContentType)
	}
	for _, up := range p.Properties.User {
		if _, reserved := reservedEVNames[up.Key]; reserved {
			debugout("skipping user property %q on %q, reserved name\n", up.Key, p.Topic)
			continue
		}
		if len(up.Key) == 0 || len(up.Key) > entry.MaxEvNameLength {
			debugout("skipping user property on %q, invalid name length %d\n", p.Topic, len(up.Key))
			continue
		}
		s.addEV(ent, up.Key, up.Value)
	}
}

func (s *mqttSubscriber) addEV(ent *entry.Entry, name string, val interface{}) {
	if err := ent.AddEnumeratedValueEx(name, val); err != nil {
		debugout("failed to attach enumerated value %q: %v\n", name, err)
	}
}

// ack acknowledges a QoS 1/2 message. QoS 0 messages carry no acknowledgement.
//
// The context check only covers our own shutdown: an ack issued while we are
// tearing down would race the disconnect, and the broker redelivering is the
// behaviour we want there. It does not cover a broker-initiated disconnect,
// where paho has already reset its ack tracker and the Ack call returns
// ErrPacketNotFound, which is logged and harmless.
func (s *mqttSubscriber) ack(p *paho.Publish, a acker) {
	if p == nil || p.QoS == 0 || a == nil {
		return
	}
	if s.ctx.Err() != nil {
		return
	}
	if err := a.Ack(p); err != nil {
		s.lg.Warn("failed to acknowledge mqtt message",
			log.KV("subscription", s.cfg.name),
			log.KV("topic", p.Topic),
			log.KVErr(err))
	}
}
