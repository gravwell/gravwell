/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package mqtt provides a hosted ingester plugin that subscribes to one or
// more MQTT v5 brokers.
package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v4/hosted"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/entry"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	Name    string = `mqtt`
	ID      string = `mqtt.ingesters.gravwell.io`
	Version string = `1.0.0`

	defaultIngesterUUIDStr string = `7db1995f-ffa3-4f0b-b5f8-1516459d100a`

	// splitChar matches the convention used by the routing preprocessors
	// (see ingest/processors/regexrouter.go) - "<match>:<tag>" with an empty
	// tag meaning "drop this entry".
	splitChar = `:`

	defaultQoS           byte   = 1
	defaultMaxInFlight   uint16 = 512
	defaultSessionExpiry uint32 = 600
	defaultKeepAlive     uint16 = 30

	// maxSessionExpiry is the MQTT v5 ceiling, the field is a uint32 of
	// seconds. Typed int64 because the untyped constant does not fit in an
	// int on a 32 bit build.
	maxSessionExpiry int64 = 4294967295

	defaultMQTTPort uint16 = 1883
	defaultTLSPort  uint16 = 8883
	defaultWSPort   uint16 = 80
	defaultWSSPort  uint16 = 443

	// topicLevelSep is the MQTT topic level separator, used as the glob
	// separator so that a "*" in a Topic-Route does not cross topic levels.
	topicLevelSep = '/'
)

var (
	ErrNoBrokers = errors.New("no Broker specified")
	ErrNoTopics  = errors.New("no Topic specified")
)

type Config struct {
	hosted.BaseConfig
	hosted.SingleTagConfig

	Broker       []string `dynamic:"required"` // repeatable, failover list
	Topic        []string `dynamic:"required"` // repeatable MQTT topic filters
	Shared_Group string   // wraps each Topic as $share/<group>/<filter>
	QoS          string   // 0, 1 or 2; read as a string so an explicit 0 is distinguishable from unset
	Client_ID    string

	// Topic_Route is a repeatable "<topic glob>:<tag>" list. An empty tag
	// drops matching messages. First match wins.
	Topic_Route []string

	Username string
	Password string `json:"-" dynamic:"secret"` // DO NOT send this when marshalling

	CA_Cert                  string
	Client_Cert              string
	Client_Key               string
	Insecure_Skip_TLS_Verify bool

	Max_In_Flight  int
	Session_Expiry int
	Keep_Alive     int

	// Disable_Metadata turns off the topic/qos/retain/user-property
	// enumerated values. Metadata is attached by default.
	Disable_Metadata bool

	Source_Override string

	Ignore_Timestamps         bool
	Extract_Timestamps        bool
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string

	// derived state, populated by Verify
	brokers       []*url.URL
	topics        []string
	routes        []topicRoute
	qos           byte
	tlsCfg        *tls.Config
	maxInFlight   uint16
	sessionExpiry uint32
	keepAlive     uint16
	srcOverride   net.IP
}

// topicRoute is a compiled Topic-Route entry.
type topicRoute struct {
	pattern string
	glob    glob.Glob
	tag     string // empty means drop
	drop    bool
}

var _ hosted.Config = (*Config)(nil)      // compile time interface check
var _ hosted.TagProvider = (*Config)(nil) // compile time interface check

// Equal implements hosted.Config so the runner can decide whether a config reload
// actually changed anything for this ingester.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.SingleTagConfig == nc.SingleTagConfig &&
		slices.Equal(c.Broker, nc.Broker) &&
		slices.Equal(c.Topic, nc.Topic) &&
		slices.Equal(c.Topic_Route, nc.Topic_Route) &&
		c.Shared_Group == nc.Shared_Group &&
		c.QoS == nc.QoS &&
		c.Client_ID == nc.Client_ID &&
		c.Username == nc.Username &&
		c.Password == nc.Password &&
		c.CA_Cert == nc.CA_Cert &&
		c.Client_Cert == nc.Client_Cert &&
		c.Client_Key == nc.Client_Key &&
		c.Insecure_Skip_TLS_Verify == nc.Insecure_Skip_TLS_Verify &&
		c.Max_In_Flight == nc.Max_In_Flight &&
		c.Session_Expiry == nc.Session_Expiry &&
		c.Keep_Alive == nc.Keep_Alive &&
		c.Disable_Metadata == nc.Disable_Metadata &&
		c.Source_Override == nc.Source_Override &&
		c.Ignore_Timestamps == nc.Ignore_Timestamps &&
		c.Extract_Timestamps == nc.Extract_Timestamps &&
		c.Assume_Local_Timezone == nc.Assume_Local_Timezone &&
		c.Timezone_Override == nc.Timezone_Override &&
		c.Timestamp_Format_Override == nc.Timestamp_Format_Override
}

// Verify validates the config and populates the derived state the ingester runs on.
// It is idempotent, the runner may call it more than once on the same value.
func (c *Config) Verify() (err error) {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)

	if c.brokers, err = c.brokerURLs(); err != nil {
		return err
	}
	if c.topics, err = c.topicFilters(); err != nil {
		return err
	}
	if c.routes, err = c.topicRoutes(); err != nil {
		return err
	}
	if c.qos, err = c.qosValue(); err != nil {
		return err
	}
	if c.tlsCfg, err = c.tlsConfig(c.brokers); err != nil {
		return err
	}
	if c.maxInFlight, err = boundedUint16("Max-In-Flight", c.Max_In_Flight, defaultMaxInFlight); err != nil {
		return err
	}
	if c.keepAlive, err = boundedUint16("Keep-Alive", c.Keep_Alive, defaultKeepAlive); err != nil {
		return err
	}
	if c.sessionExpiry, err = c.sessionExpiryValue(); err != nil {
		return err
	}

	c.srcOverride = nil
	if len(c.Source_Override) > 0 {
		if c.srcOverride = net.ParseIP(c.Source_Override); c.srcOverride == nil {
			return fmt.Errorf("invalid Source-Override %q", c.Source_Override)
		}
	}

	if err = c.verifyTimestampOptions(); err != nil {
		return err
	}
	if err = c.BaseConfig.Verify(); err != nil {
		return err
	}
	// last, so the tags checked are the ones this config will really write to
	return hosted.VerifyTags(c)
}

// Tags reports every tag this subscription may write to. Each one is
// pre-negotiated with the indexer at startup, so the drop routes, which write
// nothing, contribute nothing.
func (c *Config) Tags() []string {
	seen := make(map[string]struct{}, len(c.routes)+1)
	var tags []string
	add := func(tn string) {
		if tn == `` {
			return
		}
		if _, ok := seen[tn]; !ok {
			seen[tn] = struct{}{}
			tags = append(tags, tn)
		}
	}
	add(c.ResolveTag(entry.DefaultTagName))
	// routes may not be parsed yet if Tags is called before Verify
	routes := c.routes
	if routes == nil {
		routes, _ = c.topicRoutes()
	}
	for _, r := range routes {
		add(r.tag)
	}
	sort.Strings(tags)
	return tags
}

// ClientID is the identity this subscription presents to the broker. It has to
// be stable across restarts or the broker will not hand back the durable
// session, and anything queued in it is lost.
func (c *Config) ClientID(name string) string {
	if c.Client_ID != `` {
		return c.Client_ID
	}
	uuid := c.UUID().String()
	if len(uuid) > 8 {
		uuid = uuid[:8]
	}
	return `gravwell-mqtt-` + uuid + `-` + SanitizeClientName(name)
}

// SanitizeClientName reduces a subscription name to characters that are safe
// in an MQTT client ID.
func SanitizeClientName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
}

// brokerURLs parses each Broker entry into a URL, applying the default port
// appropriate to the scheme.
func (c *Config) brokerURLs() (urls []*url.URL, err error) {
	if len(c.Broker) == 0 {
		return nil, ErrNoBrokers
	}
	for _, b := range c.Broker {
		b = strings.TrimSpace(b)
		if b == `` {
			return nil, errors.New("empty Broker entry")
		}
		if !strings.Contains(b, `://`) {
			b = `mqtt://` + b // bare host:port is the common case
		}
		var u *url.URL
		if u, err = url.Parse(b); err != nil {
			return nil, fmt.Errorf("invalid Broker %q: %w", b, err)
		}
		var defPort uint16
		switch strings.ToLower(u.Scheme) {
		case `mqtt`, `tcp`:
			defPort = defaultMQTTPort
		case `ssl`, `tls`, `mqtts`, `mqtt+ssl`, `tcps`:
			defPort = defaultTLSPort
		case `ws`:
			defPort = defaultWSPort
		case `wss`:
			defPort = defaultWSSPort
		default:
			return nil, fmt.Errorf("invalid Broker %q: unsupported scheme %q", b, u.Scheme)
		}
		if u.Host == `` {
			return nil, fmt.Errorf("invalid Broker %q: missing host", b)
		}
		u.Host = appendDefaultPort(u.Host, defPort)
		if _, _, err = net.SplitHostPort(u.Host); err != nil {
			return nil, fmt.Errorf("invalid Broker %q: %w", b, err)
		}
		urls = append(urls, u)
	}
	return urls, nil
}

// appendDefaultPort adds a port if the host does not already carry one. IPv6
// literals are bracketed, so a bare "::1" is not mistaken for host:port.
func appendDefaultPort(host string, port uint16) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host // already has a port
	}
	if strings.ContainsRune(host, ':') && !strings.HasPrefix(host, `[`) {
		host = `[` + host + `]`
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// topicFilters validates each Topic and applies the Shared-Group wrapper.
func (c *Config) topicFilters() (topics []string, err error) {
	if len(c.Topic) == 0 {
		return nil, ErrNoTopics
	}
	group := strings.TrimSpace(c.Shared_Group)
	if group != `` && strings.ContainsAny(group, "/+#") {
		return nil, fmt.Errorf("invalid Shared-Group %q: cannot contain '/', '+' or '#'", group)
	}
	for _, t := range c.Topic {
		if err = validateTopicFilter(t); err != nil {
			return nil, err
		}
		if group != `` {
			t = `$share/` + group + `/` + t
		}
		topics = append(topics, t)
	}
	return topics, nil
}

// validateTopicFilter applies the MQTT v5 topic filter rules: '#' must be the
// last level on its own, '+' must occupy a whole level, and no nulls.
func validateTopicFilter(t string) error {
	if t == `` {
		return errors.New("empty Topic")
	} else if strings.ContainsRune(t, 0) {
		return fmt.Errorf("invalid Topic %q: contains a null character", t)
	} else if strings.HasPrefix(t, `$share/`) {
		return fmt.Errorf("invalid Topic %q: use Shared-Group rather than a literal $share prefix", t)
	}
	levels := strings.Split(t, string(topicLevelSep))
	for i, l := range levels {
		if strings.Contains(l, `#`) {
			if l != `#` {
				return fmt.Errorf("invalid Topic %q: '#' must occupy an entire level", t)
			} else if i != len(levels)-1 {
				return fmt.Errorf("invalid Topic %q: '#' must be the last level", t)
			}
		}
		if strings.Contains(l, `+`) && l != `+` {
			return fmt.Errorf("invalid Topic %q: '+' must occupy an entire level", t)
		}
	}
	return nil
}

// topicRoutes compiles the Topic-Route list. The format matches the routing
// preprocessors: "<glob>:<tag>", where an empty tag means drop.
func (c *Config) topicRoutes() (routes []topicRoute, err error) {
	for _, r := range c.Topic_Route {
		idx := strings.LastIndex(r, splitChar)
		if idx == -1 {
			return nil, fmt.Errorf("invalid Topic-Route %q: missing %q separator", r, splitChar)
		}
		pattern := strings.TrimSpace(r[:idx])
		tag := strings.TrimSpace(r[idx+1:])
		if pattern == `` {
			return nil, fmt.Errorf("invalid Topic-Route %q: empty match pattern", r)
		}
		tr := topicRoute{pattern: pattern, tag: tag, drop: tag == ``}
		if !tr.drop {
			if err = ingest.CheckTag(tag); err != nil {
				return nil, fmt.Errorf("invalid Topic-Route %q: bad tag %q: %w", r, tag, err)
			}
		}
		if tr.glob, err = glob.Compile(pattern, topicLevelSep); err != nil {
			return nil, fmt.Errorf("invalid Topic-Route %q: %w", r, err)
		}
		routes = append(routes, tr)
	}
	return routes, nil
}

// qosValue parses QoS. It is read as a string rather than an int so that an
// explicit "QoS=0" is distinguishable from an omitted QoS, which gcfg would
// otherwise both present as the zero value.
func (c *Config) qosValue() (q byte, err error) {
	v := strings.TrimSpace(c.QoS)
	if v == `` {
		return defaultQoS, nil
	}
	n, perr := strconv.ParseUint(v, 10, 8)
	if perr != nil || n > 2 {
		return 0, fmt.Errorf("invalid QoS %q: must be 0, 1, or 2", c.QoS)
	}
	return byte(n), nil
}

func (c *Config) sessionExpiryValue() (uint32, error) {
	switch {
	case c.Session_Expiry < 0:
		return 0, errors.New("Session-Expiry cannot be negative")
	case int64(c.Session_Expiry) > maxSessionExpiry:
		// an unchecked conversion would wrap, and a wrapped value of zero
		// means "end the session at disconnect", silently discarding every
		// queued QoS 1/2 message
		return 0, fmt.Errorf("Session-Expiry cannot exceed %d", maxSessionExpiry)
	case c.Session_Expiry == 0:
		return defaultSessionExpiry, nil
	}
	return uint32(c.Session_Expiry), nil
}

// verifyTimestampOptions rejects combinations that would silently do nothing.
func (c *Config) verifyTimestampOptions() error {
	if c.Ignore_Timestamps && c.Extract_Timestamps {
		return errors.New("cannot specify both Ignore-Timestamps and Extract-Timestamps")
	}
	if c.Timezone_Override != `` && c.Assume_Local_Timezone {
		return errors.New("cannot specify Assume-Local-Timezone and Timezone-Override together")
	}
	if !c.Extract_Timestamps {
		// these only feed the timegrinder, so accepting them without
		// Extract-Timestamps would silently do nothing
		switch {
		case c.Timestamp_Format_Override != ``:
			return errors.New("Timestamp-Format-Override requires Extract-Timestamps")
		case c.Timezone_Override != ``:
			return errors.New("Timezone-Override requires Extract-Timestamps")
		case c.Assume_Local_Timezone:
			return errors.New("Assume-Local-Timezone requires Extract-Timestamps")
		}
		return nil
	}
	if c.Timezone_Override != `` {
		if _, err := time.LoadLocation(c.Timezone_Override); err != nil {
			return fmt.Errorf("invalid Timezone-Override %q: %w", c.Timezone_Override, err)
		}
	}
	// build a throwaway to surface a bad format override at config time rather
	// than on the first message
	if _, err := c.newTimeGrinder(); err != nil {
		return err
	}
	return nil
}

// newTimeGrinder builds the timestamp extractor. A TimeGrinder is stateful and
// not safe for concurrent use, so each ingester gets its own rather than
// sharing one hung off the config.
//
// Unlike the standalone ingesters there are no [TimeFormat] sections in the
// hosted runner config, so Timestamp-Format-Override can only name a built-in
// format and it is safe to resolve it here at construction.
func (c *Config) newTimeGrinder() (*timegrinder.TimeGrinder, error) {
	if !c.Extract_Timestamps {
		return nil, nil
	}
	tg, err := timegrinder.NewTimeGrinder(timegrinder.Config{
		EnableLeftMostSeed: true,
		FormatOverride:     c.Timestamp_Format_Override,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate new timegrinder: %w", err)
	}
	if c.Assume_Local_Timezone {
		tg.SetLocalTime()
	}
	if c.Timezone_Override != `` {
		if err = tg.SetTimezone(c.Timezone_Override); err != nil {
			return nil, fmt.Errorf("failed to override timezone: %w", err)
		}
	}
	return tg, nil
}

// tlsConfig assembles a *tls.Config when any TLS material is configured or a
// TLS scheme is in use. It returns nil when TLS is not involved at all.
func (c *Config) tlsConfig(brokers []*url.URL) (*tls.Config, error) {
	// Read the scheme off the already parsed URLs. Sniffing the raw config
	// strings would disagree with brokerURLs over leading whitespace, and a
	// broker that parsed as TLS but missed this check would dial with a nil
	// tls.Config, silently losing the MinVersion floor below.
	var tlsScheme bool
	for _, u := range brokers {
		switch strings.ToLower(u.Scheme) {
		case `ssl`, `tls`, `mqtts`, `mqtt+ssl`, `tcps`, `wss`:
			tlsScheme = true
		}
	}
	hasMaterial := c.CA_Cert != `` || c.Client_Cert != `` || c.Client_Key != ``
	if !tlsScheme {
		// autopaho only applies TlsCfg to a TLS scheme, so accepting these
		// against a plaintext broker would silently dial in the clear and send
		// the credentials with it, while the operator believes TLS is on.
		if hasMaterial {
			return nil, errors.New("CA-Cert, Client-Cert and Client-Key require a TLS Broker scheme (tls://, ssl://, mqtts:// or wss://)")
		}
		if c.Insecure_Skip_TLS_Verify {
			return nil, errors.New("Insecure-Skip-TLS-Verify requires a TLS Broker scheme (tls://, ssl://, mqtts:// or wss://)")
		}
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.Insecure_Skip_TLS_Verify,
	}
	if c.CA_Cert != `` {
		pem, err := os.ReadFile(c.CA_Cert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA-Cert %q: %w", c.CA_Cert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA-Cert %q contains no usable certificates", c.CA_Cert)
		}
		cfg.RootCAs = pool
	}
	if (c.Client_Cert == ``) != (c.Client_Key == ``) {
		return nil, errors.New("Client-Cert and Client-Key must be specified together")
	}
	if c.Client_Cert != `` {
		crt, err := tls.LoadX509KeyPair(c.Client_Cert, c.Client_Key)
		if err != nil {
			return nil, fmt.Errorf("failed to load client keypair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{crt}
	}
	return cfg, nil
}

func boundedUint16(name string, v int, def uint16) (uint16, error) {
	if v < 0 {
		return 0, fmt.Errorf("%s cannot be negative", name)
	} else if v == 0 {
		return def, nil
	} else if v > 65535 {
		return 0, fmt.Errorf("%s cannot exceed 65535", name)
	}
	return uint16(v), nil
}
