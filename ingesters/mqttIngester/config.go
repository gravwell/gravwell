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
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/gravwell/gravwell/v4/ingest"
	"github.com/gravwell/gravwell/v4/ingest/attach"
	"github.com/gravwell/gravwell/v4/ingest/config"
	"github.com/gravwell/gravwell/v4/ingest/processors"
	"github.com/gravwell/gravwell/v4/timegrinder"
)

const (
	// splitChar matches the convention used by the routing preprocessors
	// (see ingest/processors/regexrouter.go) - "<match>:<tag>" with an empty
	// tag meaning "drop this entry".
	splitChar = `:`

	defaultQoS           byte   = 1
	defaultMaxInFlight   uint16 = 512
	defaultSessionExpiry uint32 = 600
	defaultKeepAlive     uint16 = 30

	// maxSessionExpiry is the MQTT v5 ceiling, the field is a uint32 of seconds
	maxSessionExpiry = 4294967295

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

type ConfigSubscription struct {
	Broker       []string // repeatable, failover list
	Client_ID    string
	Topic        []string // repeatable MQTT topic filters
	Shared_Group string   // wraps each Topic as $share/<group>/<filter>
	QoS          string
	Default_Tag  string

	// Topic_Route is a repeatable "<topic glob>:<tag>" list. An empty tag
	// drops matching messages. First match wins.
	Topic_Route []string

	Username string
	Password string

	CA_Cert                  string
	Client_Cert              string
	Client_Key               string
	Insecure_Skip_TLS_Verify bool

	Max_In_Flight  int
	Session_Expiry int
	Keep_Alive     int
	Synchronous    bool

	// Disable_Metadata turns off the topic/qos/retain/user-property
	// enumerated values. Metadata is attached by default.
	Disable_Metadata bool

	Source_Override string

	Ignore_Timestamps         bool
	Extract_Timestamps        bool
	Assume_Local_Timezone     bool
	Timezone_Override         string
	Timestamp_Format_Override string

	Preprocessor []string
}

// topicRoute is a compiled Topic-Route entry.
type topicRoute struct {
	pattern string
	glob    glob.Glob
	tag     string // empty means drop
	drop    bool
}

// subCfg is the validated, derived form of a ConfigSubscription.
type subCfg struct {
	name          string
	brokers       []*url.URL
	clientID      string
	topics        []string // already Shared-Group wrapped
	qos           byte
	defTag        string
	routes        []topicRoute
	username      string
	password      string
	tlsCfg        *tls.Config
	maxInFlight   uint16
	sessionExpiry uint32
	keepAlive     uint16
	sync          bool
	attachMeta    bool
	srcOverride   net.IP
	ignoreTS      bool
	extractTS     bool
	tg            *timegrinder.TimeGrinder
	timeWindow    timegrinder.TimestampWindow
	preprocessor  []string
}

type cfgReadType struct {
	Global       config.IngestConfig
	Attach       attach.AttachConfig `gcfg:",section=raw,ident=regex"`
	Subscription map[string]*ConfigSubscription
	Preprocessor processors.ProcessorConfig
	TimeFormat   config.CustomTimeFormat
}

type cfgType struct {
	config.IngestConfig
	Attach        attach.AttachConfig `gcfg:",section=raw,ident=regex"`
	Subscriptions map[string]*subCfg
	Preprocessor  processors.ProcessorConfig
	TimeFormat    config.CustomTimeFormat
}

func GetConfig(path, overlayPath string) (*cfgType, error) {
	var cr cfgReadType
	if err := config.LoadConfigFile(&cr, path); err != nil {
		return nil, err
	} else if err = config.LoadConfigOverlays(&cr, overlayPath); err != nil {
		return nil, err
	}

	c := &cfgType{
		IngestConfig:  cr.Global,
		Attach:        cr.Attach,
		Subscriptions: make(map[string]*subCfg, len(cr.Subscription)),
		Preprocessor:  cr.Preprocessor,
		TimeFormat:    cr.TimeFormat,
	}

	for k, v := range cr.Subscription {
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return nil, fmt.Errorf("Subscription %s preprocessor invalid: %w", k, err)
		}
		sub, err := v.validateAndProcess(k, cr)
		if err != nil {
			return nil, fmt.Errorf("Subscription %s: %w", k, err)
		}
		if sub.tg != nil {
			if err = c.TimeFormat.LoadFormats(sub.tg); err != nil {
				return nil, err
			}
			if v.Timestamp_Format_Override != `` {
				if err = sub.tg.SetFormatOverride(v.Timestamp_Format_Override); err != nil {
					return nil, err
				}
			}
		}
		c.Subscriptions[k] = &sub
	}
	if err := c.Verify(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cfgType) Verify() error {
	if err := c.IngestConfig.Verify(); err != nil {
		return err
	} else if err = c.Attach.Verify(); err != nil {
		return err
	} else if len(c.Subscriptions) == 0 {
		return errors.New("no subscriptions defined")
	} else if err = c.Preprocessor.Validate(); err != nil {
		return err
	} else if err = c.TimeFormat.Validate(); err != nil {
		return err
	} else if err = c.checkClientIDs(); err != nil {
		return err
	}
	return nil
}

// checkClientIDs rejects subscriptions that would share an MQTT client ID. A
// broker evicts the existing session when a second client connects with the
// same ID, so a collision puts the two subscriptions in a permanent loop of
// kicking each other off. Note that the generated IDs can collide even when
// the section names differ, because SanitizeClientName is not injective.
func (c *cfgType) checkClientIDs() error {
	seen := make(map[string]string, len(c.Subscriptions))
	names := make([]string, 0, len(c.Subscriptions))
	for name := range c.Subscriptions {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic error messages
	for _, name := range names {
		key := c.Subscriptions[name].clientIDKey(name)
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("Subscriptions %s and %s resolve to the same MQTT Client-ID %q, set a distinct Client-ID on one of them", prev, name, key)
		}
		seen[key] = name
	}
	return nil
}

// clientIDKey is the identity this subscription will present to the broker:
// the explicit Client-ID if there is one, otherwise the sanitized section name
// that defaultClientID will build on.
func (sc *subCfg) clientIDKey(name string) string {
	if sc.clientID != `` {
		return sc.clientID
	}
	return SanitizeClientName(name)
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

// Tags returns the union of every tag any subscription might write to so that
// the base package can pre-negotiate them with the muxer.
func (c *cfgType) Tags() (tgs []string, err error) {
	tagMp := make(map[string]bool, len(c.Subscriptions))
	add := func(tn string) {
		if tn == `` {
			return
		}
		if !tagMp[tn] {
			tgs = append(tgs, tn)
			tagMp[tn] = true
		}
	}
	for name, v := range c.Subscriptions {
		if v.defTag == `` {
			return nil, fmt.Errorf("Subscription %s is missing Default-Tag definition", name)
		} else if err = ingest.CheckTag(v.defTag); err != nil {
			return nil, fmt.Errorf("Subscription %s Default-Tag is invalid: %w", name, err)
		}
		add(v.defTag)
		for _, r := range v.routes {
			add(r.tag) // empty (drop) routes add nothing
		}
	}
	if len(tgs) == 0 {
		return nil, errors.New("No tags specified")
	}
	sort.Strings(tgs)
	return tgs, nil
}

func (c *cfgType) IngestBaseConfig() config.IngestConfig {
	return c.IngestConfig
}

func (c *cfgType) AttachConfig() attach.AttachConfig {
	return c.Attach
}

func (cs ConfigSubscription) validateAndProcess(name string, cr cfgReadType) (c subCfg, err error) {
	c.name = name

	if len(cs.Default_Tag) == 0 {
		err = errors.New("missing Default-Tag")
		return
	} else if err = ingest.CheckTag(cs.Default_Tag); err != nil {
		err = fmt.Errorf("invalid Default-Tag: %w", err)
		return
	}
	c.defTag = cs.Default_Tag

	if c.brokers, err = cs.brokerURLs(); err != nil {
		return
	}
	if c.topics, err = cs.topicFilters(); err != nil {
		return
	}
	if c.routes, err = cs.topicRoutes(); err != nil {
		return
	}
	if c.qos, err = cs.qosValue(); err != nil {
		return
	}
	if c.tlsCfg, err = cs.tlsConfig(c.brokers); err != nil {
		return
	}

	c.clientID = strings.TrimSpace(cs.Client_ID)
	c.username = cs.Username
	c.password = cs.Password
	c.sync = cs.Synchronous
	c.attachMeta = !cs.Disable_Metadata
	c.preprocessor = cs.Preprocessor

	if c.maxInFlight, err = boundedUint16("Max-In-Flight", cs.Max_In_Flight, defaultMaxInFlight); err != nil {
		return
	}
	if c.keepAlive, err = boundedUint16("Keep-Alive", cs.Keep_Alive, defaultKeepAlive); err != nil {
		return
	}
	if cs.Session_Expiry < 0 {
		err = errors.New("Session-Expiry cannot be negative")
		return
	} else if cs.Session_Expiry > maxSessionExpiry {
		// an unchecked conversion would wrap, and a wrapped value of zero
		// means "end the session at disconnect", silently discarding every
		// queued QoS 1/2 message
		err = fmt.Errorf("Session-Expiry cannot exceed %d", maxSessionExpiry)
		return
	} else if cs.Session_Expiry == 0 {
		c.sessionExpiry = defaultSessionExpiry
	} else {
		c.sessionExpiry = uint32(cs.Session_Expiry)
	}

	if len(cs.Source_Override) > 0 {
		if c.srcOverride = net.ParseIP(cs.Source_Override); c.srcOverride == nil {
			err = fmt.Errorf("invalid Source-Override %q", cs.Source_Override)
			return
		}
	}

	if cs.Ignore_Timestamps && cs.Extract_Timestamps {
		err = errors.New("cannot specify both Ignore-Timestamps and Extract-Timestamps")
		return
	}
	if !cs.Extract_Timestamps {
		// these only feed the timegrinder, so accepting them without
		// Extract-Timestamps would silently do nothing
		switch {
		case cs.Timestamp_Format_Override != ``:
			err = errors.New("Timestamp-Format-Override requires Extract-Timestamps")
		case cs.Timezone_Override != ``:
			err = errors.New("Timezone-Override requires Extract-Timestamps")
		case cs.Assume_Local_Timezone:
			err = errors.New("Assume-Local-Timezone requires Extract-Timestamps")
		}
		if err != nil {
			return
		}
	}
	if cs.Timezone_Override != `` && cs.Assume_Local_Timezone {
		err = errors.New("cannot specify Assume-Local-Timezone and Timezone-Override in the same subscription")
		return
	}
	if cs.Timezone_Override != `` {
		if _, err = time.LoadLocation(cs.Timezone_Override); err != nil {
			err = fmt.Errorf("invalid Timezone-Override %q: %w", cs.Timezone_Override, err)
			return
		}
	}
	if c.timeWindow, err = cr.Global.GlobalTimestampWindow(); err != nil {
		err = fmt.Errorf("failed to get global timestamp window: %w", err)
		return
	}

	c.ignoreTS = cs.Ignore_Timestamps
	if cs.Extract_Timestamps {
		c.extractTS = true
		// Timestamp_Format_Override is deliberately NOT set here. It would
		// make NewTimeGrinder resolve the override immediately, against the
		// built-in processors only, so a custom [TimeFormat] section could
		// never be named. GetConfig applies it after LoadFormats instead.
		tcfg := timegrinder.Config{
			EnableLeftMostSeed: true,
			TSWindow:           c.timeWindow,
		}
		if c.tg, err = timegrinder.NewTimeGrinder(tcfg); err != nil {
			err = fmt.Errorf("failed to generate new timegrinder: %w", err)
			return
		}
		if cs.Assume_Local_Timezone {
			c.tg.SetLocalTime()
		}
		if cs.Timezone_Override != `` {
			if err = c.tg.SetTimezone(cs.Timezone_Override); err != nil {
				err = fmt.Errorf("failed to override timezone: %w", err)
				return
			}
		}
	}
	return
}

// brokerURLs parses each Broker entry into a URL, applying the default port
// appropriate to the scheme.
func (cs ConfigSubscription) brokerURLs() (urls []*url.URL, err error) {
	if len(cs.Broker) == 0 {
		err = ErrNoBrokers
		return
	}
	for _, b := range cs.Broker {
		b = strings.TrimSpace(b)
		if b == `` {
			err = errors.New("empty Broker entry")
			return
		}
		if !strings.Contains(b, `://`) {
			b = `mqtt://` + b // bare host:port is the common case
		}
		var u *url.URL
		if u, err = url.Parse(b); err != nil {
			err = fmt.Errorf("invalid Broker %q: %w", b, err)
			return
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
			err = fmt.Errorf("invalid Broker %q: unsupported scheme %q", b, u.Scheme)
			return
		}
		if u.Host == `` {
			err = fmt.Errorf("invalid Broker %q: missing host", b)
			return
		}
		u.Host = config.AppendDefaultPort(u.Host, defPort)
		if _, _, err = net.SplitHostPort(u.Host); err != nil {
			err = fmt.Errorf("invalid Broker %q: %w", b, err)
			return
		}
		urls = append(urls, u)
	}
	return
}

// topicFilters validates each Topic and applies the Shared-Group wrapper.
func (cs ConfigSubscription) topicFilters() (topics []string, err error) {
	if len(cs.Topic) == 0 {
		err = ErrNoTopics
		return
	}
	group := strings.TrimSpace(cs.Shared_Group)
	if group != `` {
		if strings.ContainsAny(group, "/+#") {
			err = fmt.Errorf("invalid Shared-Group %q: cannot contain '/', '+' or '#'", group)
			return
		}
	}
	for _, t := range cs.Topic {
		if err = validateTopicFilter(t); err != nil {
			return
		}
		if group != `` {
			t = `$share/` + group + `/` + t
		}
		topics = append(topics, t)
	}
	return
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
func (cs ConfigSubscription) topicRoutes() (routes []topicRoute, err error) {
	for _, r := range cs.Topic_Route {
		idx := strings.LastIndex(r, splitChar)
		if idx == -1 {
			err = fmt.Errorf("invalid Topic-Route %q: missing %q separator", r, splitChar)
			return
		}
		pattern := strings.TrimSpace(r[:idx])
		tag := strings.TrimSpace(r[idx+1:])
		if pattern == `` {
			err = fmt.Errorf("invalid Topic-Route %q: empty match pattern", r)
			return
		}
		tr := topicRoute{pattern: pattern, tag: tag, drop: tag == ``}
		if !tr.drop {
			if err = ingest.CheckTag(tag); err != nil {
				err = fmt.Errorf("invalid Topic-Route %q: bad tag %q: %w", r, tag, err)
				return
			}
		}
		if tr.glob, err = glob.Compile(pattern, topicLevelSep); err != nil {
			err = fmt.Errorf("invalid Topic-Route %q: %w", r, err)
			return
		}
		routes = append(routes, tr)
	}
	return
}

// qosValue parses QoS. It is read as a string rather than an int so that an
// explicit "QoS=0" is distinguishable from an omitted QoS, which gcfg would
// otherwise both present as the zero value.
func (cs ConfigSubscription) qosValue() (q byte, err error) {
	v := strings.TrimSpace(cs.QoS)
	if v == `` {
		return defaultQoS, nil
	}
	n, perr := strconv.ParseUint(v, 10, 8)
	if perr != nil || n > 2 {
		return 0, fmt.Errorf("invalid QoS %q: must be 0, 1, or 2", cs.QoS)
	}
	return byte(n), nil
}

// tlsConfig assembles a *tls.Config when any TLS material is configured or a
// TLS scheme is in use. It returns nil when TLS is not involved at all.
func (cs ConfigSubscription) tlsConfig(brokers []*url.URL) (cfg *tls.Config, err error) {
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
	hasMaterial := cs.CA_Cert != `` || cs.Client_Cert != `` || cs.Client_Key != ``
	if !tlsScheme && !hasMaterial && !cs.Insecure_Skip_TLS_Verify {
		return
	}

	cfg = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cs.Insecure_Skip_TLS_Verify,
	}
	if cs.CA_Cert != `` {
		var pem []byte
		if pem, err = os.ReadFile(cs.CA_Cert); err != nil {
			err = fmt.Errorf("failed to read CA-Cert %q: %w", cs.CA_Cert, err)
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA-Cert %q contains no usable certificates", cs.CA_Cert)
		}
		cfg.RootCAs = pool
	}
	if (cs.Client_Cert == ``) != (cs.Client_Key == ``) {
		return nil, errors.New("Client-Cert and Client-Key must be specified together")
	}
	if cs.Client_Cert != `` {
		var crt tls.Certificate
		if crt, err = tls.LoadX509KeyPair(cs.Client_Cert, cs.Client_Key); err != nil {
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
