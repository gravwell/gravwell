/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/ingesters/utils"
)

const (
	subscribeTimeout  = 10 * time.Second
	disconnectTimeout = 5 * time.Second
	connectTimeout    = 10 * time.Second
	// subscribeDenyDelay is the backoff used once the broker has told us a
	// filter is rejected for a reason retrying cannot fix.
	subscribeDenyDelay = 5 * time.Minute
)

var (
	ErrAlreadyStarted = errors.New("subscriber already started")
)

// syncer is the slice of *ingest.IngestMuxer the subscriber actually needs,
// broken out so the handler can be tested without a live muxer.
type syncer interface {
	SyncContext(ctx context.Context, to time.Duration) error
}

// acker is the slice of *paho.Client the handler needs.
type acker interface {
	Ack(*paho.Publish) error
}

// runtimeRoute is a Topic-Route with its tag already negotiated.
type runtimeRoute struct {
	pattern string
	glob    glob.Glob
	tag     entry.EntryTag
	drop    bool
}

type subscriberConfig struct {
	cfg    *subCfg
	igst   syncer
	proc   *processors.ProcessorSet
	defTag entry.EntryTag
	routes []runtimeRoute
	src    net.IP
	lg     *log.Logger
}

type mqttSubscriber struct {
	subscriberConfig
	mtx     sync.Mutex
	started bool
	ctx     context.Context
	cf      context.CancelFunc
	cm      *autopaho.ConnectionManager

	subMtx    sync.Mutex
	subCancel context.CancelFunc
}

func newSubscriber(sc subscriberConfig) (*mqttSubscriber, error) {
	if sc.cfg == nil {
		return nil, errors.New("nil subscription config")
	} else if sc.proc == nil {
		return nil, errors.New("nil processor set")
	} else if sc.lg == nil {
		return nil, errors.New("nil logger")
	}
	s := &mqttSubscriber{subscriberConfig: sc}
	s.ctx, s.cf = context.WithCancel(context.Background())
	return s, nil
}

func (s *mqttSubscriber) clientConfig() autopaho.ClientConfig {
	return autopaho.ClientConfig{
		ServerUrls:                    s.cfg.brokers,
		TlsCfg:                        s.cfg.tlsCfg,
		KeepAlive:                     s.cfg.keepAlive,
		CleanStartOnInitialConnection: false, // we want a durable session
		SessionExpiryInterval:         s.cfg.sessionExpiry,
		ConnectTimeout:                connectTimeout,
		ReconnectBackoff:              autopaho.DefaultExponentialBackoff(),
		ConnectUsername:               s.cfg.username,
		ConnectPassword:               []byte(s.cfg.password),
		OnConnectionUp:                s.onConnectionUp,
		OnConnectionDown:              s.onConnectionDown,
		OnConnectError:                s.onConnectError,
		ConnectPacketBuilder:          s.buildConnect,
		ClientConfig: paho.ClientConfig{
			ClientID: s.cfg.clientID,
			// Manual acks are what let us hold off acknowledging a message
			// until it has actually been handed to the muxer.
			EnableManualAcknowledgment: true,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				s.handle,
			},
			OnClientError: s.onClientError,
		},
	}
}

// buildConnect sets Receive Maximum on the CONNECT packet. This matters for
// two reasons: it tells the broker how many unacknowledged QoS 1/2 messages it
// may have outstanding (our backpressure), and it sizes paho's internal
// inbound publish channel, which otherwise defaults to 65535 entries.
func (s *mqttSubscriber) buildConnect(c *paho.Connect, u *url.URL) (*paho.Connect, error) {
	if c == nil {
		return nil, errors.New("nil connect packet")
	}
	if c.Properties == nil {
		c.Properties = &paho.ConnectProperties{}
	}
	rm := s.cfg.maxInFlight
	c.Properties.ReceiveMaximum = &rm
	return c, nil
}

func (s *mqttSubscriber) Start(wg *sync.WaitGroup) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.started {
		return ErrAlreadyStarted
	}
	cm, err := autopaho.NewConnection(s.ctx, s.clientConfig())
	if err != nil {
		return fmt.Errorf("failed to start mqtt connection: %w", err)
	}
	s.cm = cm
	s.started = true
	if wg != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-cm.Done()
		}()
	}
	return nil
}

// Close stops the subscriber.
//
// The context is cancelled FIRST, before disconnecting. Disconnect waits for
// paho's workers to wind up, and one of those workers is the publish router
// running our handler, which may be parked in a retry backoff. Cancelling
// first is what releases it; disconnecting first deadlocks until the
// disconnect timeout expires. Anything in flight at that point is not
// acknowledged and will be redelivered.
func (s *mqttSubscriber) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.cf()
	if !s.started {
		return nil
	}
	s.started = false

	var err error
	if s.cm != nil {
		ctx, cf := context.WithTimeout(context.Background(), disconnectTimeout)
		err = s.cm.Disconnect(ctx)
		cf()
	}
	return err
}

func (s *mqttSubscriber) onConnectionUp(cm *autopaho.ConnectionManager, connack *paho.Connack) {
	s.lg.Info("mqtt connection established", log.KV("subscription", s.cfg.name))
	// OnConnectionUp must not block, so the subscribe happens in its own
	// goroutine. autopaho calls this on every reconnect, which is what
	// re-establishes the subscriptions. Each call supersedes the last: a
	// flapping broker would otherwise pile up goroutines, each waking from a
	// backoff to issue a redundant SUBSCRIBE against whatever connection has
	// since become current.
	ctx := s.restartSubscribe()
	go s.subscribe(ctx, cm)
}

// restartSubscribe cancels any in-flight subscribe attempt and returns the
// context governing the new one.
func (s *mqttSubscriber) restartSubscribe() context.Context {
	s.subMtx.Lock()
	defer s.subMtx.Unlock()
	if s.subCancel != nil {
		s.subCancel()
	}
	ctx, cf := context.WithCancel(s.ctx)
	s.subCancel = cf
	return ctx
}

// subscribe issues the SUBSCRIBE and keeps retrying until it lands.
//
// Retrying matters because autopaho only calls OnConnectionUp again on a
// reconnect. Giving up after one failure would leave a perfectly healthy MQTT
// session with no subscriptions on it, ingesting nothing, with a single log
// line as the only evidence. Note that paho returns an error if ANY topic in a
// multi-topic SUBSCRIBE comes back with a failure reason code, so one denied
// topic would otherwise take the whole subscription down with it.
func (s *mqttSubscriber) subscribe(ctx context.Context, cm *autopaho.ConnectionManager) {
	subs := make([]paho.SubscribeOptions, 0, len(s.cfg.topics))
	for _, t := range s.cfg.topics {
		subs = append(subs, paho.SubscribeOptions{
			Topic: t,
			QoS:   s.cfg.qos,
		})
	}
	topics := strings.Join(s.cfg.topics, `,`)

	delay := retryInitialDelay
	for attempt := 1; ; attempt++ {
		sctx, cf := context.WithTimeout(ctx, subscribeTimeout)
		sa, err := cm.Subscribe(sctx, &paho.Subscribe{Subscriptions: subs})
		cf()
		if err == nil {
			s.lg.Info("mqtt subscribed",
				log.KV("subscription", s.cfg.name),
				log.KV("topics", topics),
				log.KV("qos", s.cfg.qos))
			return
		}
		if ctx.Err() != nil {
			return // shutting down, or superseded by a newer connection
		}
		if errors.Is(err, paho.ErrConnectionLost) {
			// the reconnect will call OnConnectionUp again, which starts a
			// fresh attempt, so this goroutine is done
			s.lg.Warn("subscribe abandoned, connection lost",
				log.KV("subscription", s.cfg.name),
				log.KV("topics", topics))
			return
		}

		// paho collapses every per-topic failure into one generic error, so
		// dig the reason codes out of the SUBACK to say what actually failed.
		granted, rejected, permanent := inspectSuback(sa, s.cfg.topics)
		if len(rejected) > 0 {
			s.lg.Error("broker rejected topic filters",
				log.KV("subscription", s.cfg.name),
				log.KV("rejected", strings.Join(rejected, `,`)),
				log.KV("granted", granted))
		}
		if granted > 0 {
			// the granted filters are live and ingesting, re-subscribing
			// would not change the rejected ones
			s.lg.Error("continuing with a partial subscription",
				log.KV("subscription", s.cfg.name),
				log.KV("granted", granted),
				log.KV("rejected", len(rejected)))
			return
		}
		if permanent {
			// not authorized, invalid filter, shared subs unsupported and the
			// like. Retrying at the normal cadence is pure log noise, but an
			// operator may still fix the ACL without restarting us, so back
			// off hard rather than giving up.
			delay = subscribeDenyDelay
		}
		s.lg.Error("failed to subscribe, retrying",
			log.KV("subscription", s.cfg.name),
			log.KV("topics", topics),
			log.KV("attempt", attempt),
			log.KV("retryin", delay),
			log.KVErr(err))
		if utils.QuitableSleep(ctx, delay) {
			return
		}
		if !permanent {
			delay = nextDelay(delay)
		}
	}
}

// inspectSuback reports how many filters the broker granted, which it
// rejected, and whether every rejection is one that retrying cannot fix.
func inspectSuback(sa *paho.Suback, topics []string) (granted int, rejected []string, permanent bool) {
	if sa == nil {
		return
	}
	permanent = true
	for i, code := range sa.Reasons {
		if code < 0x80 {
			granted++
			continue
		}
		topic := `?`
		if i < len(topics) {
			topic = topics[i]
		}
		rejected = append(rejected, fmt.Sprintf("%s(0x%02x %s)", topic, code, subackReason(code)))
		if !permanentSubackCode(code) {
			permanent = false
		}
	}
	if len(rejected) == 0 {
		permanent = false
	}
	return
}

// permanentSubackCode reports whether a SUBACK failure code describes a
// configuration problem rather than a transient broker condition.
func permanentSubackCode(code byte) bool {
	switch code {
	case 0x87, // Not authorized
		0x8F, // Topic Filter invalid
		0x9E, // Shared Subscriptions not supported
		0xA1, // Subscription Identifiers not supported
		0xA2: // Wildcard Subscriptions not supported
		return true
	}
	return false
}

func subackReason(code byte) string {
	switch code {
	case 0x80:
		return `unspecified error`
	case 0x83:
		return `implementation specific error`
	case 0x87:
		return `not authorized`
	case 0x8F:
		return `topic filter invalid`
	case 0x91:
		return `packet identifier in use`
	case 0x97:
		return `quota exceeded`
	case 0x9E:
		return `shared subscriptions not supported`
	case 0xA1:
		return `subscription identifiers not supported`
	case 0xA2:
		return `wildcard subscriptions not supported`
	}
	return `unknown`
}

func (s *mqttSubscriber) onConnectionDown() bool {
	s.lg.Warn("mqtt connection lost", log.KV("subscription", s.cfg.name))
	return true // keep trying to reconnect
}

func (s *mqttSubscriber) onConnectError(err error) {
	s.lg.Error("mqtt connection attempt failed",
		log.KV("subscription", s.cfg.name), log.KVErr(err))
}

func (s *mqttSubscriber) onClientError(err error) {
	s.lg.Error("mqtt client error",
		log.KV("subscription", s.cfg.name), log.KVErr(err))
}
