package servicenow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
)

type Override struct{ Table, Fields, Query, Timestamp, Tag string }

// APIEndpoint is an explicitly documented read-only ServiceNow REST endpoint.
// It is used only when a product exposes a GET collection that is not available
// through the common Table API catalog.
type APIEndpoint struct {
	Product, Path, Tag, Result_Path, ID_Field, Timestamp            string
	Static_ID                                                       string
	Limit_Parameter, Offset_Parameter, Required_Role, Documentation string
	Parameters                                                      map[string]string
}

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig
	Instance       string
	Secret_File    string
	Product        []string
	API            []string
	API_Endpoint   []string
	Table          []string
	Table_Override []string
	// Selector and Selector-Override provide alternate exact-catalog selection.
	Selector                      []string
	Selector_Override             []string
	Page_Size, Max_Pages, Timeout int
	Overlap, Max_Retries          *int
	Skip_Unavailable              bool
	Normalization                 string
	Normalization_Field           []string
	normalizationRules            map[string][]normalizationRule
}

var _ hosted.Config = (*Config)(nil)

// Equal implements hosted.Config so unchanged configuration reloads do not
// interrupt an active ServiceNow poll.
func (c *Config) Equal(ncp any) bool {
	nc, ok := hosted.EqualTarget[Config](ncp)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Instance == nc.Instance &&
		c.Secret_File == nc.Secret_File &&
		slices.Equal(c.Product, nc.Product) &&
		slices.Equal(c.API, nc.API) &&
		slices.Equal(c.API_Endpoint, nc.API_Endpoint) &&
		slices.Equal(c.Table, nc.Table) &&
		slices.Equal(c.Table_Override, nc.Table_Override) &&
		slices.Equal(c.Selector, nc.Selector) &&
		slices.Equal(c.Selector_Override, nc.Selector_Override) &&
		c.Page_Size == nc.Page_Size &&
		c.Max_Pages == nc.Max_Pages &&
		c.Timeout == nc.Timeout &&
		c.OverlapSeconds() == nc.OverlapSeconds() &&
		c.MaxRetries() == nc.MaxRetries() &&
		c.Skip_Unavailable == nc.Skip_Unavailable &&
		c.Normalization == nc.Normalization &&
		slices.Equal(c.Normalization_Field, nc.Normalization_Field)
}

func (c *Config) Verify() error {
	c.PollingConfig.ApplyDefaults(24, 60, 300)
	if err := c.ValidateTags(); err != nil {
		return err
	}
	if c.Page_Size == 0 {
		c.Page_Size = 100
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = 100
	}
	if c.Overlap == nil {
		c.Overlap = intPointer(300)
	}
	if c.Timeout == 0 {
		c.Timeout = 30
	}
	if c.Max_Retries == nil {
		c.Max_Retries = intPointer(4)
	}
	if c.Normalization == "" {
		c.Normalization = "disabled"
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.Instance), "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Instance must be an absolute HTTPS URL without query or fragment")
	}
	c.Instance = strings.TrimRight(u.String(), "/")
	if info, err := os.Stat(c.Secret_File); err != nil || !info.Mode().IsRegular() {
		return errors.New("Secret-File must reference a readable regular file")
	}
	if c.UUID() == uuid.Nil {
		return errors.New("Ingester-UUID must be a valid non-zero UUID")
	}
	if c.Page_Size < 1 || c.Page_Size > 1000 {
		return errors.New("Page-Size must be 1..1000")
	}
	if c.Max_Pages < 1 || c.Max_Pages > 1000 {
		return errors.New("Max-Pages must be 1..1000")
	}
	if c.OverlapSeconds() < 0 || c.OverlapSeconds() > 86400 {
		return errors.New("Overlap must be 0..86400 seconds")
	}
	if c.Timeout < 1 || c.Timeout > 300 {
		return errors.New("Timeout must be 1..300 seconds")
	}
	if c.MaxRetries() < 0 || c.MaxRetries() > 10 {
		return errors.New("Max-Retries must be 0..10")
	}
	if c.Lookback < 1 || c.Lookback > 24*90 {
		return errors.New("Lookback must be 1..2160 hours")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 600 {
		return errors.New("Requests-Per-Minute must be 1..600")
	}
	if c.Request_Interval < 60 {
		return errors.New("Request-Interval must be at least 60 seconds")
	}
	if _, err := c.NormalizationEnabled(); err != nil {
		return err
	}
	if c.normalizationRules, err = compileNormalizationRules(c.Normalization_Field); err != nil {
		return err
	}
	datasets, err := c.Datasets()
	if err != nil {
		return err
	}
	if c.Tag_Name != "" && len(datasets) > 1 {
		canonicalTag := datasets[0].Tag
		for _, d := range datasets[1:] {
			if d.Tag != canonicalTag {
				return errors.New("Tag-Name requires resolved ServiceNow datasets from exactly one semantic product tag")
			}
		}
	}
	for _, d := range datasets {
		if err := ingest.CheckTag(c.Tag(d)); err != nil {
			return fmt.Errorf("ServiceNow dataset %s tag: %w", d.Name, err)
		}
	}
	return nil
}

func intPointer(value int) *int { return &value }

func (c *Config) OverlapSeconds() int {
	if c.Overlap == nil {
		return 300
	}
	return *c.Overlap
}

func (c *Config) MaxRetries() int {
	if c.Max_Retries == nil {
		return 4
	}
	return *c.Max_Retries
}

func (c *Config) Overrides() (map[string]Override, error) {
	result := map[string]Override{}
	for _, item := range []struct {
		name   string
		values []string
	}{
		{name: "Table-Override", values: c.Table_Override},
		{name: "Selector-Override", values: c.Selector_Override},
	} {
		for _, raw := range item.values {
			name, body, ok := strings.Cut(raw, "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if !ok || name == "" {
				return nil, fmt.Errorf("%s must be catalog-key={JSON object}", item.name)
			}
			if _, exists := catalog[name]; !exists {
				return nil, fmt.Errorf("%s names unknown catalog key %q", item.name, name)
			}
			if _, duplicate := result[name]; duplicate {
				return nil, fmt.Errorf("duplicate table override for catalog key %q", name)
			}
			var value Override
			if err := json.Unmarshal([]byte(body), &value); err != nil {
				return nil, fmt.Errorf("%s %s: %w", item.name, name, err)
			}
			result[name] = value
		}
	}
	return result, nil
}

func (c *Config) Datasets() ([]Dataset, error) {
	if len(c.Selector) > 0 && (len(c.Product) > 0 || len(c.API) > 0 || len(c.API_Endpoint) > 0 || len(c.Table) > 0) {
		return nil, errors.New("Selector cannot be combined with Product, API, API-Endpoint, or Table")
	}
	var datasets []Dataset
	if len(c.Selector) > 0 {
		var err error
		datasets, err = ResolveDatasets(c.Selector)
		if err != nil {
			return nil, err
		}
	} else {
		products, err := ResolveProducts(c.Product)
		if err != nil {
			return nil, err
		}
		apis, err := ResolveDatasets(c.API)
		if err != nil && len(c.API) > 0 {
			return nil, err
		}
		tables, err := ResolveTables(c.Table)
		if err != nil {
			return nil, err
		}
		datasets = append(products, apis...)
		datasets = append(datasets, tables...)
		endpoints, err := c.EndpointDatasets()
		if err != nil {
			return nil, err
		}
		datasets = append(datasets, endpoints...)
		if len(datasets) == 0 {
			return nil, errors.New("at least one Product, API, API-Endpoint, or Table is required")
		}
		seen := map[string]bool{}
		unique := datasets[:0]
		for _, d := range datasets {
			if !seen[d.Name] {
				unique = append(unique, d)
				seen[d.Name] = true
			}
		}
		datasets = unique
	}
	overrides, err := c.Overrides()
	if err != nil {
		return nil, err
	}
	for i := range datasets {
		o := overrides[datasets[i].Name]
		if o.Table != "" {
			datasets[i].Table = o.Table
		}
		if o.Fields != "" {
			datasets[i].Fields = o.Fields
		}
		if o.Query != "" {
			datasets[i].Query = o.Query
		}
		if o.Timestamp != "" {
			datasets[i].Timestamp = o.Timestamp
		}
		if o.Tag != "" {
			datasets[i].Tag = o.Tag
		}
		if datasets[i].RequiresTableOverride && (datasets[i].Table == "CUSTOM_TABLE_NAME" || datasets[i].Table == "") {
			return nil, fmt.Errorf("ServiceNow catalog key %s requires Table-Override", datasets[i].Name)
		}
	}
	return datasets, nil
}

// EndpointDatasets parses API-Endpoint="name={JSON}" values. The path is
// deliberately restricted to the current ServiceNow instance and GET collection
// semantics so this escape hatch cannot become an arbitrary URL fetcher.
func (c *Config) EndpointDatasets() ([]Dataset, error) {
	seen := map[string]bool{}
	result := make([]Dataset, 0, len(c.API_Endpoint))
	for _, raw := range c.API_Endpoint {
		name, body, custom := strings.Cut(raw, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if !validDatasetName(name) {
			return nil, errors.New("API-Endpoint must be a validated endpoint name, all, or lowercase-name={JSON object}")
		}
		if !custom && name == "all" {
			for _, builtInName := range endpointNames() {
				if seen[builtInName] {
					continue
				}
				result = append(result, endpointDataset(builtInName, endpointCatalog[builtInName]))
				seen[builtInName] = true
			}
			continue
		}
		if _, exists := catalog[name]; exists {
			return nil, fmt.Errorf("API-Endpoint name %q collides with the built-in catalog", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate API-Endpoint %q", name)
		}
		endpoint, builtIn := endpointCatalog[name]
		endpoint.Parameters = cloneStringMap(endpoint.Parameters)
		if custom {
			if err := json.Unmarshal([]byte(body), &endpoint); err != nil {
				return nil, fmt.Errorf("API-Endpoint %s: %w", name, err)
			}
		} else if !builtIn {
			return nil, fmt.Errorf("unknown validated API-Endpoint %q", name)
		}
		endpoint.Product = strings.TrimSpace(endpoint.Product)
		endpoint.Path = strings.TrimSpace(endpoint.Path)
		endpoint.Tag = strings.TrimSpace(endpoint.Tag)
		endpoint.Result_Path = strings.TrimSpace(endpoint.Result_Path)
		endpoint.ID_Field = strings.TrimSpace(endpoint.ID_Field)
		endpoint.Static_ID = strings.TrimSpace(endpoint.Static_ID)
		endpoint.Timestamp = strings.TrimSpace(endpoint.Timestamp)
		endpoint.Limit_Parameter = strings.TrimSpace(endpoint.Limit_Parameter)
		endpoint.Offset_Parameter = strings.TrimSpace(endpoint.Offset_Parameter)
		endpoint.Required_Role = strings.TrimSpace(endpoint.Required_Role)
		endpoint.Documentation = strings.TrimSpace(endpoint.Documentation)
		if endpoint.Product == "" || endpoint.Tag == "" || endpoint.Required_Role == "" || endpoint.Documentation == "" {
			return nil, fmt.Errorf("API-Endpoint %s requires Product, Tag, Required_Role, and Documentation", name)
		}
		u, err := url.Parse(endpoint.Path)
		if err != nil || !strings.HasPrefix(u.Path, "/api/") || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("API-Endpoint %s Path must be an instance-relative /api/... path without query or fragment", name)
		}
		doc, err := url.Parse(endpoint.Documentation)
		if err != nil || doc.Scheme != "https" || (doc.Host != "www.servicenow.com" && doc.Host != "servicenow.com") {
			return nil, fmt.Errorf("API-Endpoint %s Documentation must be an official HTTPS servicenow.com URL", name)
		}
		if endpoint.Result_Path == "" {
			endpoint.Result_Path = "result"
		}
		if endpoint.ID_Field == "" && endpoint.Static_ID == "" {
			endpoint.ID_Field = "sys_id"
		}
		if endpoint.Timestamp == "" {
			endpoint.Timestamp = "sys_updated_on"
		}
		for key := range endpoint.Parameters {
			if strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("API-Endpoint %s has an empty parameter name", name)
			}
		}
		result = append(result, endpointDataset(name, endpoint))
		seen[name] = true
	}
	return result, nil
}

func endpointDataset(name string, endpoint APIEndpoint) Dataset {
	return Dataset{
		Name: name, Product: endpoint.Product, Tag: endpoint.Tag,
		Timestamp: endpoint.Timestamp, RequiredRole: endpoint.Required_Role,
		Documentation: endpoint.Documentation,
		REST: &RESTSpec{Path: endpoint.Path, ResultPath: endpoint.Result_Path,
			IDField: endpoint.ID_Field, StaticID: endpoint.Static_ID,
			LimitParameter:  endpoint.Limit_Parameter,
			OffsetParameter: endpoint.Offset_Parameter, Parameters: cloneStringMap(endpoint.Parameters)},
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func validDatasetName(name string) bool {
	if name == "" || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func (c *Config) NormalizationEnabled() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(c.Normalization)) {
	case "enabled", "true":
		return true, nil
	case "disabled", "false":
		return false, nil
	default:
		return false, errors.New("Normalization must be enabled, disabled, true, or false")
	}
}
func (c *Config) Tag(d Dataset) string {
	// Catalog tags are the complete public tag contract. MultiTagConfig expects
	// only the suffix when a fallback prefix is supplied; passing the complete
	// tag would emit servicenow-servicenow-* at runtime. Normalization changes
	// the record representation, not source identity, so both modes resolve the
	// same canonical dataset tag.
	suffix := strings.TrimPrefix(d.Tag, "servicenow-")
	return c.ResolveTag(suffix, "servicenow")
}
func (c *Config) Tags() []string {
	datasets, _ := c.Datasets()
	out := make([]string, 0, len(datasets))
	for _, d := range datasets {
		out = append(out, c.Tag(d))
	}
	return out
}
func (c *Config) StateNamespace() string {
	if enabled, _ := c.NormalizationEnabled(); enabled {
		return normalizationNamespace(c.Normalization_Field)
	}
	return "servicenow/raw"
}

// FallbackStateNamespaces lists alternate keys that may contain the same
// cursor contract. Values are copied into the active namespace without
// deleting the source key.
func (c *Config) FallbackStateNamespaces() []string {
	if enabled, _ := c.NormalizationEnabled(); enabled {
		return []string{"servicenow-normalization-v2"}
	}
	return nil
}
func (c *Config) Interval() time.Duration { return time.Duration(c.Request_Interval) * time.Second }
