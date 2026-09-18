package restpoller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins/slack/internal/secretfile"
)

var queryNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]{1,128}$`)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig

	Dataset            []string
	Base_URL           string
	Credential_File    string `json:"-"`
	Start_Time         string
	Page_Size          int
	Max_Pages          int
	Query              []string
	SuiteQL_Query_File string
	Allow_Plaintext    bool
	Ingest_Unchanged   bool
	Emit_Errors        bool
	Error_Tag          string
	Scope_Identity     string

	definitions []Definition
	queries     map[string]url.Values
	product     string
}

// SetProduct binds this shared polling configuration to one vendor-owned
// plugin. Product identity is deliberately not configurable in the public
// Gravwell stanza.
func (c *Config) SetProduct(product string) {
	c.product = strings.ToLower(strings.TrimSpace(product))
}

func (c *Config) Product() string { return c.product }

func (c *Config) Verify() error {
	if c.product == "" {
		return errors.New("vendor plugin did not bind a product identity")
	}
	if c.product == "slack" && c.Scope_Identity == "" {
		return errors.New("Scope-Identity must name the organization and scope without a secret")
	}
	c.ApplyDefaults(24, 60, 300)
	if c.UUID() == uuid.Nil {
		return errors.New("Ingester-UUID must be a valid non-zero UUID")
	}
	definitions, err := ResolveDefinitions(c.product, c.Dataset)
	if err != nil {
		return err
	}
	if c.Tag_Name != "" && len(definitions) != 1 {
		return errors.New("Tag-Name requires exactly one Dataset")
	}
	if err := c.ValidateTags(); err != nil {
		return err
	}
	base, err := url.Parse(strings.TrimSpace(c.Base_URL))
	if err != nil || base.Host == "" {
		return errors.New("Base-URL must be an absolute URL")
	}
	if base.Scheme != "https" && !(c.Allow_Plaintext && base.Scheme == "http") {
		return errors.New("Base-URL must use HTTPS unless Allow-Plaintext=true is explicitly set for a private test endpoint")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("Base-URL must not contain userinfo, query parameters, or a fragment")
	}
	c.Base_URL = strings.TrimRight(base.String(), "/")
	if c.Page_Size == 0 {
		c.Page_Size = 100
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = 1000
	}
	if c.Page_Size < 1 || c.Page_Size > 9999 {
		return errors.New("Page-Size must be between 1 and 9999")
	}
	if c.Max_Pages < 1 || c.Max_Pages > 100000 {
		return errors.New("Max-Pages must be between 1 and 100000")
	}
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 10000 {
		return errors.New("Requests-Per-Minute must be between 1 and 10000")
	}
	if c.Request_Interval < 60 {
		return errors.New("Request-Interval must be at least 60 seconds")
	}
	if c.Lookback < 0 {
		return errors.New("Lookback must not be negative")
	}
	needsCredential := false
	for _, definition := range definitions {
		if definition.Auth != authNone {
			needsCredential = true
		}
		if definition.MaximumPageSize > 0 && c.Page_Size > definition.MaximumPageSize {
			return fmt.Errorf("Page-Size %d exceeds %s/%s maximum %d", c.Page_Size, c.product, definition.Name, definition.MaximumPageSize)
		}
		if definition.RequiredStart && strings.TrimSpace(c.Start_Time) == "" {
			return fmt.Errorf("Start-Time is required for %s/%s", c.product, definition.Name)
		}
	}
	if needsCredential {
		if _, err := secretfile.Read(c.Credential_File, "Credential-File"); err != nil {
			return err
		}
	}
	if c.Start_Time != "" {
		if _, err := time.Parse(time.RFC3339, c.Start_Time); err != nil {
			return errors.New("Start-Time must use RFC3339")
		}
	}
	queries, err := parseQueries(c.Query)
	if err != nil {
		return err
	}
	if c.product == "netsuite" {
		if err := validateSuiteQLFile(c.SuiteQL_Query_File); err != nil {
			return err
		}
	}
	if c.Error_Tag == "" {
		c.Error_Tag = c.product + "-collector-errors"
	}
	c.definitions = definitions
	c.queries = queries
	return nil
}

func parseQueries(values []string) (map[string]url.Values, error) {
	result := make(map[string]url.Values)
	for _, raw := range values {
		dataset, remainder, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok || strings.TrimSpace(dataset) == "" {
			return nil, fmt.Errorf("Query %q must use dataset:name=value", raw)
		}
		name, value, ok := strings.Cut(remainder, "=")
		if !ok || !queryNamePattern.MatchString(name) {
			return nil, fmt.Errorf("Query %q has an invalid parameter name", raw)
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "key") {
			return nil, fmt.Errorf("Query %q may contain credential material; use Credential-File and header authentication", raw)
		}
		if result[dataset] == nil {
			result[dataset] = make(url.Values)
		}
		if _, duplicate := result[dataset][name]; duplicate {
			return nil, fmt.Errorf("duplicate Query for %s:%s", dataset, name)
		}
		result[dataset].Set(name, value)
	}
	return result, nil
}

func validateSuiteQLFile(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("SuiteQL-Query-File: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("SuiteQL-Query-File must be a regular file no larger than 1 MiB")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	query := strings.TrimSpace(string(body))
	if !strings.HasPrefix(strings.ToUpper(query), "SELECT ") {
		return errors.New("SuiteQL query must be read-only and begin with SELECT")
	}
	if !strings.Contains(strings.ToUpper(query), " ORDER BY ") {
		return errors.New("SuiteQL query must contain a stable ORDER BY for offset pagination")
	}
	return nil
}

func (c *Config) Credential() (string, error) {
	if strings.TrimSpace(c.Credential_File) == "" {
		return "", nil
	}
	return secretfile.Read(c.Credential_File, "Credential-File")
}

func (c *Config) SuiteQLBody() (string, error) {
	if c.product != "netsuite" {
		return "", nil
	}
	body, err := os.ReadFile(c.SuiteQL_Query_File)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (c *Config) Definitions() []Definition {
	return append([]Definition(nil), c.definitions...)
}

func (c *Config) Queries(name string) url.Values {
	result := make(url.Values)
	for key, values := range c.queries[name] {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func (c *Config) Tag(definition Definition) string {
	return c.ResolveTag(definition.TagKind, c.product)
}

func (c *Config) Tags() []string {
	tags := make([]string, 0, len(c.definitions)+1)
	for _, definition := range c.definitions {
		tags = append(tags, c.Tag(definition))
	}
	if c.Emit_Errors {
		tags = append(tags, c.Error_Tag)
	}
	return tags
}

func (c *Config) stateKey(definition Definition) string {
	if c.product == "slack" {
		return fmt.Sprintf("slack-audit-v2/%x", sha256.Sum256([]byte(c.Base_URL+"\x00"+c.Scope_Identity+"\x00"+strings.Join(c.Query, "\x00")+"\x00"+c.Start_Time+"\x00"+c.Tag(definition))))
	}
	return fmt.Sprintf("rest-poller/%s/%s/state-v2/%x", c.product, definition.Name, sha256.Sum256([]byte(c.Base_URL+"\x00"+c.Scope_Identity+"\x00"+strings.Join(c.Query, "\x00")+"\x00"+c.Start_Time+"\x00"+c.Tag(definition)+"\x00"+c.SuiteQL_Query_File)))
}

func (c *Config) EffectiveStart(definition Definition) string {
	if c.Start_Time != "" {
		return c.Start_Time
	}
	if definition.StartQuery == "" {
		return ""
	}
	return time.Now().UTC().Add(-time.Duration(c.Lookback) * time.Hour).Format(time.RFC3339)
}

func parsePositive(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("value must be a positive integer")
	}
	return value, nil
}
