/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
)

const (
	Name    = "n8n"
	ID      = "n8n.ingesters.gravwell.io"
	Version = "1.0.0"

	defaultIngesterUUID    = "291e5806-07f4-4533-a979-3296263389bf"
	defaultRequestsPerMin  = 30
	defaultInterval        = 300
	defaultPageSize        = 100
	defaultMaxPages        = 1000
	maximumPageSize        = 250
	maximumMaxPages        = 100000
	maximumRequestInterval = 9_223_372_036 // largest whole-second delay representable by time.Duration
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig

	Dataset         []string
	Base_URL        string
	Credential      string `json:"-"`
	Credential_File string `json:"-"`
	Page_Size       int
	Max_Pages       int
	Allow_Plaintext bool

	definitions []datasetDefinition
}

var _ hosted.Config = (*Config)(nil)

func (c *Config) Verify() error {
	if c == nil {
		return errors.New("nil n8n configuration")
	}
	c.ApplyDefaultIngesterUUID(defaultIngesterUUID)
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	if c.Lookback != 0 {
		return errors.New("Lookback is not supported because n8n collection uses complete API snapshots")
	}
	c.PollingConfig.ApplyDefaults(0, defaultRequestsPerMin, defaultInterval)
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > 10000 {
		return errors.New("Requests-Per-Minute must be between 1 and 10000")
	}
	if c.Request_Interval < 60 || int64(c.Request_Interval) > maximumRequestInterval {
		return fmt.Errorf("Request-Interval must be between 60 and %d seconds", maximumRequestInterval)
	}
	if c.Page_Size == 0 {
		c.Page_Size = defaultPageSize
	}
	if c.Page_Size < 1 || c.Page_Size > maximumPageSize {
		return fmt.Errorf("Page-Size must be between 1 and %d", maximumPageSize)
	}
	if c.Max_Pages == 0 {
		c.Max_Pages = defaultMaxPages
	}
	if c.Max_Pages < 1 || c.Max_Pages > maximumMaxPages {
		return fmt.Errorf("Max-Pages must be between 1 and %d", maximumMaxPages)
	}

	definitions, err := resolveDatasets(c.Dataset)
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
	if err != nil || base.Scheme == "" || base.Host == "" {
		return errors.New("Base-URL must be an absolute URL")
	}
	if base.Scheme != "https" && !(c.Allow_Plaintext && base.Scheme == "http") {
		return errors.New("Base-URL must use HTTPS unless Allow-Plaintext=true is explicitly set")
	}
	if base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" {
		return errors.New("Base-URL must not contain userinfo, query parameters, or a fragment")
	}
	// A non-canonical escaped path (%2f and friends) decodes to a path that passes the
	// /api/v1 suffix check but is sent to n8n verbatim, so every dataset request would
	// address a path the operator never configured.
	if base.RawPath != "" {
		return errors.New("Base-URL path must use canonical escaping")
	}
	if !strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/api/v1") {
		return errors.New("Base-URL path must end with /api/v1")
	}
	c.Base_URL = strings.TrimRight(base.String(), "/")
	c.Credential = strings.TrimSpace(c.Credential)
	if _, err := c.credential(); err != nil {
		return err
	}
	for _, definition := range definitions {
		if err := ingest.CheckTag(c.tag(definition)); err != nil {
			return fmt.Errorf("invalid tag for Dataset %q: %w", definition.Name, err)
		}
	}
	c.definitions = definitions
	return nil
}

func (c *Config) Equal(other any) bool {
	nc, ok := hosted.EqualTarget[Config](other)
	if c == nil || !ok {
		return false
	}
	return c.BaseConfig == nc.BaseConfig &&
		c.MultiTagConfig == nc.MultiTagConfig &&
		c.PollingConfig == nc.PollingConfig &&
		c.Base_URL == nc.Base_URL &&
		c.Credential == nc.Credential &&
		c.Credential_File == nc.Credential_File &&
		c.Page_Size == nc.Page_Size &&
		c.Max_Pages == nc.Max_Pages &&
		c.Allow_Plaintext == nc.Allow_Plaintext &&
		slices.Equal(c.Dataset, nc.Dataset)
}

func (c *Config) SanitizedConfig() any {
	if c == nil {
		return nil
	}
	return struct {
		Dataset           []string
		BaseURL           string
		PageSize          int
		MaxPages          int
		TagName           string
		TagPrefix         string
		RequestsPerMinute int
		RequestInterval   int
		AllowPlaintext    bool
	}{
		Dataset: c.resolvedDatasetNames(), BaseURL: c.Base_URL,
		PageSize: c.Page_Size, MaxPages: c.Max_Pages,
		TagName: c.Tag_Name, TagPrefix: c.Tag_Prefix,
		RequestsPerMinute: c.Requests_Per_Minute, RequestInterval: c.Request_Interval,
		AllowPlaintext: c.Allow_Plaintext,
	}
}

func (c *Config) definitionsCopy() []datasetDefinition {
	return append([]datasetDefinition(nil), c.definitions...)
}

func (c *Config) resolvedDatasetNames() []string {
	definitions := c.definitions
	if len(definitions) == 0 {
		definitions, _ = resolveDatasets(c.Dataset)
	}
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func (c *Config) tag(definition datasetDefinition) string {
	return c.ResolveTag(definition.TagKind, Name)
}

func (c *Config) Tags() []string {
	definitions := c.definitions
	if len(definitions) == 0 {
		definitions, _ = resolveDatasets(c.Dataset)
	}
	tags := make([]string, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		tag := c.tag(definition)
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	return tags
}

func (c *Config) stateKey(definition datasetDefinition) string {
	identity := strings.Join([]string{Version, c.Base_URL, definition.Name, c.tag(definition)}, "\x00")
	// Typed scalar identities must not interpret manifests from the earlier
	// untyped identity or aggregate-digest namespaces. Starting this namespace
	// intentionally replays one snapshot, then normal per-record dedup resumes.
	return fmt.Sprintf("n8n/%s/snapshot-v6/%x", definition.Name, sha256.Sum256([]byte(identity)))
}

func (c *Config) credential() (string, error) {
	if strings.TrimSpace(c.Credential_File) != "" {
		return readCredentialFile(c.Credential_File)
	}
	if c.Credential == "" {
		return "", errors.New("Credential or Credential-File is required")
	}
	return c.Credential, nil
}
