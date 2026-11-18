/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mqtt

import (
	"errors"

	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

// Reserved enumerated value names. A v5 User Property using one of these is
// skipped so that a publisher cannot shadow the metadata we attach.
const (
	evTopic       = `topic`
	evQoS         = `qos`
	evRetain      = `retain`
	evContentType = `content-type`
)

var reservedEVNames = map[string]struct{}{
	evTopic:       {},
	evQoS:         {},
	evRetain:      {},
	evContentType: {},
}

// acker is the slice of *paho.Client the handler needs.
type acker interface {
	Ack(*paho.Publish) error
}

// handle is the paho OnPublishReceived callback. It runs on paho's publish
// router goroutine, which is fed by a channel bounded by Receive Maximum, so
// blocking here is exactly the backpressure we want.
//
// The panic guard is not optional: this runs on a goroutine the paho library
// owns, which the runner's own recover does not cover, so a panic here would
// take the whole hosted runner down.
func (s *subscriber) handle(pr paho.PublishReceived) (handled bool, err error) {
	s.guard("publish handler", func() {
		var a acker
		if pr.Client != nil {
			a = pr.Client
		}
		handled, err = s.handlePublish(pr.Packet, a)
	})
	return
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
func (s *subscriber) handlePublish(p *paho.Publish, a acker) (bool, error) {
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
	if err := s.retry(`write entry`, p.Topic, func() error {
		return s.rt.Write(*ent)
	}); err != nil {
		if isPermanent(err) {
			s.rt.Error("dropping entry that can never be written",
				log.KV("subscription", s.name),
				log.KV("topic", p.Topic),
				log.KV("size", len(p.Payload)),
				log.KVErr(err))
			s.ack(p, a)
			return true, nil
		}
		return true, err // shutting down, let the broker redeliver
	}

	s.ack(p, a)
	return true, nil
}

// retry runs fn until it succeeds, the error is permanent, or we shut down.
func (s *subscriber) retry(what, topic string, fn func() error) error {
	delay := retryInitialDelay
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			if attempt > 1 {
				s.rt.Info("recovered after retry",
					log.KV("subscription", s.name),
					log.KV("operation", what),
					log.KV("attempts", attempt))
				s.rt.ClearError()
			}
			return nil
		}
		if cerr := s.ctx.Err(); cerr != nil {
			return cerr
		}
		if isPermanent(err) {
			return err
		}
		s.rt.Error("operation failed, retrying",
			log.KV("subscription", s.name),
			log.KV("operation", what),
			log.KV("topic", topic),
			log.KV("attempt", attempt),
			log.KV("retryin", delay),
			log.KVErr(err))
		s.rt.SetError(err)
		if s.rt.Sleep(delay) {
			return s.ctx.Err()
		}
		delay = nextDelay(delay)
	}
}

// isPermanent reports whether retrying could ever succeed. An oversized entry
// or an unknown tag fails identically every time, so retrying it forever would
// stall the subscription on a single poison message.
func isPermanent(err error) bool {
	return errors.Is(err, ingest.ErrOversizedEntry) || errors.Is(err, ingest.ErrUnknownTag)
}

// resolveTag walks the compiled Topic-Route list in config order, first match
// wins, falling back to the subscription default tag.
func (s *subscriber) resolveTag(topic string) (tag entry.EntryTag, drop bool) {
	for _, r := range s.routes {
		if r.glob == nil || !r.glob.Match(topic) {
			continue
		}
		if r.drop {
			return 0, true
		}
		return r.tag, false
	}
	return s.defTag, false
}

func (s *subscriber) buildEntry(p *paho.Publish, tag entry.EntryTag) *entry.Entry {
	ent := &entry.Entry{
		Data: p.Payload,
		Tag:  tag,
		SRC:  s.conf.srcOverride,
		TS:   entry.Now(),
	}
	if !s.conf.Ignore_Timestamps && s.tg != nil {
		if ts, ok, err := s.tg.Extract(p.Payload); err != nil {
			s.rt.Warn("catastrophic timegrinder error",
				log.KV("subscription", s.name), log.KVErr(err))
		} else if ok {
			ent.TS = entry.FromStandard(ts)
		}
	}
	if !s.conf.Disable_Metadata {
		s.attachMetadata(ent, p)
	}
	return ent
}

// attachMetadata hangs the MQTT framing off the entry as enumerated values so
// that the payload itself stays byte for byte what the publisher sent.
func (s *subscriber) attachMetadata(ent *entry.Entry, p *paho.Publish) {
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
			continue // a publisher does not get to shadow the real topic
		}
		if len(up.Key) == 0 || len(up.Key) > entry.MaxEvNameLength {
			continue
		}
		s.addEV(ent, up.Key, up.Value)
	}
}

func (s *subscriber) addEV(ent *entry.Entry, name string, val interface{}) {
	if err := ent.AddEnumeratedValueEx(name, val); err != nil {
		s.rt.Debug("failed to attach enumerated value",
			log.KV("subscription", s.name), log.KV("name", name), log.KVErr(err))
	}
}

// ack acknowledges a QoS 1/2 message. QoS 0 messages carry no acknowledgement.
//
// The context check only covers our own shutdown: an ack issued while we are
// tearing down would race the disconnect, and the broker redelivering is the
// behaviour we want there.
func (s *subscriber) ack(p *paho.Publish, a acker) {
	if p == nil || p.QoS == 0 || a == nil {
		return
	}
	if s.ctx.Err() != nil {
		return
	}
	if err := a.Ack(p); err != nil {
		s.rt.Warn("failed to acknowledge mqtt message",
			log.KV("subscription", s.name),
			log.KV("topic", p.Topic),
			log.KVErr(err))
	}
}
