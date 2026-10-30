package microsoft

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
)

const (
	defaultGraphHost         = "https://graph.microsoft.com"
	defaultARMHost           = "https://management.azure.com"
	defaultDefenderHost      = "https://api.security.microsoft.com"
	defaultPowerBIHost       = "https://api.powerbi.com"
	defaultAuthHost          = "https://login.microsoftonline.com"
	defaultLookbackHours     = 24
	defaultRequestsPerMinute = 10
	defaultIntervalSeconds   = 3600
	defaultPageSize          = 10
	defaultMaxPages          = 1
	defaultOverlapSeconds    = 900
	defaultMaxRetries        = 3
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig
	// Shadow the upstream integer field with a decoder-compatible hour type.
	// This keeps the portable plugin usable with the official config loader.
	Lookback LookbackHours

	Tenant_ID          string
	Client_ID          string
	Client_Secret_File string
	Subscription_ID    []string
	Api                []string
	Graph_Host         string
	ARM_Host           string
	Defender_Host      string
	PowerBI_Host       string
	Auth_Host          string
	Page_Size          int
	Max_Pages          int
	Overlap            int
	Max_Retries        int
	Ingest_Unchanged   bool
	Normalization      string
	Tag_Schema         string
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
		c.Lookback == nc.Lookback &&
		c.Tenant_ID == nc.Tenant_ID &&
		c.Client_ID == nc.Client_ID &&
		c.Client_Secret_File == nc.Client_Secret_File &&
		slices.Equal(c.Subscription_ID, nc.Subscription_ID) &&
		slices.Equal(c.Api, nc.Api) &&
		c.Graph_Host == nc.Graph_Host &&
		c.ARM_Host == nc.ARM_Host &&
		c.Defender_Host == nc.Defender_Host &&
		c.PowerBI_Host == nc.PowerBI_Host &&
		c.Auth_Host == nc.Auth_Host &&
		c.Page_Size == nc.Page_Size &&
		c.Max_Pages == nc.Max_Pages &&
		c.Overlap == nc.Overlap &&
		c.Max_Retries == nc.Max_Retries &&
		c.Ingest_Unchanged == nc.Ingest_Unchanged &&
		c.Normalization == nc.Normalization &&
		c.Tag_Schema == nc.Tag_Schema
}

func (c *Config) Verify() error {
	c.PollingConfig.ApplyDefaults(defaultLookbackHours, defaultRequestsPerMinute, defaultIntervalSeconds)
	if c.Lookback == 0 {
		c.Lookback = LookbackHours(c.PollingConfig.Lookback)
	}
	c.PollingConfig.Lookback = int(c.Lookback)
	if c.Graph_Host == "" {
		c.Graph_Host = defaultGraphHost
	}
	if c.ARM_Host == "" {
		c.ARM_Host = defaultARMHost
	}
	if c.Defender_Host == "" {
		c.Defender_Host = defaultDefenderHost
	}
	if c.PowerBI_Host == "" {
		c.PowerBI_Host = defaultPowerBIHost
	}
	if c.Auth_Host == "" {
		c.Auth_Host = defaultAuthHost
	}
	if c.Page_Size == 0 {
		c.Page_Size = defaultPageSize
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = defaultMaxPages
	}
	if c.Overlap == 0 {
		c.Overlap = defaultOverlapSeconds
	}
	if c.Max_Retries == 0 {
		c.Max_Retries = defaultMaxRetries
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
	if strings.TrimSpace(c.Client_Secret_File) == "" {
		return errors.New("Client-Secret-File must be specified")
	}
	if _, err := readSecret(c.Client_Secret_File); err != nil {
		return fmt.Errorf("Client-Secret-File: %w", err)
	}
	if len(c.Api) == 0 {
		return errors.New("at least one Api must be specified")
	}
	resolved, err := ResolveDatasets(c.Api)
	if err != nil {
		return err
	}
	if c.Tag_Schema, err = validateTagSchema(c.Tag_Schema); err != nil {
		return err
	}
	if c.Tag_Name != "" {
		if c.Tag_Schema == TagSchemaLegacy && len(resolved) != 1 {
			return errors.New("Tag-Name requires exactly one resolved API with legacy Tag-Schema")
		}
		if c.Tag_Schema == TagSchemaConsolidated && len(destinationTags(resolved, c.Tag_Schema)) != 1 {
			return errors.New("Tag-Name requires APIs that resolve to exactly one consolidated tag")
		}
	}
	if err := c.ValidateTags(); err != nil {
		return err
	}
	for name, raw := range map[string]string{"Graph-Host": c.Graph_Host, "ARM-Host": c.ARM_Host, "Defender-Host": c.Defender_Host, "PowerBI-Host": c.PowerBI_Host, "Auth-Host": c.Auth_Host} {
		if err := validateHTTPSBase(raw); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	needsSubscription := false
	for _, dataset := range resolved {
		if strings.Contains(dataset.Path, "{subscriptionId}") || dataset.Kind == KindResourceGraph {
			needsSubscription = true
		}
	}
	if needsSubscription && len(c.Subscription_ID) == 0 {
		return errors.New("at least one Subscription-ID is required by the selected Azure APIs")
	}
	for _, subscriptionID := range c.Subscription_ID {
		parsed, err := uuid.Parse(subscriptionID)
		if err != nil || parsed == uuid.Nil {
			return fmt.Errorf("invalid Subscription-ID %q", subscriptionID)
		}
	}
	if c.Page_Size < 1 || c.Page_Size > 1000 {
		return errors.New("Page-Size must be between 1 and 1000")
	}
	if c.Max_Pages < 1 || c.Max_Pages > 1000 {
		return errors.New("Max-Pages must be between 1 and 1000")
	}
	if c.Overlap < 0 || c.Overlap > 86400 {
		return errors.New("Overlap must be between 0 and 86400 seconds")
	}
	if c.Max_Retries < 0 || c.Max_Retries > 10 {
		return errors.New("Max-Retries must be between 0 and 10")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 6000 {
		return errors.New("Requests-Per-Minute must be between 1 and 6000")
	}
	if c.Request_Interval < 60 || uint64(c.Request_Interval) > uint64((time.Duration(1<<63-1))/time.Second) {
		return errors.New("Request-Interval must be between 60 and 9223372036 seconds")
	}
	if c.Lookback < 1 || c.Lookback > 24*28 {
		return errors.New("Lookback must be between 1 and 672 hours")
	}
	if _, err := c.NormalizationEnabled(); err != nil {
		return err
	}
	if c.UUID() == uuid.Nil {
		return errors.New("Ingester-UUID must be a valid non-zero UUID")
	}
	return nil
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

func (c *Config) Tags() []string {
	resolved, _ := ResolveDatasets(c.Api)
	schema, _ := validateTagSchema(c.Tag_Schema)
	groups := TagsForSelectors(c.Api)
	if schema == TagSchemaConsolidated {
		groups = destinationTags(resolved, schema)
	}
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

func (c *Config) Tag(group string) string {
	tag := group
	if c.Tag_Name != "" {
		tag = c.Tag_Name
	} else if c.Tag_Prefix != "" {
		tag = c.Tag_Prefix + "-" + group
	}
	enabled, _ := c.NormalizationEnabled()
	if enabled && !strings.HasSuffix(tag, "-normalized") {
		tag += "-normalized"
	}
	return tag
}

// NormalizationEnabled parses the source-stanza normalization contract. The
// zero value deliberately preserves the compact vendor-native selected record.
func (c *Config) NormalizationEnabled() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(c.Normalization)) {
	case "", "disabled", "false":
		return false, nil
	case "enabled", "true":
		return true, nil
	default:
		return false, errors.New("Normalization must be enabled, disabled, true, or false")
	}
}

func (c *Config) StateNamespace() string {
	enabled, _ := c.NormalizationEnabled()
	if enabled {
		return "microsoft-normalized-v1"
	}
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
	case ServicePowerBI:
		return strings.TrimRight(c.PowerBI_Host, "/")
	default:
		return ""
	}
}

func readSecret(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(b) > 1<<20 {
		return "", errors.New("secret file exceeds the 1 MiB safety limit")
	}
	value := strings.TrimSpace(string(b))
	if value == "" {
		return "", errors.New("file is empty")
	}
	return value, nil
}
