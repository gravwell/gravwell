package hosted

import (
	"cmp"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/ingest"
)

var (
	ErrInvalidConfigValue = errors.New("invalid config value")
)

// ParseUUID attempts to parse an ingester UUID string.
// Returns uuid.Nil() for empty or invalid values.
func ParseUUID(s string) uuid.UUID {
	if s != "" {
		if u, err := uuid.Parse(s); err == nil {
			return u
		}
	}
	return uuid.Nil()
}

// BaseConfig holds fields that are common to all plugin configs.
type BaseConfig struct {
	Ingester_UUID string
}

func (b *BaseConfig) UUID() uuid.UUID {
	return ParseUUID(b.Ingester_UUID)
}

// ApplyDefaultIngesterUUID applies a default ingester UUID if none is provided.
func (b *BaseConfig) ApplyDefaultIngesterUUID(defaultIngesterUUID string) {
	b.Ingester_UUID = cmp.Or(b.Ingester_UUID, defaultIngesterUUID)
}

// Verify validates required fields.
// Returns ErrInvalidConfigValue if something fails validation.
func (b *BaseConfig) Verify() error {
	if _, err := uuid.Parse(b.Ingester_UUID); err != nil {
		return fmt.Errorf("%w: invalid Ingester-UUID %q %w", ErrInvalidConfigValue, b.Ingester_UUID, err)
	}
	return nil
}

// TagProvider is implemented by a config that knows every tag it will write to, with the
// defaults and prefixes it was configured with already resolved.
//
// Every tag taking plugin implements it, and VerifyTags takes it rather than an any on
// purpose: a plugin that stops implementing it should fail to build rather than quietly
// stop being checked.
type TagProvider interface {
	Tags() []string
}

// VerifyTags checks every tag a config will actually write to.
//
// The resolved set is checked rather than the fields it was built from, because a
// Tag-Prefix is not a tag, it is half of one: whether it yields something legal depends on
// what it gets joined to, and only the config knows that.  Asking it what it will write to
// answers the question exactly, and covers per source overrides at the same time.
//
// Call this at the end of Verify, after defaults have been applied and any override
// strings parsed.  Tags resolved before that are not the tags the plugin will use.
//
// A tag that the indexer will refuse is not a cosmetic problem: the ingester comes up,
// fails to negotiate the tag, and does not ingest.  Catching it in Verify is what turns
// that into an error an operator can see against the configuration that caused it.
func VerifyTags(tp TagProvider) error {
	if tp == nil {
		return nil
	}
	for _, tag := range tp.Tags() {
		if err := ingest.CheckTag(tag); err != nil {
			return fmt.Errorf("%w: invalid tag %q: %w", ErrInvalidConfigValue, tag, err)
		}
	}
	return nil
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

// PendingOrInterval returns ContinueNow if pending is true, otherwise ContinueAfterInterval.
// Use this at the end of Handle to collapse the common pagination fan-out pattern.
func (p *PollingConfig) PendingOrInterval(pending bool) *Continuation {
	return ContinueNowOrAfter(pending, time.Duration(p.Request_Interval)*time.Second)
}
