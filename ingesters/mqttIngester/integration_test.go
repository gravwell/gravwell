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
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/ingest/log"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/stretchr/testify/require"
)

// These tests need a live MQTT v5 broker. Point MQTT_TEST_BROKER at one to run
// them, for example:
//
//	docker run -d --name gw-mqtt-test -p 18833:1883 eclipse-mosquitto:2 \
//	    mosquitto -c /mosquitto-no-auth.conf
//	MQTT_TEST_BROKER=mqtt://127.0.0.1:18833 go test ./ingesters/mqttIngester/...
const brokerEnv = `MQTT_TEST_BROKER`

// uniqueID keeps each run isolated. Durable sessions survive on the broker, so
// reusing a client ID or topic would let state from an earlier run leak in.
func uniqueID(t *testing.T) string {
	t.Helper()
	var b [8]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)
	return hex.EncodeToString(b[:])
}

func testBroker(t *testing.T) string {
	t.Helper()
	b := os.Getenv(brokerEnv)
	if b == `` {
		t.Skipf("set %s to a live MQTT v5 broker to run this test", brokerEnv)
	}
	return b
}

// collectingWriter signals a channel as entries arrive.
type collectingWriter struct {
	*fakeWriter
	got chan *entry.Entry
}

func (cw *collectingWriter) WriteEntryContext(ctx context.Context, e *entry.Entry) error {
	if err := cw.fakeWriter.WriteEntryContext(ctx, e); err != nil {
		return err
	}
	select {
	case cw.got <- e:
	default:
	}
	return nil
}

func (cw *collectingWriter) WriteEntry(e *entry.Entry) error {
	return cw.WriteEntryContext(context.Background(), e)
}

func (cw *collectingWriter) WriteBatchContext(ctx context.Context, ents []*entry.Entry) error {
	for _, e := range ents {
		if err := cw.WriteEntryContext(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (cw *collectingWriter) WriteBatch(ents []*entry.Entry) error {
	return cw.WriteBatchContext(context.Background(), ents)
}

// publishOne connects a throwaway client and publishes a single message.
func publishOne(t *testing.T, broker, topic, payload string, qos byte, props *paho.PublishProperties) {
	t.Helper()
	u, err := url.Parse(broker)
	require.NoError(t, err)

	ctx, cf := context.WithTimeout(context.Background(), 15*time.Second)
	defer cf()

	up := make(chan struct{})
	var once sync.Once
	cm, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     20,
		CleanStartOnInitialConnection: true,
		OnConnectionUp: func(*autopaho.ConnectionManager, *paho.Connack) {
			once.Do(func() { close(up) })
		},
		ClientConfig: paho.ClientConfig{ClientID: `gravwell-test-pub-` + uniqueID(t)},
	})
	require.NoError(t, err)
	defer func() {
		dctx, dcf := context.WithTimeout(context.Background(), 2*time.Second)
		cm.Disconnect(dctx)
		dcf()
	}()

	select {
	case <-up:
	case <-ctx.Done():
		t.Fatal("publisher never connected")
	}

	_, err = cm.Publish(ctx, &paho.Publish{
		Topic:      topic,
		QoS:        qos,
		Payload:    []byte(payload),
		Properties: props,
	})
	require.NoError(t, err)
}

// startSubscriber wires a real mqttSubscriber against the broker.
func startSubscriber(t *testing.T, broker string, sc *subCfg) (*mqttSubscriber, *collectingWriter) {
	t.Helper()
	u, err := url.Parse(broker)
	require.NoError(t, err)
	sc.brokers = []*url.URL{u}

	cw := &collectingWriter{fakeWriter: newFakeWriter(), got: make(chan *entry.Entry, 16)}
	defTag, err := cw.NegotiateTag(`default`)
	require.NoError(t, err)

	sub, err := newSubscriber(subscriberConfig{
		cfg:    sc,
		proc:   processors.NewProcessorSet(cw),
		defTag: defTag,
		src:    net.ParseIP(`10.0.0.1`),
		lg:     log.NewDiscardLogger(),
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	require.NoError(t, sub.Start(&wg))
	t.Cleanup(func() {
		sub.Close()
		wg.Wait()
	})

	// wait for the subscription to actually be in place before publishing
	actx, acf := context.WithTimeout(context.Background(), 15*time.Second)
	defer acf()
	require.NoError(t, sub.cm.AwaitConnection(actx))
	time.Sleep(500 * time.Millisecond)
	return sub, cw
}

func TestIntegrationSubscribeAndIngest(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	sc := &subCfg{
		name:          `itest`,
		clientID:      `gravwell-itest-` + id,
		topics:        []string{`gwtest/` + id + `/#`},
		qos:           1,
		maxInFlight:   16,
		sessionExpiry: 60,
		keepAlive:     20,
		attachMeta:    true,
	}
	_, cw := startSubscriber(t, broker, sc)

	publishOne(t, broker, `gwtest/`+id+`/sensor/temp`, `{"temp":22}`, 1, &paho.PublishProperties{
		ContentType: `application/json`,
		User:        paho.UserProperties{{Key: `device`, Value: `sensor-7`}},
	})

	select {
	case ent := <-cw.got:
		require.Equal(t, []byte(`{"temp":22}`), ent.Data)
		v, ok := ent.GetEnumeratedValue(evTopic)
		require.True(t, ok)
		require.Equal(t, `gwtest/`+id+`/sensor/temp`, v)
		v, ok = ent.GetEnumeratedValue(evContentType)
		require.True(t, ok)
		require.Equal(t, `application/json`, v)
		v, ok = ent.GetEnumeratedValue(`device`)
		require.True(t, ok)
		require.Equal(t, `sensor-7`, v)
	case <-time.After(15 * time.Second):
		t.Fatal("no entry arrived from the broker")
	}
}

// TestIntegrationDurableSession checks the thing unit tests cannot: that a
// message published while the ingester is down is delivered when it returns.
func TestIntegrationDurableSession(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	newCfg := func() *subCfg {
		return &subCfg{
			name:          `durable`,
			clientID:      `gravwell-itest-durable-` + id,
			topics:        []string{`gwdurable/` + id + `/#`},
			qos:           1,
			maxInFlight:   16,
			sessionExpiry: 300,
			keepAlive:     20,
			attachMeta:    true,
		}
	}

	// first connection establishes the durable session and its subscription
	sub, _ := startSubscriber(t, broker, newCfg())
	require.NoError(t, sub.Close())
	time.Sleep(500 * time.Millisecond)

	// publish while nothing is connected, the broker must queue it
	publishOne(t, broker, `gwdurable/`+id+`/queued`, `while-away`, 1, nil)
	time.Sleep(500 * time.Millisecond)

	// coming back with the same client ID should drain the queue
	_, cw := startSubscriber(t, broker, newCfg())
	select {
	case ent := <-cw.got:
		require.Equal(t, []byte(`while-away`), ent.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("queued message was not delivered after reconnect")
	}
}

// TestIntegrationRedeliveryOnWriteFailure is the ack discipline end to end: a
// message whose write fails must not be acknowledged, and must therefore come
// back from the broker on the next connection.
func TestIntegrationRedeliveryOnWriteFailure(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	newCfg := func() *subCfg {
		return &subCfg{
			name:          `redeliver`,
			clientID:      `gravwell-itest-redeliver-` + id,
			topics:        []string{`gwredeliver/` + id + `/#`},
			qos:           1,
			maxInFlight:   16,
			sessionExpiry: 300,
			keepAlive:     20,
			attachMeta:    true,
		}
	}

	// first pass: the writer rejects everything, so nothing gets acked
	sub, cw := startSubscriber(t, broker, newCfg())
	cw.fakeWriter.mtx.Lock()
	cw.fakeWriter.err = errTestWrite
	cw.fakeWriter.mtx.Unlock()

	publishOne(t, broker, `gwredeliver/`+id+`/msg`, `must-come-back`, 1, nil)
	time.Sleep(time.Second) // let the failing write happen
	require.Empty(t, cw.fakeWriter.entries(), "the write was supposed to fail")
	require.NoError(t, sub.Close())
	time.Sleep(500 * time.Millisecond)

	// second pass: same client ID, writer now healthy
	_, cw2 := startSubscriber(t, broker, newCfg())
	select {
	case ent := <-cw2.got:
		require.Equal(t, []byte(`must-come-back`), ent.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("an unacknowledged message was not redelivered")
	}
}
