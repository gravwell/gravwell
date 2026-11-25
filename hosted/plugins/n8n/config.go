/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/ingest"
)

const (
	defaultIngesterUUIDStr   = "291e5806-07f4-4533-a979-3296263389bf"
	defaultRequestsPerMin    = 30
	defaultInterval          = 300
	defaultPageSize          = 100
	defaultMaxPages          = 1000
	maximumPageSize          = 250
	maximumMaxPages          = 100000
	maximumLookbackHours     = 2_562_047     // largest whole-hour delay representable by time.Duration
	maximumRequestInterval   = 9_223_372_036 // largest whole-second delay representable by time.Duration
	maximumRequestsPerMinute = 10000         // highest accepted client-side request rate
	minimumRequestInterval   = 60            // shortest accepted delay between polls, in seconds
)

type Config struct {
	hosted.BaseConfig
	hosted.MultiTagConfig
	hosted.PollingConfig

	Dataset                []string
	Base_URL               string
	Credential             string `json:"-"`
	Credential_File        string `json:"-"`
	Page_Size              int
	Max_Pages              int
	Allow_Plaintext        bool
	Include_Execution_Data bool

	definitions []datasetDefinition
}

var _ hosted.Config = (*Config)(nil)

func (c *Config) Verify() error {
	if c == nil {
		return errors.New("nil n8n configuration")
	}
	c.ApplyDefaultIngesterUUID(defaultIngesterUUIDStr)
	if err := c.BaseConfig.Verify(); err != nil {
		return err
	}
	if c.Lookback < 0 || c.Lookback > maximumLookbackHours {
		return fmt.Errorf("Lookback must be between 0 and %d hours", maximumLookbackHours)
	}
	c.PollingConfig.ApplyDefaults(0, defaultRequestsPerMin, defaultInterval)
	if c.Requests_Per_Minute < 1 || c.Requests_Per_Minute > maximumRequestsPerMinute {
		return fmt.Errorf("Requests-Per-Minute must be between 1 and %d", maximumRequestsPerMinute)
	}
	if c.Request_Interval < minimumRequestInterval || int64(c.Request_Interval) > maximumRequestInterval {
		return fmt.Errorf("Request-Interval must be between %d and %d seconds", minimumRequestInterval, maximumRequestInterval)
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
	hasExecutions := false
	for _, definition := range definitions {
		hasExecutions = hasExecutions || definition.Name == "executions"
	}
	if c.Include_Execution_Data && !hasExecutions {
		return errors.New("Include-Execution-Data requires Dataset=executions")
	}
	if c.Lookback != 0 && !c.Include_Execution_Data {
		return errors.New("Lookback is supported only when Include-Execution-Data=true")
	}
	if c.Tag_Name != "" && len(definitions) != 1 {
		return errors.New("Tag-Name requires exactly one Dataset")
	}
	if err := c.ValidateTags(); err != nil {
		return err
	}

	base, err := url.Parse(strings.TrimSpace(c.Base_URL))
	if err != nil || base.Scheme == "" || base.Hostname() == "" {
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
		c.Include_Execution_Data == nc.Include_Execution_Data &&
		slices.Equal(c.Dataset, nc.Dataset)
}

func (c *Config) SanitizedConfig() any {
	if c == nil {
		return nil
	}
	return struct {
		Dataset              []string
		BaseURL              string
		PageSize             int
		MaxPages             int
		TagName              string
		TagPrefix            string
		RequestsPerMinute    int
		RequestInterval      int
		Lookback             int
		AllowPlaintext       bool
		IncludeExecutionData bool
	}{
		Dataset: c.resolvedDatasetNames(), BaseURL: c.Base_URL,
		PageSize: c.Page_Size, MaxPages: c.Max_Pages,
		TagName: c.Tag_Name, TagPrefix: c.Tag_Prefix,
		RequestsPerMinute: c.Requests_Per_Minute, RequestInterval: c.Request_Interval,
		Lookback:       c.Lookback,
		AllowPlaintext: c.Allow_Plaintext, IncludeExecutionData: c.Include_Execution_Data,
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
	// The runner's state bucket already scopes data to this plugin stanza and
	// Ingester UUID. Keeping mutable source and tag settings out of the key lets
	// a verified reload continue the same checkpoint. Operators who intentionally
	// change source identity use a new Ingester UUID, which selects a new bucket.
	return fmt.Sprintf("n8n/%s/snapshot-v1", definition.Name)
}

func (c *Config) credential() (string, error) {
	inline, file := strings.TrimSpace(c.Credential), strings.TrimSpace(c.Credential_File)
	if (inline != "") == (file != "") {
		return "", errors.New("exactly one of Credential or Credential-File is required")
	}
	if file != "" {
		return readCredentialFile(c.Credential_File)
	}
	return inline, nil
}

const maximumCredentialFileSize = 64 << 10

// The pre-open Lstat is not an atomic guarantee: an attacker able to replace
// the configured path could swap in a FIFO or symlink before open. Nonblocking,
// no-follow open lets the authoritative descriptor checks below fail closed
// without pinning the polling goroutine or following the replacement.
func openCredentialFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}

func readCredentialFile(path string) (string, error) {
	return readCredentialFileWithOpen(path, openCredentialFile)
}

func readCredentialFileWithOpen(path string, openFile func(string) (*os.File, error)) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("Credential-File is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("Credential-File must be a regular file and not a symbolic link")
	}
	f, err := openFile(path)
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	defer f.Close()

	openedInfo, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return "", errors.New("Credential-File changed during validation")
	}
	if !openedInfo.Mode().IsRegular() {
		return "", errors.New("Credential-File must be a regular file and not a symbolic link")
	}
	if openedInfo.Size() < 1 || openedInfo.Size() > maximumCredentialFileSize {
		return "", fmt.Errorf("Credential-File must be between 1 byte and %d bytes", maximumCredentialFileSize)
	}
	if openedInfo.Mode().Perm()&0o077 != 0 {
		return "", errors.New("Credential-File must not be accessible by group or other users")
	}
	body, err := readCredentialContents(f, openedInfo.Size())
	if err != nil {
		return "", fmt.Errorf("Credential-File: %w", err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", errors.New("Credential-File is empty")
	}
	return value, nil
}

func readCredentialContents(r io.Reader, expectedSize int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maximumCredentialFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) < 1 || len(body) > maximumCredentialFileSize {
		return nil, fmt.Errorf("Credential-File must be between 1 byte and %d bytes", maximumCredentialFileSize)
	}
	if int64(len(body)) != expectedSize {
		return nil, errors.New("Credential-File changed during read")
	}
	return body, nil
}
