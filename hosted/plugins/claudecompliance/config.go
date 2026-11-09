package claudecompliance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

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

	// maxResponseBytes caps one response body before JSON decoding. The page
	// decoder also streams array elements and limits top-level fields, so this
	// byte ceiling is not multiplied into millions of retained RawMessages.
	// Thirty-two MiB still leaves substantial headroom above documented pages.
	maxResponseBytes = 32 << 20
	// maxCredentialBytes caps the credential file. API keys are short; this
	// only exists so a wrong path cannot read something enormous.
	maxCredentialBytes = 16384
	// maxPaginationTokenBytes keeps an untrusted continuation token from
	// inflating request URLs, in-memory loop detection, and persisted state.
	maxPaginationTokenBytes = 4096
	// maxEnvelopeFields rejects pathological top-level key floods before they
	// can spend unbounded CPU in the streaming envelope decoder. Documented
	// responses use fewer than ten fields.
	maxEnvelopeFields = 32
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
	Credential      string   `json:"-"` // DO NOT send this when marshalling
	Credential_File string   `json:"-"` // DO NOT send this when marshalling
	Parameter       []string `json:"-"` // opaque customer identifiers; omit from raw config reports
	Page_Size       int
	Max_Pages       int
	Max_Retries     int
	Overlap_Seconds int
	Follow_Children string
	Max_Children    int
	Max_Pending     int

	dataset Dataset
	path    string
	// discovered distinguishes a child this stanza reached through discovery
	// from a dataset the stanza selects by configuration. The two are separate
	// logical owners of their progress even when they resolve to one dataset
	// and one path, so only a configured bind owns a stanza-level checkpoint
	// namespace (see key).
	discovered bool
	// stateKey, when set, is the physical checkpoint namespace this bound
	// config owns outright. Only childConfig sets it, from the numbered slot
	// the owning root's durable worklist allocated (see childStateKey).
	stateKey string
	// sourcePrefix names the finite state namespace this stanza currently
	// collects into. It is resolved against the stanza's source record before
	// every collection cycle, so rotating a credential selects a different
	// namespace and can never reuse another tenant's checkpoint.
	sourcePrefix string
	// sourceID is the one-way fingerprint of the configured origin and current
	// credential that sourcePrefix was selected for. It is never credential
	// material and is never reported.
	sourceID string
	// parentContext contains only parameters derived from vendor records.
	// Operator-configured parameters remain available in Parameter for request
	// binding but are never emitted through the _parent intrinsic field.
	parentContext []string
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

func invalidOpaqueID(v string) bool {
	if v == "." || v == ".." || strings.ContainsAny(v, "/\\?#,\"") {
		return true
	}
	// An identifier is stored three times over in the child worklist, so its
	// serialized cost has to stay proportional to its length or a nominally
	// bounded identifier could inflate persisted state several-fold. Together
	// with the rejections above and the control-character check below, this
	// leaves no character that encoding/json escapes, so one byte of
	// identifier is one byte of JSON. Invalid UTF-8 is rejected for the same
	// reason -- each bad byte becomes a six-byte escape -- and the Unicode
	// line separators are rejected both for that and because this ingester
	// frames one event per entry.
	if !utf8.ValidString(v) || strings.ContainsAny(v, "<>&  ") {
		return true
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// sourceFingerprint is the one-way name of the origin and credential a stanza
// collects with. It never appears in a state key: the key carries the finite
// slot the stanza's source record assigned to this fingerprint instead,
// because hosted.Storage cannot remove a key and a fingerprint-named namespace
// would therefore leave one unreclaimable key set behind on every rotation.
func sourceFingerprint(host, credential string) string {
	sum := sha256.Sum256([]byte(host + "\x00" + credential))
	return hex.EncodeToString(sum[:])
}

// selectNamespace binds c to the finite state namespace that belongs to
// fingerprint, recording the selection durably before anything is collected
// under it. It is phase one of a source handover and deliberately clears
// nothing: a changed credential is not evidence that the replacement works. A
// mistyped or expired API key is non-empty and fingerprints to a namespace of
// its own exactly as a valid one does, and reclaiming the previous namespace on
// the strength of that would destroy a live source's lower bound, manifests,
// child worklist, cleanup backlog and tombstones before anything had read a
// single record under the new one -- so restoring the key would restart that
// source at Lookback and skip every record older than it. Only
// reclaimSupersededSources clears, and only once a cycle has proven the
// replacement usable and complete.
//
// A slot is reused only once it is already clear. A successful, complete cycle
// reclaims superseded slots. If every slot is still occupied, those sources
// have not supplied enough proof to authorize reclamation, so collection fails
// closed and the operator can restore a previously used credential without
// losing its checkpoint.
func (c *Config) selectNamespace(rt hosted.Runtime, fingerprint string) (bool, error) {
	record, err := loadSourceRecord(rt)
	if err != nil {
		return false, err
	}
	if len(record.Slots) != maxSourceSlots {
		grown := make([]sourceSlot, maxSourceSlots)
		copy(grown, record.Slots)
		record.Slots = grown
	}
	dirty := false
	slot := -1
	for i := range record.Slots {
		if record.Slots[i].Fingerprint == fingerprint {
			slot = i
			break
		}
	}
	if slot < 0 {
		for i := range record.Slots {
			if record.Slots[i].Fingerprint == "" {
				record.Slots[i] = sourceSlot{Fingerprint: fingerprint}
				slot, dirty = i, true
				break
			}
		}
	}
	if slot < 0 {
		return false, errors.New("Compliance has no free state namespace; restore a previously used credential or complete a successful collection before rotating again")
	}
	if record.Current != fingerprint {
		record.Current, dirty = fingerprint, true
	}
	prefix := slotPrefix(slot)
	roots, err := c.rootPrefixes(prefix)
	if err != nil {
		return false, err
	}
	tracked, err := record.trackRoots(rt, slot, roots)
	if err != nil {
		return false, err
	}
	if dirty || tracked {
		if err = putSourceRecord(rt, record); err != nil {
			return false, err
		}
	}
	changed := c.sourcePrefix != "" && c.sourcePrefix != prefix
	c.sourcePrefix, c.sourceID = prefix, fingerprint
	return changed, nil
}

// rootPrefixes lists the checkpoint namespace every configured dataset of this
// stanza owns inside namespace. Recording them is what lets a superseded slot
// be reclaimed without the storage enumeration hosted.Storage does not have.
func (c *Config) rootPrefixes(namespace string) ([]string, error) {
	out := make([]string, 0, len(c.Dataset))
	for _, name := range c.Dataset {
		bound, err := c.bind(name)
		if err != nil {
			return nil, err
		}
		bound.sourcePrefix = namespace
		out = append(out, bound.key())
	}
	return out, nil
}

// resolveNamespace binds this configuration to its state namespace without
// collecting anything. Handle does this at the start of every cycle; it is
// available separately so a caller that needs the stanza's state keys before
// the first poll can obtain them.
func (c *Config) resolveNamespace(rt hosted.Runtime) error {
	credential, err := c.credential()
	if err != nil {
		return err
	}
	_, err = c.selectNamespace(rt, sourceFingerprint(c.Host, credential))
	return err
}

// cycleConfig binds the namespace and request credential to one read of the
// configured source. Credential files may be replaced between cycles, but a
// single cycle must never select its state namespace with one credential and
// issue requests with another.
func (c *Config) cycleConfig(rt hosted.Runtime) (*Config, bool, error) {
	credential, err := c.credential()
	if err != nil {
		return nil, false, err
	}
	frozen := *c
	frozen.Credential = credential
	frozen.Credential_File = ""
	changed, err := frozen.selectNamespace(rt, sourceFingerprint(c.Host, credential))
	if err != nil {
		return nil, false, err
	}
	// Keep the stanza's own view of where its state lives current, so a caller
	// holding the unfrozen configuration names the keys this cycle wrote.
	c.sourcePrefix, c.sourceID = frozen.sourcePrefix, frozen.sourceID
	return &frozen, changed, nil
}

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
	return c.verify(false)
}

// verify validates an operator stanza or one internal dataset-bound copy.
// Discovery capacity belongs to the operator stanza as a whole and is checked
// before fan-out. Bound copies retain the already-validated settings while
// still applying endpoint-specific clamps and all other safety checks.
func (c *Config) verify(bound bool) error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUID)
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	// Zero is a valid initial lower bound: it means collection begins at the
	// first poll instead of backfilling. Only pacing fields receive defaults.
	c.PollingConfig.ApplyDefaults(0, 60, 300)
	if c.Lookback < 0 || int64(c.Lookback) > maxLookbackHours {
		return errors.New("Lookback must be zero or a positive, representable number of hours")
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
		c.Max_Pending = maxPendingWorkItems
	}
	if c.Max_Children < 1 || c.Max_Children > 1000 || c.Max_Pending < c.Max_Children || c.Max_Pending > maxPendingWorkItems {
		return errors.New("invalid child-work bounds")
	}
	// The credential is read here so a missing, unsafe or unreadable key fails
	// configuration validation rather than the first collection cycle. The
	// state namespace it selects is resolved against durable state instead,
	// which validation has no runtime for (see selectNamespace).
	if _, err := c.credential(); err != nil {
		return err
	}

	params := map[string]string{}
	for _, s := range c.Parameter {
		k, v, ok := strings.Cut(s, ":")
		if !ok || k == "" || v == "" || invalidOpaqueID(v) {
			return errors.New("Parameter must contain name:opaque-id")
		}
		// A configured Parameter is bound into exactly the same child request
		// paths and persisted work items as a discovered identity, so it gets
		// the same ceiling. Accepting a longer one here and discovering it
		// later during work processing would treat operator configuration as
		// if it were vendor evidence of a removed resource.
		if len(v) > maxDiscoveredParameterLen {
			return fmt.Errorf("Parameter %s value exceeds %d bytes", k, maxDiscoveredParameterLen)
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
	if !bound && c.Follow_Children == "enabled" {
		floor := discoveryPendingFloor(c.Dataset)
		if c.Max_Pending < floor {
			return fmt.Errorf("Max-Pending must be at least %d to hold one discovery page atomically", floor)
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
	if err := b.verify(true); err != nil {
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

// key names this dataset's checkpoint inside a one-way namespace derived
// from the current origin and credential. The runtime already hands every
// ingester its own bucket keyed by kind/name/ingester-UUID; the source prefix
// additionally prevents a reused stanza from inheriting another tenant's
// lower bound, manifest, cursor, or child worklist.
//
// The resolved path alone does not identify a dataset. Two different dataset
// templates can resolve to the very same path once operator or discovered
// parameter values are substituted -- a project-document with document id
// "attachments" and a project-attachments listing of project "documents" both
// resolve to /apps/projects/documents/attachments -- and sharing a namespace
// would mean sharing a lower bound, cursor, manifest, worklist and tombstone.
// The catalog name disambiguates them unambiguously: it is bounded, drawn
// only from the catalog, never contains the separator, and the path that
// follows it always begins with one.
//
// The dataset and path together still do not identify a logical *owner*. One
// stanza can select a dataset explicitly and also reach that same dataset,
// resolved path and parameter set as a discovered child: selecting
// "organizations" and "organization-roles" with Parameter="organization_id:O"
// and Follow-Children="enabled" does exactly that as soon as organization O is
// listed. Those are two independently scheduled walks -- the discovered one
// owns a worklist entry, a retry schedule and a retirement; the configured one
// owns none of them and instead owns the stanza's child worklist, cleanup
// backlog and tombstone registry -- so the role is part of the namespace. One
// shared namespace would let each walk overwrite the other's committed
// manifest and re-emit records, and would let the discovered child's
// retirement clear the configured selector's checkpoint and, for a child that
// is itself a parent, its durable descendant cleanup obligations.
//
// The role alone still does not say *which* owner a discovered key belongs to.
// A stanza keeps one child worklist per configured root, and two different
// roots can reach one resolved descendant: with "organizations" and
// "organization-roles" (Parameter="organization_id:O") both selected, the
// organizations worklist reaches role R's permissions through organization O's
// roles, while the organization-roles worklist reaches the very same
// role-permissions directly. Each worklist independently schedules, retries,
// retires, capacity-evicts and reclaims its own entry, so one shared namespace
// would let either reclaim a descendant the other still owns -- resetting a
// manifest and cursor that are still in use, and replaying records or
// restarting a traversal under the surviving owner. A discovered namespace is
// therefore allocated *inside* its owning root's namespace, from that root's
// own worklist (see childStateKey). Within one root the same descendant stays
// one slot, and so one logical owner, however many paths reach it, and no
// root's slot can be named by any other root.
func (c *Config) key() string {
	if c.stateKey != "" {
		return c.stateKey
	}
	return c.sourcePrefix + keyRoleConfigured + c.dataset.Name + c.path + "#"
}

const (
	// keyRoleConfigured names a configured selector's checkpoint namespace
	// inside the stanza's state namespace, and keyRoleDiscovered names a
	// discovered child's numbered slot inside its owning root's namespace.
	// keyRoleDiscovered deliberately ends in the separator that the decimal
	// slot following it never contains, and it is not a prefix of keyChildren,
	// keyCleanup or keyTombstones, so a child slot can never collide with the
	// bookkeeping its own root keeps.
	keyRoleConfigured = "/dataset/"
	keyRoleDiscovered = "/child/"
)

// childStateKey names the checkpoint namespace a discovered child owns inside
// its configured root's namespace.
//
// The slot, rather than the child's resolved path, is what keeps this stanza's
// physical key space finite. hosted.Storage offers neither deletion nor
// enumeration, so a namespace named after an opaque vendor identifier would
// leave one dead key set behind for every child that is ever retired, without
// bound, however small the live worklist stays. A slot is allocated from the
// owning root's durable worklist, is bounded by Max-Pending, and is handed back
// for reuse only after the obligation to clear it has completed -- so the
// number of distinct keys a root can ever write is Max-Pending namespaces plus
// its own, whatever the vendor's child churn is.
//
// Two different dataset templates can resolve to the same path once parameter
// values are substituted, and two roots can reach the same descendant. Neither
// can alias here: a slot is unique within the one worklist that allocates it,
// and that worklist belongs to exactly one configured root.
func childStateKey(root string, slot int) string {
	return root + keyRoleDiscovered + strconv.Itoa(slot) + "#"
}

// credential returns the API key, preferring an inline Credential over
// Credential-File. The path is opened without following symlinks and in
// non-blocking mode, then the descriptor is checked and read with a hard
// ceiling. A FIFO therefore cannot stall collection or runner shutdown, a path
// replacement cannot swap in a different file between validation and read,
// and a short read cannot silently truncate a key.
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
	f, err := os.OpenFile(c.Credential_File, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", errors.New("cannot read Compliance credential file")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 || st.Size() > maxCredentialBytes {
		return "", errors.New("invalid Compliance credential file")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || (owner.Uid != uint32(os.Geteuid()) && owner.Uid != 0) {
		return "", errors.New("invalid Compliance credential file")
	}
	v, err := readCredential(f, st.Size())
	if err != nil {
		return "", err
	}
	return v, nil
}

// readCredential consumes exactly the descriptor size validated by the caller.
// Reading fewer bytes means the file changed after validation; accepting that
// prefix would silently substitute a different credential. Reading more bytes
// is rejected by the same hard ceiling that bounds ordinary credential files.
func readCredential(r io.Reader, expected int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxCredentialBytes+1))
	if err != nil {
		return "", errors.New("cannot read Compliance credential file")
	}
	if len(b) > maxCredentialBytes || int64(len(b)) != expected {
		return "", errors.New("invalid Compliance credential file")
	}
	v := strings.TrimSpace(string(b))
	if v == "" || strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid Compliance credential")
	}
	return v, nil
}
