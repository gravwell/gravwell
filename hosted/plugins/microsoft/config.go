/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package microsoft

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	// defaultIngesterUUIDStr is the stable identity for the initial public
	// Microsoft Hosted Runner configuration. Explicit per-stanza UUIDs remain
	// supported when operators intentionally run independent jobs.
	defaultIngesterUUIDStr = "2b823d47-3626-4451-ba18-92916bb401fc"
	defaultGraphHost       = "https://graph.microsoft.com"
	defaultARMHost         = "https://management.azure.com"
	// Defender for Endpoint / Defender Vulnerability Management and the unified
	// Microsoft Defender XDR incidents API are two distinct Entra ID resources.
	// A bearer token carries the audience of the resource it was minted for, so
	// each resource needs its own origin and its own Service: a securitycenter
	// token presented to api.security.microsoft.com is rejected, and the reverse
	// is equally true. ServiceDefender and ServiceDefenderXDR keep the pairing.
	defaultDefenderHost      = "https://api.securitycenter.microsoft.com"
	defaultDefenderXDRHost   = "https://api.security.microsoft.com"
	defaultPowerBIHost       = "https://api.powerbi.com"
	defaultManagementHost    = "https://manage.office.com"
	defaultAuthHost          = "https://login.microsoftonline.com"
	defaultLookback          = 24
	defaultRequestsPerMinute = 10
	defaultIntervalSeconds   = 3600
	defaultPageSize          = 100
	defaultMaxPages          = 100
	// sourceConsistencyDelay holds timestamped event windows behind wall clock
	// so Microsoft services can finalize records before the half-open window is
	// durably checkpointed.
	sourceConsistencyDelay = 5 * time.Minute
	// sourceReplayOverlap re-reads the trailing event-time range from the prior
	// completed window. A bounded receipt set suppresses records already written
	// from that range while allowing later-visible records through.
	sourceReplayOverlap = 5 * time.Minute
	// maximumOverlapReceipts is a hard per-selector bound across the retained
	// overlap receipts. Capacity is checked before any record in a fetched chunk
	// is written; receipts never extend outside the sliding overlap horizon.
	maximumOverlapReceipts = 200000
	// temporalFingerprintLength is the width of a canonical overlap receipt
	// fingerprint: temporalReceiptFingerprint is the lowercase hex encoding of
	// a sha256 digest, so every fingerprint this plugin records is exactly
	// 64 characters drawn from [0-9a-f]. A stored receipt has to carry that
	// exact shape. Accepting any nonempty string instead meant a stored
	// document could hand the replay-suppression map arbitrarily long keys --
	// the count bound says how many receipts a document may hold and nothing
	// at all about how large one is -- and then have them re-marshaled into the
	// document the next completion persists.
	temporalFingerprintLength = 64
	// maximumOverlapReceiptBytes bounds the aggregate stored cost of the
	// receipts one overlap document retains, charged before any of them is
	// retained. The canonical set is far below it: a canonical receipt is a
	// 64-character fingerprint plus an RFC3339 timestamp, about 128 bytes with
	// the separator the document needs to hold it, so the full
	// maximumOverlapReceipts count bound is roughly 25 MB. 28 MiB admits that
	// whole count bound at the shape the plugin writes, which is what keeps the
	// bound non-destructive: a set the completion path may legitimately produce
	// is never refused by it.
	//
	// What it does bound is the amplification the canonical shape does not
	// describe. A stored receipt is charged the bytes it actually occupies in
	// the stored document, so interior whitespace, a timestamp written with far
	// more fractional digits than a nanosecond needs, and \u escapes are all
	// charged for what they cost to read and to keep.
	maximumOverlapReceiptBytes = 28 << 20
	// maximumOverlapDocumentBytes is the hard ceiling on the stored overlap
	// document, measured before it is decoded and again before it is persisted.
	// It is the backstop that makes the bounds above a statement about the
	// document rather than about its parts: a JSON member this build does not
	// know is discarded by the decoder and charged against nothing else at all,
	// so 200 bytes of receipts could arrive inside an arbitrarily large
	// document and be adopted clean.
	//
	// 32 MiB leaves the aggregate receipt budget better than 15% of headroom
	// for the document frame and for members a later build may add, and stays
	// well below the 16 MiB-per-response ceiling's nearest neighbour in this
	// plugin, maximumCursorDocumentBytes at 12 MiB, only because the overlap
	// receipt set is the one document whose size the count bound alone drives
	// into the tens of megabytes.
	//
	// Crossing it when loading routes the document into the existing
	// reset/recovery path, which replaces the overlap key and restarts the
	// window from the durable watermark without dispatching a request; the
	// receipts it drops can only replay records the horizon still covers, which
	// delivery is idempotent against by design. Crossing it when persisting
	// fails the poll before the first Put, so an unreadable overlap document is
	// never the durable one.
	maximumOverlapDocumentBytes = 32 << 20
	// maximumFabricStateBytes is the hard ceiling on the small Fabric scan
	// metadata document. The current document is three timestamps and remains
	// far below one KiB even at their longest valid encodings; the headroom is
	// reserved for compatible fields added by later builds, not for unbounded
	// ignored members supplied by the state store.
	//
	// Loading measures the stored bytes before either JSON decoder sees them and
	// routes an oversized document through the existing continuation reset path.
	// Persisting measures the encoded document before the first checkpoint Put,
	// so refusal leaves the cursor, overlap, Fabric metadata and watermark as one
	// intact prior checkpoint.
	maximumFabricStateBytes = 1 << 10
	// defaultFetchDeadline bounds all authentication, retries, and pages in one
	// selector/subscription chunk. Later scheduled polls retry from saved state.
	defaultFetchDeadline      = 2 * time.Minute
	defaultHTTPAttemptTimeout = 30 * time.Second
	defaultRetryBackoff       = 10 * time.Second
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig

	Tenant_ID            string
	Client_ID            string
	Client_Secret        string `json:"-"`
	Publisher_Identifier string
	Subscription_ID      []string
	Api                  []string
	Graph_Host           string
	Graph_Scope          string
	ARM_Host             string
	ARM_Scope            string
	Defender_Host        string
	Defender_Scope       string
	Defender_XDR_Host    string
	Defender_XDR_Scope   string
	PowerBI_Host         string
	PowerBI_Scope        string
	Management_Host      string
	Management_Scope     string
	Auth_Host            string
	Tag_Schema           string
}

var _ hosted.Config = (*Config)(nil)

// Equal implements hosted.Config so the runner can avoid restarting an
// unchanged Microsoft job when its configuration is reloaded.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Tenant_ID == nc.Tenant_ID &&
		c.Client_ID == nc.Client_ID &&
		c.Client_Secret == nc.Client_Secret &&
		c.Publisher_Identifier == nc.Publisher_Identifier &&
		slices.Equal(c.Subscription_ID, nc.Subscription_ID) &&
		slices.Equal(c.Api, nc.Api) &&
		c.Graph_Host == nc.Graph_Host &&
		c.Graph_Scope == nc.Graph_Scope &&
		c.ARM_Host == nc.ARM_Host &&
		c.ARM_Scope == nc.ARM_Scope &&
		c.Defender_Host == nc.Defender_Host &&
		c.Defender_Scope == nc.Defender_Scope &&
		c.Defender_XDR_Host == nc.Defender_XDR_Host &&
		c.Defender_XDR_Scope == nc.Defender_XDR_Scope &&
		c.PowerBI_Host == nc.PowerBI_Host &&
		c.PowerBI_Scope == nc.PowerBI_Scope &&
		c.Management_Host == nc.Management_Host &&
		c.Management_Scope == nc.Management_Scope &&
		c.Auth_Host == nc.Auth_Host &&
		c.Tag_Schema == nc.Tag_Schema
}

func (c *Config) Verify() error {
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)
	c.PollingConfig.ApplyDefaults(defaultLookback, defaultRequestsPerMinute, defaultIntervalSeconds)
	if c.Graph_Host == "" {
		c.Graph_Host = defaultGraphHost
	}
	if c.ARM_Host == "" {
		c.ARM_Host = defaultARMHost
	}
	if c.Defender_Host == "" {
		c.Defender_Host = defaultDefenderHost
	}
	if c.Defender_XDR_Host == "" {
		c.Defender_XDR_Host = defaultDefenderXDRHost
	}
	if c.PowerBI_Host == "" {
		c.PowerBI_Host = defaultPowerBIHost
	}
	if c.Management_Host == "" {
		c.Management_Host = defaultManagementHost
	}
	if c.Management_Scope == "" {
		c.Management_Scope = strings.TrimRight(c.Management_Host, "/") + "/.default"
	}
	if c.Auth_Host == "" {
		c.Auth_Host = defaultAuthHost
	}
	if strings.TrimSpace(c.Tenant_ID) == "" {
		return errors.New("Tenant-ID must be specified")
	}
	if strings.TrimSpace(c.Tenant_ID) == uuid.Nil.String() {
		return errors.New("Tenant-ID must not be the all-zero example identifier")
	}
	if strings.TrimSpace(c.Client_ID) == "" {
		return errors.New("Client-ID must be specified")
	}
	if strings.TrimSpace(c.Client_ID) == uuid.Nil.String() {
		return errors.New("Client-ID must not be the all-zero example identifier")
	}
	if strings.TrimSpace(c.Client_Secret) == "" {
		return errors.New("Client-Secret must be specified")
	}
	if len(c.Api) == 0 {
		return errors.New("at least one Api must be specified")
	}
	schema, err := validateTagSchema(c.Tag_Schema)
	if err != nil {
		return err
	}
	resolved, err := resolveDatasetsForSchema(c.Api, schema)
	if err != nil {
		return err
	}
	c.Tag_Schema = schema
	if c.Tag_Name != "" {
		if len(destinationTags(resolved, c.Tag_Schema)) != 1 {
			return errors.New("Tag-Name requires APIs that resolve to exactly one consolidated tag")
		}
	}
	if strings.TrimSpace(c.Publisher_Identifier) != "" {
		publisherID, err := uuid.Parse(c.Publisher_Identifier)
		if err != nil || publisherID == uuid.Nil {
			return errors.New("Publisher-Identifier must be a valid non-zero UUID")
		}
		c.Publisher_Identifier = publisherID.String()
	}
	if hasManagementDataset(resolved) && c.Publisher_Identifier == "" {
		return errors.New("Publisher-Identifier is required by Management Activity APIs")
	}
	if err := c.ValidateTags(); err != nil {
		return err
	}
	for name, raw := range map[string]string{"Graph-Host": c.Graph_Host, "ARM-Host": c.ARM_Host, "Defender-Host": c.Defender_Host, "Defender-XDR-Host": c.Defender_XDR_Host, "PowerBI-Host": c.PowerBI_Host, "Management-Host": c.Management_Host, "Auth-Host": c.Auth_Host} {
		if err := validateHTTPSBase(raw); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if name == "Management-Host" {
			u, _ := url.Parse(raw)
			if u.EscapedPath() != "" && u.EscapedPath() != "/" {
				return errors.New("Management-Host must not contain a path")
			}
		}
	}
	if err := validateManagementScope(c.Management_Scope); err != nil {
		return err
	}
	for name, raw := range map[string]string{
		"Graph-Scope": c.Graph_Scope, "ARM-Scope": c.ARM_Scope,
		"Defender-Scope": c.Defender_Scope, "Defender-XDR-Scope": c.Defender_XDR_Scope,
		"PowerBI-Scope": c.PowerBI_Scope,
	} {
		if raw != "" {
			if err := validateOAuthScope(name, raw); err != nil {
				return err
			}
		}
	}
	needsSubscription := false
	for _, dataset := range resolved {
		if strings.Contains(dataset.Path, "{subscriptionId}") || dataset.Kind == KindResourceGraph {
			needsSubscription = true
		}
	}
	if hasManagementDataset(resolved) && c.PollingConfig.Lookback > 24*7 {
		return errors.New("Lookback must not exceed 168 hours for Management Activity APIs")
	}
	if needsSubscription && len(c.Subscription_ID) == 0 {
		return errors.New("at least one Subscription-ID is required by the selected Azure APIs")
	}
	seenSubscriptions := make(map[uuid.UUID]struct{}, len(c.Subscription_ID))
	for idx, subscriptionID := range c.Subscription_ID {
		parsed, err := uuid.Parse(subscriptionID)
		if err != nil || parsed == uuid.Nil {
			return fmt.Errorf("invalid Subscription-ID %q", subscriptionID)
		}
		if _, ok := seenSubscriptions[parsed]; ok {
			return fmt.Errorf("duplicate Subscription-ID %q", subscriptionID)
		}
		seenSubscriptions[parsed] = struct{}{}
		c.Subscription_ID[idx] = parsed.String()
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 6000 {
		return errors.New("Requests-Per-Minute must be between 1 and 6000")
	}
	if c.Request_Interval < 60 || uint64(c.Request_Interval) > uint64((time.Duration(1<<63-1))/time.Second) {
		return errors.New("Request-Interval must be between 60 and 9223372036 seconds")
	}
	if c.PollingConfig.Lookback < 1 || c.PollingConfig.Lookback > 24*28 {
		return errors.New("Lookback must be between 1 and 672 hours")
	}
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	if c.UUID() == uuid.Nil {
		return errors.New("Ingester-UUID must be a valid non-zero UUID")
	}
	return nil
}

func hasManagementDataset(resolved []Dataset) bool {
	for _, dataset := range resolved {
		if dataset.Kind == KindManagementActivity {
			return true
		}
	}
	return false
}

func validateHTTPSBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("must be an absolute HTTPS URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must not contain a query or fragment")
	}
	return nil
}

func validateManagementScope(raw string) error {
	return validateOAuthScope("Management-Scope", raw)
}

func validateOAuthScope(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.EscapedPath(), "/.default") {
		return fmt.Errorf("%s must be an absolute HTTPS resource ending in /.default", name)
	}
	return nil
}

func (c *Config) Tags() []string {
	schema, _ := validateTagSchema(c.Tag_Schema)
	resolved, _ := resolveDatasetsForSchema(c.Api, schema)
	groups := destinationTags(resolved, schema)
	result := make([]string, 0, len(groups))
	for _, group := range groups {
		result = append(result, c.Tag(group))
	}
	return result
}

func (c *Config) TagForDataset(dataset Dataset) string {
	schema, _ := validateTagSchema(c.Tag_Schema)
	return c.Tag(destinationTag(dataset, schema))
}

func (c *Config) TagForManagementEvent(dataset Dataset, _ string) string {
	schema, _ := validateTagSchema(c.Tag_Schema)
	return c.Tag(destinationTag(dataset, schema))
}

func (c *Config) Tag(group string) string {
	tag := group
	if c.Tag_Name != "" {
		tag = c.Tag_Name
	} else if c.Tag_Prefix != "" {
		tag = c.Tag_Prefix + "-" + group
	}
	return tag
}

func (c *Config) StateNamespace() string {
	return "microsoft"
}

func (c *Config) Base(service Service) string {
	switch service {
	case ServiceGraph:
		return strings.TrimRight(c.Graph_Host, "/")
	case ServiceARM:
		return strings.TrimRight(c.ARM_Host, "/")
	case ServiceDefender:
		return strings.TrimRight(c.Defender_Host, "/")
	case ServiceDefenderXDR:
		return strings.TrimRight(c.Defender_XDR_Host, "/")
	case ServicePowerBI:
		return strings.TrimRight(c.PowerBI_Host, "/")
	case ServiceManagement:
		return strings.TrimRight(c.Management_Host, "/")
	default:
		return ""
	}
}

func (c *Config) Scope(service Service, catalogScope string) string {
	var override string
	switch service {
	case ServiceGraph:
		override = c.Graph_Scope
	case ServiceARM:
		override = c.ARM_Scope
	case ServiceDefender:
		override = c.Defender_Scope
	case ServiceDefenderXDR:
		override = c.Defender_XDR_Scope
	case ServicePowerBI:
		override = c.PowerBI_Scope
	case ServiceManagement:
		override = c.Management_Scope
	}
	if override != "" {
		return override
	}
	return catalogScope
}
