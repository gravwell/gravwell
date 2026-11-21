/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mqtt

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	subscribeTimeout  = 10 * time.Second
	disconnectTimeout = 5 * time.Second
	connectTimeout    = 10 * time.Second

	retryInitialDelay = 100 * time.Millisecond
	retryMaxDelay     = 10 * time.Second

	// subscribeDenyDelay is the backoff used once the broker has told us a
	// filter is rejected for a reason retrying cannot fix.
	subscribeDenyDelay = 5 * time.Minute
)

// runtimeRoute is a Topic-Route with its tag already negotiated.
type runtimeRoute struct {
	pattern string
	glob    glob.Glob
	tag     entry.EntryTag
	drop    bool
}

// Ingester is the long lived MQTT subscription. It implements hosted.Ingester
// rather than hosted.Job because an MQTT subscription is a persistent push
// connection, not something that can be polled once and put down.
type Ingester struct {
	conf     Config
	name     string
	clientID string
	defTag   entry.EntryTag
	routes   []runtimeRoute
}

var _ hosted.Ingester = (*Ingester)(nil) // compile time interface check

// New builds the ingester, negotiating every tag it may write to up front.
// Tags are resolved here rather than in Run so that a bad tag is a startup
// failure instead of an ingester that restarts once a minute forever.
func New(conf Config, name string, tn hosted.TagNegotiator) (*Ingester, error) {
	if tn == nil {
		return nil, errors.New("nil tag negotiator")
	}
	i := &Ingester{
		conf:     conf,
		name:     name,
		clientID: conf.ClientID(name),
	}

	var err error
	if i.defTag, err = tn.NegotiateTag(conf.ResolveTag(entry.DefaultTagName)); err != nil {
		return nil, fmt.Errorf("negotiate default tag: %w", err)
	}
	for _, r := range conf.routes {
		rt := runtimeRoute{pattern: r.pattern, glob: r.glob, drop: r.drop}
		if !r.drop {
			if rt.tag, err = tn.NegotiateTag(r.tag); err != nil {
				return nil, fmt.Errorf("negotiate Topic-Route tag %q: %w", r.tag, err)
			}
		}
		i.routes = append(i.routes, rt)
	}
	return i, nil
}

// subscriber is the per-Run state. Run may be called again after a restart, so
// nothing that belongs to a single connection attempt lives on the Ingester.
type subscriber struct {
	*Ingester
	ctx context.Context
	rt  hosted.Runtime
	tg  *timegrinder.TimeGrinder
}

// Run holds the MQTT connection open until the context is cancelled.
//
// It must block: the runner restarts a Run that returns early, even with a nil
// error, after a flat one minute delay. It must also return PROMPTLY once the
// context is cancelled, because NativeRunner.Close waits on it with no timeout
// and a Run that ignores cancellation hangs the whole process on shutdown.
func (i *Ingester) Run(ctx context.Context, rt hosted.Runtime) error {
	tg, err := i.conf.newTimeGrinder()
	if err != nil {
		return err
	}
	s := &subscriber{Ingester: i, ctx: ctx, rt: rt, tg: tg}

	cm, err := autopaho.NewConnection(ctx, s.clientConfig())
	if err != nil {
		return fmt.Errorf("failed to start mqtt connection: %w", err)
	}
	defer func() {
		// a fresh context, the one we were handed is already cancelled by the
		// time we get here and DISCONNECT still needs to go out
		dctx, dcf := context.WithTimeout(context.Background(), disconnectTimeout)
		if derr := cm.Disconnect(dctx); derr != nil {
			rt.Warn("mqtt disconnect did not complete cleanly",
				log.KV("subscription", i.name), log.KVErr(derr))
		}
		dcf()
	}()

	select {
	case <-ctx.Done():
	case <-cm.Done():
		// autopaho gave up on its own, let the runner restart us
		if ctx.Err() == nil {
			return errors.New("mqtt connection manager terminated")
		}
	}
	// a cancelled context is a clean stop. Returning ctx.Err() here would have
	// the runner log it as an ingester failure and flag an error status on
	// every shutdown and every config reload.
	return nil
}

func (s *subscriber) clientConfig() autopaho.ClientConfig {
	return autopaho.ClientConfig{
		ServerUrls:                    s.conf.brokers,
		TlsCfg:                        s.conf.tlsCfg,
		KeepAlive:                     s.conf.keepAlive,
		CleanStartOnInitialConnection: false, // we want a durable session
		SessionExpiryInterval:         s.conf.sessionExpiry,
		ConnectTimeout:                connectTimeout,
		ReconnectBackoff:              autopaho.DefaultExponentialBackoff(),
		ConnectUsername:               s.conf.Username,
		ConnectPassword:               []byte(s.conf.Password),
		OnConnectionUp:                s.onConnectionUp,
		OnConnectionDown:              s.onConnectionDown,
		OnConnectError:                s.onConnectError,
		ConnectPacketBuilder:          s.buildConnect,
		ClientConfig: paho.ClientConfig{
			ClientID: s.clientID,
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
func (s *subscriber) buildConnect(c *paho.Connect, u *url.URL) (*paho.Connect, error) {
	if c == nil {
		return nil, errors.New("nil connect packet")
	}
	if c.Properties == nil {
		c.Properties = &paho.ConnectProperties{}
	}
	rm := s.conf.maxInFlight
	c.Properties.ReceiveMaximum = &rm
	return c, nil
}

func (s *subscriber) onConnectionUp(cm *autopaho.ConnectionManager, connack *paho.Connack) {
	s.rt.Info("mqtt connection established", log.KV("subscription", s.name))
	s.rt.ClearError()
	s.rt.ClearWarn()
	// OnConnectionUp must not block, so the subscribe happens in its own
	// goroutine. autopaho calls this on every reconnect, which is what
	// re-establishes the subscriptions.
	go s.guard("subscribe", func() { s.subscribe(cm) })
}

// subscribe issues the SUBSCRIBE and keeps retrying until it lands.
//
// Retrying matters because autopaho only calls OnConnectionUp again on a
// reconnect. Giving up after one failure would leave a perfectly healthy MQTT
// session with no subscriptions on it, ingesting nothing, with a single log
// line as the only evidence.
func (s *subscriber) subscribe(cm *autopaho.ConnectionManager) {
	subs := make([]paho.SubscribeOptions, 0, len(s.conf.topics))
	for _, t := range s.conf.topics {
		subs = append(subs, paho.SubscribeOptions{Topic: t, QoS: s.conf.qos})
	}
	topics := strings.Join(s.conf.topics, `,`)

	delay := retryInitialDelay
	for attempt := 1; ; attempt++ {
		sctx, cf := context.WithTimeout(s.ctx, subscribeTimeout)
		sa, err := cm.Subscribe(sctx, &paho.Subscribe{Subscriptions: subs})
		cf()
		if err == nil {
			s.rt.Info("mqtt subscribed",
				log.KV("subscription", s.name),
				log.KV("topics", topics),
				log.KV("qos", s.conf.qos))
			s.rt.ClearError()
			return
		}
		if s.ctx.Err() != nil {
			return // shutting down
		}
		if errors.Is(err, paho.ErrConnectionLost) || errors.Is(err, autopaho.ConnectionDownError) {
			// the reconnect will call OnConnectionUp again, which starts a
			// fresh attempt, so this goroutine is done
			s.rt.Warn("subscribe abandoned, connection lost",
				log.KV("subscription", s.name), log.KV("topics", topics))
			return
		}

		// paho collapses every per-topic failure into one generic error, so
		// dig the reason codes out of the SUBACK to say what actually failed.
		granted, rejected, permanent := inspectSuback(sa, s.conf.topics)
		if len(rejected) > 0 {
			s.rt.Error("broker rejected topic filters",
				log.KV("subscription", s.name),
				log.KV("rejected", strings.Join(rejected, `,`)),
				log.KV("granted", granted))
		}
		if granted > 0 {
			// the granted filters are live and ingesting, re-subscribing
			// would not change the rejected ones
			s.rt.SetWarn(fmt.Sprintf("partial subscription, %d of %d topic filters rejected",
				len(rejected), len(s.conf.topics)))
			return
		}
		if permanent {
			// not authorized, invalid filter, shared subs unsupported and the
			// like. Retrying at the normal cadence is pure log noise, but an
			// operator may still fix the ACL without restarting us, so back
			// off hard rather than giving up.
			delay = subscribeDenyDelay
			s.rt.SetError(fmt.Errorf("broker rejected every topic filter: %s", strings.Join(rejected, `,`)))
		}
		s.rt.Error("failed to subscribe, retrying",
			log.KV("subscription", s.name),
			log.KV("topics", topics),
			log.KV("attempt", attempt),
			log.KV("retryin", delay),
			log.KVErr(err))
		if s.rt.Sleep(delay) {
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

func nextDelay(d time.Duration) time.Duration {
	if d *= 2; d > retryMaxDelay {
		return retryMaxDelay
	}
	return d
}

// guard runs fn and swallows a panic.
//
// The runner's own recover only wraps the goroutine running Run. Everything
// paho dispatches - connection callbacks and the publish router - runs on
// goroutines the library owns, so a panic there would take down the hosted
// runner and every other plugin sharing the process with it.
func (s *subscriber) guard(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			s.rt.Error("recovered from panic",
				log.KV("subscription", s.name),
				log.KV("operation", what),
				log.KV("panic", fmt.Sprintf("%v", r)))
		}
	}()
	fn()
}

func (s *subscriber) onConnectionDown() bool {
	s.rt.Warn("mqtt connection lost", log.KV("subscription", s.name))
	s.rt.SetWarn("mqtt connection lost, reconnecting")
	return true // keep trying to reconnect
}

// onConnectError reports upstream as well as logging. autopaho retries
// forever, so without this an instance with a bad password or an unreachable
// broker would log on every attempt while still reporting itself healthy.
func (s *subscriber) onConnectError(err error) {
	s.rt.Error("mqtt connection attempt failed",
		log.KV("subscription", s.name), log.KVErr(err))
	s.rt.SetError(fmt.Errorf("mqtt connection failed: %w", err))
}

func (s *subscriber) onClientError(err error) {
	s.rt.Error("mqtt client error",
		log.KV("subscription", s.name), log.KVErr(err))
	s.rt.SetError(fmt.Errorf("mqtt client error: %w", err))
}
