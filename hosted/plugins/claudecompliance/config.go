package claudecompliance

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"slices"
	"sort"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
)

const (
	Name    = "claudecompliance"
	ID      = "claudecompliance.ingesters.gravwell.io"
	Version = "1.0.0"

	// defaultHost is the public Compliance API endpoint. Host exists so the
	// plugin can be pointed at a mock, a proxy, or a future region-specific
	// endpoint.
	defaultHost = "https://api.anthropic.com"
	// defaultIngesterUUID is used when a stanza does not set its own.
	defaultIngesterUUID = "5f9b2c14-1d73-4f0e-9a6c-2b8e7d4a3c61"
	// apiVersion is the value of the required anthropic-version header.
	apiVersion = "2023-06-01"
	// defaultTagPrefix produces one tag per semantic family, not per endpoint.
	defaultTagPrefix = "claude-compliance"

	// maxResponseBytes caps a single decoded response held in memory at once.
	// This is a guard against an unbounded read, not a data policy: it is far
	// above any page the documented limits can produce (the largest page is
	// 5000 activity records).
	maxResponseBytes = 64 << 20
	// maxCredentialBytes caps the credential file. API keys are short; this
	// only exists so a wrong path cannot read something enormous.
	maxCredentialBytes = 16384
)

var _ hosted.Config = (*Config)(nil)

const (
	// maxLookbackHours is the largest lookback that still converts to a
	// time.Duration. Beyond it the multiplication wraps negative and a
	// window's lower bound would land after its upper bound.
	maxLookbackHours = int64(math.MaxInt64) / int64(time.Hour)
	// maxIntervalSeconds is the largest poll interval that still converts to
	// a time.Duration. Beyond it the scheduled delay wraps negative, which
	// the runner reads as "run again immediately" -- a hot success loop.
	// The shared PollingConfig does not bound this, so it is checked here
	// rather than by widening shared Hosted Runner behavior.
	maxIntervalSeconds = int64(math.MaxInt64) / int64(time.Second)
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig
	Host            string
	Dataset         []string
	Credential      string `json:"-"` // DO NOT send this when marshalling
	Credential_File string `json:"-"` // DO NOT send this when marshalling
	Parameter       []string
	Page_Size       int
	Max_Pages       int
	Max_Retries     int
	Overlap_Seconds int
	Follow_Children string
	Max_Children    int
	Max_Pending     int

	dataset    Dataset
	path       string
	discovered bool
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// parametersFor returns the subset of params whose names appear as
// placeholders in d's path, preserving the caller's order.
func parametersFor(d Dataset, params []string) []string {
	needed := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(d.Path, -1) {
		needed[m[1]] = true
	}
	var out []string
	for _, p := range params {
		if k, _, ok := strings.Cut(p, ":"); ok && needed[k] {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUID)
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	c.PollingConfig.ApplyDefaults(24, 60, 300)
	if c.Lookback < 1 || int64(c.Lookback) > maxLookbackHours {
		return errors.New("Lookback must be a positive, representable number of hours")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 600 {
		return errors.New("invalid rate")
	}
	if c.Request_Interval < 60 || int64(c.Request_Interval) > maxIntervalSeconds {
		return errors.New("Request-Interval must be at least 60 seconds and representable as a duration")
	}
	if c.Host == "" {
		c.Host = defaultHost
	}
	// Host is an origin and nothing else. Userinfo would smuggle credentials
	// into every request URL, and a query or fragment would be silently
	// dropped when the request path and query are appended.
	u, err := url.Parse(strings.TrimSuffix(c.Host, "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return errors.New("Host must be a bare https origin such as https://api.anthropic.com, with no credentials, path, query, or fragment")
	}
	c.Host = u.Scheme + "://" + u.Host

	if len(c.Dataset) == 0 {
		return errors.New("at least one Dataset is required")
	}
	seenDataset := map[string]bool{}
	for _, name := range c.Dataset {
		if seenDataset[name] {
			return fmt.Errorf("duplicate Dataset %s", name)
		}
		seenDataset[name] = true
		if _, ok := Datasets[name]; !ok {
			return errors.New("unknown Compliance JSON dataset")
		}
	}
	// A single-dataset config is a bound config: it is the unit the collector
	// actually runs against, so it carries the resolved dataset and path.
	if len(c.Dataset) == 1 {
		c.dataset = Datasets[c.Dataset[0]]
	}

	if c.Follow_Children == "" {
		c.Follow_Children = "enabled"
	}
	if c.Follow_Children != "enabled" && c.Follow_Children != "disabled" {
		return errors.New("Follow-Children must be enabled or disabled")
	}
	if c.Max_Children == 0 {
		c.Max_Children = 100
	}
	if c.Max_Pending == 0 {
		c.Max_Pending = 10000
	}
	if c.Max_Children < 1 || c.Max_Children > 1000 || c.Max_Pending < c.Max_Children || c.Max_Pending > 100000 {
		return errors.New("invalid child-work bounds")
	}
	if _, err := c.credential(); err != nil {
		return err
	}

	params := map[string]string{}
	for _, s := range c.Parameter {
		k, v, ok := strings.Cut(s, ":")
		if !ok || k == "" || v == "" || strings.ContainsAny(v, "/\\?#") {
			return errors.New("Parameter must contain name:opaque-id")
		}
		if _, exists := params[k]; exists {
			return errors.New("duplicate Parameter")
		}
		params[k] = v
	}
	// Every placeholder of every selected dataset must be supplied, and every
	// supplied Parameter must be used by at least one of them.
	used := map[string]bool{}
	for _, name := range c.Dataset {
		d := Datasets[name]
		p := d.Path
		for _, m := range placeholder.FindAllStringSubmatch(d.Path, -1) {
			v, ok := params[m[1]]
			if !ok {
				return fmt.Errorf("missing Parameter %s for Dataset %s", m[1], name)
			}
			p = strings.ReplaceAll(p, m[0], url.PathEscape(v))
			used[m[1]] = true
		}
		if len(c.Dataset) == 1 {
			c.path = "/v1/compliance" + p
		}
	}
	for k := range params {
		if !used[k] {
			return errors.New("unused Parameter")
		}
	}

	if err := c.ValidateTags(); err != nil {
		return err
	}
	for _, tag := range c.Tags() {
		if err := ingest.CheckTag(tag); err != nil {
			return err
		}
	}

	if c.Page_Size == 0 {
		c.Page_Size = 100
	}
	if c.Page_Size < 1 {
		return errors.New("Page-Size must be positive")
	}
	// Clamp to the bound endpoint's documented maximum. Doing it here rather
	// than only in bind keeps every execution path -- a single-dataset
	// stanza, a multi-dataset fan-out, and every discovered child -- on the
	// same ceiling.
	if len(c.Dataset) == 1 {
		if d := Datasets[c.Dataset[0]]; d.Limit > 0 && c.Page_Size > d.Limit {
			c.Page_Size = d.Limit
		}
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = 1000
	}
	if c.Max_Pages < 1 || c.Max_Pages > 10000 {
		return errors.New("Max-Pages outside 1..10000")
	}
	if c.Max_Retries == 0 {
		c.Max_Retries = 4
	}
	if c.Max_Retries < 1 || c.Max_Retries > 10 {
		return errors.New("invalid Max-Retries")
	}
	if c.Overlap_Seconds == 0 {
		c.Overlap_Seconds = 300
	}
	if c.Overlap_Seconds < 1 || c.Overlap_Seconds > 86400 {
		return errors.New("invalid overlap")
	}
	return nil
}

// tag is the resolved destination tag for this bound config's family.
func (c *Config) tag() string { return c.ResolveTag(c.dataset.Tag, defaultTagPrefix) }

// Tags lists every distinct destination tag this stanza can write to, in a
// stable order.
func (c *Config) Tags() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range c.Dataset {
		t := c.ResolveTag(Datasets[name].Tag, defaultTagPrefix)
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// bind returns a copy of c restricted to one selected dataset. The collector
// only ever runs against a bound config, so a stanza listing several datasets
// is simply several bound configs sharing one set of polling settings.
func (c *Config) bind(name string) (*Config, error) {
	d, ok := Datasets[name]
	if !ok {
		return nil, errors.New("unknown Compliance JSON dataset")
	}
	b := *c
	b.Dataset = []string{name}
	// Keep only the parameters this dataset's path actually consumes. A
	// stanza may legitimately select datasets with disjoint placeholders,
	// and a sibling's parameter is not "unused" just because this dataset
	// does not take it.
	b.Parameter = parametersFor(d, c.Parameter)
	// Verify clamps Page-Size to this endpoint's documented maximum.
	if err := b.Verify(); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Config) Equal(other any) bool {
	n, ok := hosted.EqualTarget[Config](other)
	if c == nil || !ok {
		return false
	}
	return c.Ingester_UUID == n.Ingester_UUID &&
		c.Tag_Name == n.Tag_Name &&
		c.Tag_Prefix == n.Tag_Prefix &&
		c.Lookback == n.Lookback &&
		c.Requests_Per_Minute == n.Requests_Per_Minute &&
		c.Request_Interval == n.Request_Interval &&
		c.Host == n.Host &&
		slices.Equal(c.Dataset, n.Dataset) &&
		c.Credential == n.Credential &&
		c.Credential_File == n.Credential_File &&
		slices.Equal(c.Parameter, n.Parameter) &&
		c.Page_Size == n.Page_Size &&
		c.Max_Pages == n.Max_Pages &&
		c.Max_Retries == n.Max_Retries &&
		c.Overlap_Seconds == n.Overlap_Seconds &&
		c.Follow_Children == n.Follow_Children &&
		c.Max_Children == n.Max_Children &&
		c.Max_Pending == n.Max_Pending
}

// SanitizedConfig reports what is safe to surface upstream. The credential,
// its file path, and every opaque vendor identifier are omitted: a Parameter
// value is a customer's organization, chat, project or session id, so only
// the parameter names are reported. Host is already validated to be a bare
// origin, so it carries no userinfo or query to leak.
func (c *Config) SanitizedConfig() any {
	names := make([]string, 0, len(c.Parameter))
	for _, p := range c.Parameter {
		if k, _, ok := strings.Cut(p, ":"); ok {
			names = append(names, k)
		}
	}
	return struct {
		Host, FollowChildren            string
		Dataset, Tag                    []string
		ParameterNames                  []string
		Lookback, RequestsPerMinute     int
		RequestInterval, OverlapSeconds int
		PageSize, MaxPages, MaxRetries  int
		MaxChildren, MaxPending         int
	}{
		Host: c.Host, FollowChildren: c.Follow_Children,
		Dataset: append([]string(nil), c.Dataset...), Tag: c.Tags(),
		ParameterNames:    names,
		Lookback:          c.Lookback,
		RequestsPerMinute: c.Requests_Per_Minute,
		RequestInterval:   c.Request_Interval, OverlapSeconds: c.Overlap_Seconds,
		PageSize: c.Page_Size, MaxPages: c.Max_Pages, MaxRetries: c.Max_Retries,
		MaxChildren: c.Max_Children, MaxPending: c.Max_Pending,
	}
}

// key names this dataset's checkpoint. The runtime already hands every
// ingester its own storage bucket keyed by kind/name/ingester-UUID, so the
// only thing this has to separate is one dataset path from another within
// that bucket.
func (c *Config) key() string { return "dataset" + c.path }

// credential returns the API key, preferring an inline Credential over
// Credential-File. The file is read whole after it is confirmed to be a
// regular file of sane size, so a short read cannot silently truncate a key.
func (c *Config) credential() (string, error) {
	if c.Credential != "" {
		if strings.ContainsAny(c.Credential, "\r\n") {
			return "", errors.New("invalid Compliance credential")
		}
		return c.Credential, nil
	}
	if c.Credential_File == "" {
		return "", errors.New("one of Credential or Credential-File is required")
	}
	st, err := os.Stat(c.Credential_File)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxCredentialBytes {
		return "", errors.New("invalid Compliance credential file")
	}
	b, err := os.ReadFile(c.Credential_File)
	if err != nil {
		return "", errors.New("cannot read Compliance credential file")
	}
	if len(b) > maxCredentialBytes {
		return "", errors.New("invalid Compliance credential file")
	}
	v := strings.TrimSpace(string(b))
	if v == "" || strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid Compliance credential")
	}
	return v, nil
}
