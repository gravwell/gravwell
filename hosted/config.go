package hosted

import (
	"cmp"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ParseUUID attempts to parse an ingester UUID string.
// Returns uuid.Nil for empty or invalid values.
func ParseUUID(s string) uuid.UUID {
	if s != "" {
		if u, err := uuid.Parse(s); err == nil {
			return u
		}
	}
	return uuid.Nil
}

// BaseConfig holds fields that are common to all plugin configs.
type BaseConfig struct {
	Ingester_UUID string
}

func (b *BaseConfig) UUID() uuid.UUID {
	return ParseUUID(b.Ingester_UUID)
}

// SingleTagConfig holds a single tag name.
// Used by most ingesters.
type SingleTagConfig struct {
	Tag_Name string
}

// ResolveTag returns Tag_Name if set, otherwise the given default.
func (t *SingleTagConfig) ResolveTag(defaultTag string) string {
	return cmp.Or(t.Tag_Name, defaultTag)
}

// MultiTagConfig holds tag naming for multi-source plugins that emit to multiple tags.
type MultiTagConfig struct {
	Tag_Name   string
	Tag_Prefix string
}

// ValidateTags returns an error if Tag_Name and Tag_Prefix are both set.
func (t *MultiTagConfig) ValidateTags() error {
	if t.Tag_Name != "" && t.Tag_Prefix != "" {
		return errors.New("Tag-Name and Tag-Prefix cannot be used together")
	}
	return nil
}

// ResolveTag returns the tag for a given kind and default prefix.
func (t *MultiTagConfig) ResolveTag(kind, defaultPrefix string) string {
	if t.Tag_Name != "" {
		return t.Tag_Name
	}
	if t.Tag_Prefix != "" {
		return t.Tag_Prefix + "-" + kind
	}
	return defaultPrefix + "-" + kind
}

// PollingConfig holds rate limiting and scheduling fields common to poll-based plugins.
type PollingConfig struct {
	Lookback            int // In hours. How far back to fetch on the first run.
	Requests_Per_Minute int
	Request_Interval    int // In seconds between poll cycles.
}

// ApplyDefaults sets zero-value fields to the provided defaults.
func (p *PollingConfig) ApplyDefaults(lookback, rpm, interval int) {
	p.Lookback = cmp.Or(p.Lookback, lookback)
	p.Requests_Per_Minute = cmp.Or(p.Requests_Per_Minute, rpm)
	p.Request_Interval = cmp.Or(p.Request_Interval, interval)
}

// LookbackDuration returns the configured lookback as a time.Duration.
func (p *PollingConfig) LookbackDuration() time.Duration {
	return time.Duration(p.Lookback) * time.Hour
}

// ContinueAfterInterval returns a Continuation scheduled after the configured poll interval.
func (p *PollingConfig) ContinueAfterInterval() *Continuation {
	return ContinueAfter(time.Duration(p.Request_Interval) * time.Second)
}
