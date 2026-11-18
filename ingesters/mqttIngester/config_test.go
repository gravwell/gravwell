/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), `mqtt.conf`)
	require.NoError(t, os.WriteFile(p, []byte(body), 0600))
	return p
}

// loadConfig writes body to disk and runs it through GetConfig.
func loadConfig(t *testing.T, body string) (*cfgType, error) {
	t.Helper()
	return GetConfig(writeTempConfig(t, body), ``)
}

const globalBlock = `
[Global]
Ingest-Secret = "IngestSecrets"
Connection-Timeout = 0
Pipe-Backend-Target=/tmp/pipe
Log-Level=OFF
Log-File=/tmp/mqtt_test.log
`

func cfgWith(sub string) string {
	return globalBlock + "\n" + sub
}

const minimalSub = `
[Subscription "default"]
	Broker="mqtt://127.0.0.1:1883"
	Topic="gravwell/#"
	Default-Tag=mqtt
`

func TestMinimalConfig(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(minimalSub))
	require.NoError(t, err)
	require.Len(t, cfg.Subscriptions, 1)

	sub := cfg.Subscriptions[`default`]
	require.NotNil(t, sub)
	require.Equal(t, `mqtt`, sub.defTag)
	require.Equal(t, []string{`gravwell/#`}, sub.topics)
	require.Equal(t, defaultQoS, sub.qos)
	require.Equal(t, defaultMaxInFlight, sub.maxInFlight)
	require.Equal(t, defaultSessionExpiry, sub.sessionExpiry)
	require.Equal(t, defaultKeepAlive, sub.keepAlive)
	require.True(t, sub.attachMeta, "metadata is attached unless disabled")
	require.Nil(t, sub.tlsCfg, "plain mqtt:// needs no TLS config")

	tags, err := cfg.Tags()
	require.NoError(t, err)
	require.Equal(t, []string{`mqtt`}, tags)
}

func TestBrokerURLDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: `bare host`, in: `127.0.0.1`, want: `127.0.0.1:1883`},
		{name: `mqtt scheme`, in: `mqtt://broker.example.com`, want: `broker.example.com:1883`},
		{name: `tls scheme`, in: `tls://broker.example.com`, want: `broker.example.com:8883`},
		{name: `mqtts scheme`, in: `mqtts://broker.example.com`, want: `broker.example.com:8883`},
		{name: `ws scheme`, in: `ws://broker.example.com`, want: `broker.example.com:80`},
		{name: `wss scheme`, in: `wss://broker.example.com`, want: `broker.example.com:443`},
		{name: `explicit port wins`, in: `mqtt://broker.example.com:1234`, want: `broker.example.com:1234`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := cfgWith(fmt.Sprintf(`
[Subscription "s"]
	Broker=%q
	Topic="a/b"
	Default-Tag=mqtt
`, tt.in))
			cfg, err := loadConfig(t, body)
			require.NoError(t, err)
			sub := cfg.Subscriptions[`s`]
			require.Len(t, sub.brokers, 1)
			require.Equal(t, tt.want, sub.brokers[0].Host)
		})
	}
}

func TestTLSSchemeBuildsTLSConfig(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="tls://broker.example.com"
	Topic="a/b"
	Default-Tag=mqtt
	Insecure-Skip-TLS-Verify=true
`))
	require.NoError(t, err)
	sub := cfg.Subscriptions[`s`]
	require.NotNil(t, sub.tlsCfg)
	require.True(t, sub.tlsCfg.InsecureSkipVerify)
}

func TestSharedGroupWrapsTopics(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="factory/+/temp"
	Topic="factory/+/pressure"
	Shared-Group="gravwell"
	Default-Tag=mqtt
`))
	require.NoError(t, err)
	require.Equal(t, []string{
		`$share/gravwell/factory/+/temp`,
		`$share/gravwell/factory/+/pressure`,
	}, cfg.Subscriptions[`s`].topics)
}

func TestTopicRouteParsing(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="factory/#"
	Default-Tag=mqtt
	Topic-Route="factory/line1/*:mqttline1"
	Topic-Route="factory/*/heartbeat:"
`))
	require.NoError(t, err)
	routes := cfg.Subscriptions[`s`].routes
	require.Len(t, routes, 2)

	require.Equal(t, `factory/line1/*`, routes[0].pattern)
	require.Equal(t, `mqttline1`, routes[0].tag)
	require.False(t, routes[0].drop)
	require.True(t, routes[0].glob.Match(`factory/line1/temp`))
	require.False(t, routes[0].glob.Match(`factory/line1/sub/temp`), "* must not cross a topic level")

	require.True(t, routes[1].drop, "an empty tag means drop")
	require.Equal(t, ``, routes[1].tag)

	// the drop route contributes no tag, the routed one does
	tags, err := cfg.Tags()
	require.NoError(t, err)
	require.Equal(t, []string{`mqtt`, `mqttline1`}, tags)
}

func TestTagsAreDedupedAndSorted(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "a"]
	Broker="127.0.0.1"
	Topic="a/#"
	Default-Tag=zebra
	Topic-Route="a/x/*:apple"

[Subscription "b"]
	Broker="127.0.0.1"
	Topic="b/#"
	Default-Tag=zebra
	Topic-Route="b/y/*:apple"
`))
	require.NoError(t, err)
	tags, err := cfg.Tags()
	require.NoError(t, err)
	require.Equal(t, []string{`apple`, `zebra`}, tags)
}

func TestQoSValues(t *testing.T) {
	// QoS is read as a string so that an explicit 0 is honoured rather than
	// being indistinguishable from an omitted value.
	for _, q := range []int{0, 1, 2} {
		body := cfgWith(fmt.Sprintf(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="a/b"
	Default-Tag=mqtt
	QoS=%d
`, q))
		cfg, err := loadConfig(t, body)
		require.NoError(t, err)
		require.Equal(t, byte(q), cfg.Subscriptions[`s`].qos, "explicit QoS=%d must be honoured", q)
	}
}

func TestQoSDefaultsWhenUnset(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(minimalSub))
	require.NoError(t, err)
	require.Equal(t, defaultQoS, cfg.Subscriptions[`default`].qos)
}

// A custom [TimeFormat] section must be resolvable by name from
// Timestamp-Format-Override. That only works if the override is applied after
// the custom formats are loaded into the timegrinder.
func TestCustomTimeFormatOverride(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="a/b"
	Default-Tag=mqtt
	Extract-Timestamps=true
	Timestamp-Format-Override="mycustom"

[TimeFormat "mycustom"]
	Format="2006-01-02 15:04:05"
	Regex="\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}"
`))
	require.NoError(t, err)
	sub := cfg.Subscriptions[`s`]
	require.NotNil(t, sub.tg)
	require.Equal(t, `mycustom`, sub.tg.FormatOverride)

	ts, ok, err := sub.tg.Extract([]byte(`2021-03-04 05:06:07 hello`))
	require.NoError(t, err)
	require.True(t, ok, "the custom format should match")
	require.Equal(t, time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC), ts.UTC())
}

func TestUnknownTimeFormatOverrideIsRejected(t *testing.T) {
	_, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="a/b"
	Default-Tag=mqtt
	Extract-Timestamps=true
	Timestamp-Format-Override="nosuchformat"
`))
	require.Error(t, err)
	require.Contains(t, err.Error(), `nosuchformat`)
}

// A broker URL with leading whitespace must still be recognised as TLS.
// brokerURLs trims before parsing, so a scheme check against the raw strings
// would disagree and leave tlsCfg nil.
func TestTLSSchemeWithLeadingWhitespace(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(`
[Subscription "s"]
	Broker="  tls://broker.example.com"
	Topic="a/b"
	Default-Tag=mqtt
`))
	require.NoError(t, err)
	sub := cfg.Subscriptions[`s`]
	require.Equal(t, `broker.example.com:8883`, sub.brokers[0].Host)
	require.NotNil(t, sub.tlsCfg, "a tls:// broker must get a TLS config")
	require.Equal(t, uint16(tls.VersionTLS12), sub.tlsCfg.MinVersion)
}

func TestSessionExpiryBounds(t *testing.T) {
	load := func(v string) (*cfgType, error) {
		return loadConfig(t, cfgWith(fmt.Sprintf(`
[Subscription "s"]
	Broker="127.0.0.1"
	Topic="a/b"
	Default-Tag=mqtt
	Session-Expiry=%s
`, v)))
	}

	cfg, err := load(`900`)
	require.NoError(t, err)
	require.Equal(t, uint32(900), cfg.Subscriptions[`s`].sessionExpiry)

	cfg, err = load(`4294967295`)
	require.NoError(t, err)
	require.Equal(t, uint32(4294967295), cfg.Subscriptions[`s`].sessionExpiry)

	// one past the uint32 ceiling would wrap to 0, which means "drop the
	// session at disconnect" and silently discards every queued message
	_, err = load(`4294967296`)
	require.Error(t, err)
	require.Contains(t, err.Error(), `Session-Expiry`)
}

// Two subscriptions must never present the same client ID: the broker evicts
// the older session, so they would kick each other off forever.
func TestClientIDCollisions(t *testing.T) {
	t.Run("generated ids collide after sanitizing", func(t *testing.T) {
		_, err := loadConfig(t, cfgWith(`
[Subscription "my sensors"]
	Broker="127.0.0.1"
	Topic="a/#"
	Default-Tag=mqtt

[Subscription "my/sensors"]
	Broker="127.0.0.1"
	Topic="b/#"
	Default-Tag=mqtt
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `Client-ID`)
	})

	t.Run("explicit ids collide", func(t *testing.T) {
		_, err := loadConfig(t, cfgWith(`
[Subscription "a"]
	Broker="127.0.0.1"
	Topic="a/#"
	Default-Tag=mqtt
	Client-ID="same"

[Subscription "b"]
	Broker="127.0.0.1"
	Topic="b/#"
	Default-Tag=mqtt
	Client-ID="same"
`))
		require.Error(t, err)
		require.Contains(t, err.Error(), `Client-ID`)
	})

	t.Run("distinct names are fine", func(t *testing.T) {
		_, err := loadConfig(t, cfgWith(`
[Subscription "alpha"]
	Broker="127.0.0.1"
	Topic="a/#"
	Default-Tag=mqtt

[Subscription "beta"]
	Broker="127.0.0.1"
	Topic="b/#"
	Default-Tag=mqtt
`))
		require.NoError(t, err)
	})
}

// Timestamp knobs only feed the timegrinder, so accepting them without
// Extract-Timestamps would silently do nothing.
func TestTimestampOptionsRequireExtract(t *testing.T) {
	for _, opt := range []string{
		"\tTimestamp-Format-Override=\"RFC3339\"\n",
		"\tTimezone-Override=\"US/Pacific\"\n",
		"\tAssume-Local-Timezone=true\n",
	} {
		_, err := loadConfig(t, cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n"+opt))
		require.Error(t, err, opt)
		require.Contains(t, err.Error(), `Extract-Timestamps`)
	}
}

func TestBadConfigs(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: `no subscriptions`, body: globalBlock},
		{
			name: `missing default tag`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n"),
		},
		{
			name: `invalid default tag`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=\"bad tag\"\n"),
		},
		{
			name: `no broker`,
			body: cfgWith("[Subscription \"s\"]\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `no topic`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `bad broker scheme`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"amqp://127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `hash not last level`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/#/b\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `hash sharing a level`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b#\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `plus sharing a level`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b+/c\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `literal share prefix`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"$share/g/a/b\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `shared group with wildcard`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tShared-Group=\"a/b\"\n\tDefault-Tag=mqtt\n"),
		},
		{
			name: `route missing separator`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tTopic-Route=\"a/b\"\n"),
		},
		{
			name: `route with empty pattern`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tTopic-Route=\":tag\"\n"),
		},
		{
			name: `route with invalid tag`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tTopic-Route=\"a/*:bad tag\"\n"),
		},
		{
			name: `invalid QoS`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tQoS=3\n"),
		},
		{
			name: `timezone and local timezone`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tAssume-Local-Timezone=true\n\tTimezone-Override=\"US/Pacific\"\n"),
		},
		{
			name: `ignore and extract timestamps`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tIgnore-Timestamps=true\n\tExtract-Timestamps=true\n"),
		},
		{
			name: `client cert without key`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"tls://127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tClient-Cert=\"/tmp/nope.pem\"\n"),
		},
		{
			name: `missing CA file`,
			body: cfgWith("/dev/null/nope" + "\n[Subscription \"s\"]\n\tBroker=\"tls://127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tCA-Cert=\"/dev/null/nope.pem\"\n"),
		},
		{
			name: `bad source override`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tSource-Override=\"not-an-ip\"\n"),
		},
		{
			name: `max in flight too large`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tMax-In-Flight=70000\n"),
		},
		{
			name: `unknown preprocessor`,
			body: cfgWith("[Subscription \"s\"]\n\tBroker=\"127.0.0.1\"\n\tTopic=\"a/b\"\n\tDefault-Tag=mqtt\n\tPreprocessor=nope\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(t, tt.body)
			require.Error(t, err, "expected %s to be rejected", tt.name)
			require.Nil(t, cfg)
		})
	}
}

func TestCfgTypeSatisfiesBaseInterfaces(t *testing.T) {
	cfg, err := loadConfig(t, cfgWith(minimalSub))
	require.NoError(t, err)

	var ic = cfg.IngestBaseConfig()
	require.Equal(t, `IngestSecrets`, ic.Secret())
	require.NoError(t, cfg.AttachConfig().Verify())
	require.NoError(t, cfg.Verify())

	tgs, err := cfg.Tags()
	require.NoError(t, err)
	require.Equal(t, []string{`mqtt`}, tgs)
}
