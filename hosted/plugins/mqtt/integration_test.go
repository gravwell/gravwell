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
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/stretchr/testify/require"
)

// These tests need a live MQTT v5 broker. Point MQTT_TEST_BROKER at one to run
// them, for example:
//
//	docker run -d --name gw-mqtt-test -p 1883:1883 eclipse-mosquitto:2 \
//	    mosquitto -c /mosquitto-no-auth.conf
//	MQTT_TEST_BROKER=mqtt://127.0.0.1:1883 go test ./hosted/plugins/mqtt/...
const brokerEnv = `MQTT_TEST_BROKER`

func testBroker(t *testing.T) string {
	t.Helper()
	b := os.Getenv(brokerEnv)
	if b == `` {
		t.Skipf("set %s to a live MQTT v5 broker to run this test", brokerEnv)
	}
	return b
}

// uniqueID keeps each run isolated. Durable sessions survive on the broker, so
// reusing a client ID or topic would let state from an earlier run leak in.
func uniqueID(t *testing.T) string {
	t.Helper()
	var b [8]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)
	return hex.EncodeToString(b[:])
}

// watchedRuntime signals a channel as entries arrive.
type watchedRuntime struct {
	*hosted.Mock
	got chan entry.Entry
}

func newWatchedRuntime(ctx context.Context) *watchedRuntime {
	return &watchedRuntime{Mock: hosted.NewMock(ctx), got: make(chan entry.Entry, 16)}
}

func (w *watchedRuntime) Write(e entry.Entry) error {
	if err := w.Mock.Write(e); err != nil {
		return err
	}
	select {
	case w.got <- e:
	default:
	}
	return nil
}

// failingRuntime refuses every write, so nothing is ever acknowledged.
type failingRuntime struct {
	*hosted.Mock
	mu     sync.Mutex
	writes int
}

func (f *failingRuntime) Write(entry.Entry) error {
	f.mu.Lock()
	f.writes++
	f.mu.Unlock()
	return errTransient
}

var errTransient = &transientErr{}

type transientErr struct{}

func (*transientErr) Error() string { return "integration test write failure" }

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
		ClientConfig: paho.ClientConfig{ClientID: `gw-test-pub-` + uniqueID(t)},
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
		Topic: topic, QoS: qos, Payload: []byte(payload), Properties: props,
	})
	require.NoError(t, err)
}

func itestConfig(t *testing.T, broker, id string) Config {
	t.Helper()
	c := Config{
		Broker:         []string{broker},
		Topic:          []string{`gwtest/` + id + `/#`},
		QoS:            `1`,
		Client_ID:      `gw-itest-` + id,
		Max_In_Flight:  16,
		Session_Expiry: 300,
		Keep_Alive:     20,
	}
	c.Tag_Name = `default`
	c.Ingester_UUID = testUUID
	require.NoError(t, c.Verify())
	return c
}

// runPlugin starts Run in the background and returns a stop function that
// asserts Run unwound promptly.
func runPlugin(t *testing.T, conf Config, rt hosted.Runtime) func() {
	t.Helper()
	ing, err := New(conf, `itest`, rt)
	require.NoError(t, err)

	ctx, cf := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ing.Run(ctx, rt) }()

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cf()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			// NativeRunner.Close waits on Run with NO timeout, so a Run that
			// does not unwind here hangs the whole hosted runner on shutdown
			t.Fatal("Run did not return after context cancellation")
		}
	}
	t.Cleanup(stop)
	time.Sleep(1500 * time.Millisecond) // let the subscription land
	return stop
}

func TestIntegrationSubscribeAndIngest(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	ctx, cf := context.WithCancel(context.Background())
	defer cf()
	rt := newWatchedRuntime(ctx)

	runPlugin(t, itestConfig(t, broker, id), rt)

	publishOne(t, broker, `gwtest/`+id+`/sensor/temp`, `{"temp":22}`, 1,
		&paho.PublishProperties{
			ContentType: `application/json`,
			User:        paho.UserProperties{{Key: `device`, Value: `sensor-7`}},
		})

	select {
	case ent := <-rt.got:
		require.Equal(t, []byte(`{"temp":22}`), ent.Data)
		v, ok := ent.GetEnumeratedValue(evTopic)
		require.True(t, ok)
		require.Equal(t, `gwtest/`+id+`/sensor/temp`, v)
		v, ok = ent.GetEnumeratedValue(`device`)
		require.True(t, ok)
		require.Equal(t, `sensor-7`, v)
	case <-time.After(15 * time.Second):
		t.Fatal("no entry arrived from the broker")
	}
}

// Run must come back promptly when the context is cancelled. NativeRunner.Close
// waits on it with no timeout, so a Run that lingers hangs process shutdown.
func TestIntegrationRunStopsPromptly(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	stop := runPlugin(t, itestConfig(t, broker, id), newWatchedRuntime(ctx))
	start := time.Now()
	stop()
	require.Less(t, time.Since(start), 10*time.Second, "shutdown should not wait on a timeout")
}

// A message published while the plugin is down must arrive when it comes back.
func TestIntegrationDurableSession(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	// first connection establishes the durable session and its subscription
	stop := runPlugin(t, itestConfig(t, broker, id), newWatchedRuntime(ctx))
	stop()
	time.Sleep(500 * time.Millisecond)

	publishOne(t, broker, `gwtest/`+id+`/queued`, `while-away`, 1, nil)
	time.Sleep(500 * time.Millisecond)

	rt := newWatchedRuntime(ctx)
	runPlugin(t, itestConfig(t, broker, id), rt)
	select {
	case ent := <-rt.got:
		require.Equal(t, []byte(`while-away`), ent.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("queued message was not delivered after reconnect")
	}
}

// The ack discipline end to end: a message whose write fails must not be
// acknowledged, and must therefore come back on the next connection.
func TestIntegrationRedeliveryOnWriteFailure(t *testing.T) {
	broker := testBroker(t)
	id := uniqueID(t)
	ctx, cf := context.WithCancel(context.Background())
	defer cf()

	failing := &failingRuntime{Mock: hosted.NewMock(ctx)}
	stop := runPlugin(t, itestConfig(t, broker, id), failing)

	publishOne(t, broker, `gwtest/`+id+`/msg`, `must-come-back`, 1, nil)
	time.Sleep(time.Second) // let the failing write happen and start retrying
	require.Empty(t, failing.Entries(), "the write was supposed to fail")
	stop()
	time.Sleep(500 * time.Millisecond)

	rt := newWatchedRuntime(ctx)
	runPlugin(t, itestConfig(t, broker, id), rt)
	select {
	case ent := <-rt.got:
		require.Equal(t, []byte(`must-come-back`), ent.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("an unacknowledged message was not redelivered")
	}
}
