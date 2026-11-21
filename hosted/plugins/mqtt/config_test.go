/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mqtt

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/hosted/configtest"
	"github.com/stretchr/testify/require"
)

const testUUID = `7db1995f-ffa3-4f0b-b5f8-1516459d100a`

// baseConfig is the minimum that Verify accepts.
func baseConfig() Config {
	c := Config{
		Broker: []string{`mqtt://127.0.0.1:1883`},
		Topic:  []string{`gravwell/#`},
	}
	c.Ingester_UUID = testUUID
	return c
}

func TestMinimalConfig(t *testing.T) {
	c := baseConfig()
	require.NoError(t, c.Verify())

	require.Equal(t, []string{`gravwell/#`}, c.topics)
	require.Equal(t, defaultQoS, c.qos)
	require.Equal(t, defaultMaxInFlight, c.maxInFlight)
	require.Equal(t, defaultSessionExpiry, c.sessionExpiry)
	require.Equal(t, defaultKeepAlive, c.keepAlive)
	require.Nil(t, c.tlsCfg, "plain mqtt:// needs no TLS config")
	require.Equal(t, []string{`default`}, c.Tags(), "unset Tag-Name falls back to the default tag")
}

func TestConfigEqual(t *testing.T) {
	c := baseConfig()
	c.Tag_Name = `mqtt`
	c.Topic_Route = []string{`a/*:routed`}
	c.Password = `secret`
	configtest.CheckEqual(t, c)
}

func TestBrokerURLDefaults(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{`bare host`, `127.0.0.1`, `127.0.0.1:1883`},
		{`mqtt scheme`, `mqtt://broker.example.com`, `broker.example.com:1883`},
		{`tls scheme`, `tls://broker.example.com`, `broker.example.com:8883`},
		{`mqtts scheme`, `mqtts://broker.example.com`, `broker.example.com:8883`},
		{`ws scheme`, `ws://broker.example.com`, `broker.example.com:80`},
		{`wss scheme`, `wss://broker.example.com`, `broker.example.com:443`},
		{`explicit port wins`, `mqtt://broker.example.com:1234`, `broker.example.com:1234`},
		{`ipv6 literal`, `mqtt://[::1]`, `[::1]:1883`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseConfig()
			c.Broker = []string{tt.in}
			require.NoError(t, c.Verify())
			require.Len(t, c.brokers, 1)
			require.Equal(t, tt.want, c.brokers[0].Host)
		})
	}
}

// A broker URL with leading whitespace must still be recognised as TLS.
// brokerURLs trims before parsing, so a scheme check against the raw strings
// would disagree and leave tlsCfg nil.
func TestTLSSchemeWithLeadingWhitespace(t *testing.T) {
	c := baseConfig()
	c.Broker = []string{`  tls://broker.example.com`}
	require.NoError(t, c.Verify())
	require.Equal(t, `broker.example.com:8883`, c.brokers[0].Host)
	require.NotNil(t, c.tlsCfg)
	require.Equal(t, uint16(tls.VersionTLS12), c.tlsCfg.MinVersion)
}

func TestSharedGroupWrapsTopics(t *testing.T) {
	c := baseConfig()
	c.Topic = []string{`factory/+/temp`, `factory/+/pressure`}
	c.Shared_Group = `gravwell`
	require.NoError(t, c.Verify())
	require.Equal(t, []string{
		`$share/gravwell/factory/+/temp`,
		`$share/gravwell/factory/+/pressure`,
	}, c.topics)
}

func TestTopicRouteParsing(t *testing.T) {
	c := baseConfig()
	c.Tag_Name = `mqtt`
	c.Topic_Route = []string{`factory/line1/*:mqttline1`, `factory/*/heartbeat:`}
	require.NoError(t, c.Verify())

	require.Len(t, c.routes, 2)
	require.Equal(t, `mqttline1`, c.routes[0].tag)
	require.False(t, c.routes[0].drop)
	require.True(t, c.routes[0].glob.Match(`factory/line1/temp`))
	require.False(t, c.routes[0].glob.Match(`factory/line1/sub/temp`), "* must not cross a topic level")
	require.True(t, c.routes[1].drop, "an empty tag means drop")

	// the drop route contributes no tag, the routed one does
	require.Equal(t, []string{`mqtt`, `mqttline1`}, c.Tags())
}

func TestQoSValues(t *testing.T) {
	for _, q := range []string{`0`, `1`, `2`} {
		c := baseConfig()
		c.QoS = q
		require.NoError(t, c.Verify())
		require.Equal(t, q, string('0'+rune(c.qos)), "explicit QoS=%s must be honoured", q)
	}
	c := baseConfig()
	require.NoError(t, c.Verify())
	require.Equal(t, defaultQoS, c.qos, "unset QoS defaults")
}

func TestSessionExpiryBounds(t *testing.T) {
	c := baseConfig()
	c.Session_Expiry = 900
	require.NoError(t, c.Verify())
	require.Equal(t, uint32(900), c.sessionExpiry)

	// the ceiling and one past it, guarded because neither value is
	// representable as an int on a 32 bit build
	if max := maxSessionExpiry; int64(int(max)) == max {
		c = baseConfig()
		c.Session_Expiry = int(max)
		require.NoError(t, c.Verify())
		require.Equal(t, uint32(maxSessionExpiry), c.sessionExpiry)
	}
	if over := maxSessionExpiry + 1; int64(int(over)) == over {
		// an unchecked conversion would wrap to 0, which means "drop the
		// session at disconnect" and silently discards every queued message
		c = baseConfig()
		c.Session_Expiry = int(over)
		require.ErrorContains(t, c.Verify(), `Session-Expiry`)
	}
}

// autopaho only applies TlsCfg to a TLS scheme, so TLS options against a
// plaintext broker would be silently ignored and the credentials would go out
// in the clear.
func TestTLSOptionsRequireTLSScheme(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		`ca cert`:     func(c *Config) { c.CA_Cert = `/tmp/ca.pem` },
		`client cert`: func(c *Config) { c.Client_Cert = `/tmp/c.pem` },
		`client key`:  func(c *Config) { c.Client_Key = `/tmp/c.key` },
		`skip verify`: func(c *Config) { c.Insecure_Skip_TLS_Verify = true },
	} {
		t.Run(name, func(t *testing.T) {
			c := baseConfig() // mqtt:// broker
			mutate(&c)
			require.ErrorContains(t, c.Verify(), `TLS`)
		})
	}

	// the same options are fine once the broker is a TLS one
	c := baseConfig()
	c.Broker = []string{`tls://broker.example.com`}
	c.Insecure_Skip_TLS_Verify = true
	require.NoError(t, c.Verify())
	require.NotNil(t, c.tlsCfg)
}

func TestClientIDDefaulting(t *testing.T) {
	c := baseConfig()
	require.NoError(t, c.Verify())
	require.Equal(t, `gravwell-mqtt-7db1995f-sensors`, c.ClientID(`sensors`))
	require.Equal(t, `gravwell-mqtt-7db1995f-my_sensors`, c.ClientID(`my sensors`))
	require.Equal(t, `gravwell-mqtt-7db1995f-a_b_c`, c.ClientID(`a/b.c`))

	c.Client_ID = `explicit`
	require.Equal(t, `explicit`, c.ClientID(`ignored`))
}

func TestTimestampOptionsRequireExtract(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		`format override`: func(c *Config) { c.Timestamp_Format_Override = `RFC3339` },
		`timezone`:        func(c *Config) { c.Timezone_Override = `US/Pacific` },
		`local timezone`:  func(c *Config) { c.Assume_Local_Timezone = true },
	} {
		t.Run(name, func(t *testing.T) {
			c := baseConfig()
			mutate(&c)
			require.ErrorContains(t, c.Verify(), `Extract-Timestamps`)
		})
	}
}

func TestTimeGrinderBuilds(t *testing.T) {
	c := baseConfig()
	c.Extract_Timestamps = true
	c.Timestamp_Format_Override = `RFC3339`
	require.NoError(t, c.Verify())

	tg, err := c.newTimeGrinder()
	require.NoError(t, err)
	require.NotNil(t, tg)
	ts, ok, err := tg.Extract([]byte(`2021-03-04T05:06:07Z hello`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC), ts.UTC())
}

func TestBadConfigs(t *testing.T) {
	tests := map[string]func(*Config){
		`no broker`:               func(c *Config) { c.Broker = nil },
		`empty broker entry`:      func(c *Config) { c.Broker = []string{``} },
		`bad broker scheme`:       func(c *Config) { c.Broker = []string{`amqp://127.0.0.1`} },
		`no topic`:                func(c *Config) { c.Topic = nil },
		`hash not last level`:     func(c *Config) { c.Topic = []string{`a/#/b`} },
		`hash sharing a level`:    func(c *Config) { c.Topic = []string{`a/b#`} },
		`plus sharing a level`:    func(c *Config) { c.Topic = []string{`a/b+/c`} },
		`literal share prefix`:    func(c *Config) { c.Topic = []string{`$share/g/a/b`} },
		`shared group wildcard`:   func(c *Config) { c.Shared_Group = `a/b` },
		`route missing separator`: func(c *Config) { c.Topic_Route = []string{`a/b`} },
		`route empty pattern`:     func(c *Config) { c.Topic_Route = []string{`:tag`} },
		`route invalid tag`:       func(c *Config) { c.Topic_Route = []string{`a/*:bad tag`} },
		`invalid QoS`:             func(c *Config) { c.QoS = `3` },
		`non numeric QoS`:         func(c *Config) { c.QoS = `high` },
		`negative session expiry`: func(c *Config) { c.Session_Expiry = -1 },
		`max in flight too large`: func(c *Config) { c.Max_In_Flight = 70000 },
		`bad source override`:     func(c *Config) { c.Source_Override = `not-an-ip` },
		`client cert without key`: func(c *Config) {
			c.Broker = []string{`tls://broker.example.com`}
			c.Client_Cert = `/tmp/nope.pem`
		},
		`missing CA file`: func(c *Config) {
			c.Broker = []string{`tls://broker.example.com`}
			c.CA_Cert = `/dev/null/nope.pem`
		},
		`bad ingester uuid`: func(c *Config) { c.Ingester_UUID = `not-a-uuid` },
		`both timestamp modes`: func(c *Config) {
			c.Ignore_Timestamps = true
			c.Extract_Timestamps = true
		},
		`timezone and local timezone`: func(c *Config) {
			c.Extract_Timestamps = true
			c.Assume_Local_Timezone = true
			c.Timezone_Override = `US/Pacific`
		},
		`unknown format override`: func(c *Config) {
			c.Extract_Timestamps = true
			c.Timestamp_Format_Override = `nosuchformat`
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := baseConfig()
			mutate(&c)
			require.Error(t, c.Verify(), "expected %s to be rejected", name)
		})
	}
}
