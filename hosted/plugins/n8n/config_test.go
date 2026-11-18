/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package n8n

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gcfg"
	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/configtest"
	"github.com/gravwell/gravwell/v3/ingest"
)

func testCredentialFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "n8n-api-key")
	if err := os.WriteFile(path, []byte("synthetic-test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testConfig(t *testing.T, baseURL string, datasets ...string) *Config {
	t.Helper()
	conf := &Config{
		Base_URL: baseURL, Credential_File: testCredentialFile(t), Dataset: datasets,
		Allow_Plaintext: true, PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 10000},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return conf
}

func TestVerifyRequiresExplicitDataset(t *testing.T) {
	for name, datasets := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			conf := &Config{Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: datasets}
			if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Dataset is required") {
				t.Fatalf("Verify error=%v want Dataset-required rejection", err)
			}
			if len(conf.Tags()) != 0 || len(conf.resolvedDatasetNames()) != 0 {
				t.Fatalf("omitted Dataset resolved tags=%v datasets=%v", conf.Tags(), conf.resolvedDatasetNames())
			}
		})
	}
}

func TestIncludeExecutionDataConfigurationParsing(t *testing.T) {
	const source = `
[N8n "primary"]
Base-URL="https://n8n.example.com/api/v1"
Credential="synthetic"
Dataset="executions"
Include-Execution-Data=true
`
	var parsed struct {
		N8n map[string]*Config
	}
	if err := gcfg.ReadStringInto(&parsed, source); err != nil {
		t.Fatalf("parse Include-Execution-Data: %v", err)
	}
	conf := parsed.N8n["primary"]
	if conf == nil || !conf.Include_Execution_Data {
		t.Fatalf("parsed configuration=%+v", conf)
	}
}

func TestVerifyExpandedExecutionLookback(t *testing.T) {
	conf := &Config{
		Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t),
		Dataset: []string{"executions"}, Allow_Plaintext: true,
		Include_Execution_Data: true, PollingConfig: hosted.PollingConfig{Lookback: 2},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("expanded execution Lookback: %v", err)
	}
	if conf.Lookback != 2 {
		t.Fatalf("Lookback=%d want 2", conf.Lookback)
	}

	conf = &Config{
		Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t),
		Dataset: []string{"users"}, Allow_Plaintext: true, Include_Execution_Data: true,
	}
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "requires Dataset=executions") {
		t.Fatalf("missing executions error=%v", err)
	}
}

// Lookback is limited to the largest whole number of hours a time.Duration
// can hold, so the first incremental window never starts after its end.
func TestVerifyLookbackRange(t *testing.T) {
	for _, tc := range []struct {
		lookback int
		valid    bool
	}{
		{lookback: -1},
		{lookback: 2562047, valid: true},
		{lookback: 2562048},
	} {
		conf := &Config{
			Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t),
			Allow_Plaintext: true, Dataset: []string{"executions"}, Include_Execution_Data: true,
			PollingConfig: hosted.PollingConfig{Lookback: tc.lookback},
		}
		err := conf.Verify()
		if tc.valid {
			if err != nil {
				t.Fatalf("Lookback=%d: %v", tc.lookback, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "Lookback must be between") {
			t.Fatalf("Lookback=%d error=%v want range rejection", tc.lookback, err)
		}
	}
}

func TestVerifyDatasetSelectionContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		datasets []string
	}{
		{"blank", []string{""}}, {"whitespace", []string{" \t"}},
		{"mixed blank", []string{"users", " "}}, {"unknown", []string{"not-a-dataset"}},
		{"mixed unknown", []string{"users", "not-a-dataset"}},
		{"duplicate", []string{"users", "users"}}, {"normalized duplicate", []string{"users", " USERS "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: tc.datasets}
			if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Dataset") {
				t.Fatalf("Verify error=%v, want Dataset rejection", err)
			}
		})
	}
	t.Run("valid normalized", func(t *testing.T) {
		conf := testConfig(t, "http://127.0.0.1:12345/api/v1", " USERS ", "workflows")
		if got := conf.resolvedDatasetNames(); !slices.Equal(got, []string{"users", "workflows"}) {
			t.Fatalf("datasets=%v", got)
		}
		if got := conf.Tags(); !slices.Equal(got, []string{"n8n-users", "n8n-workflows"}) {
			t.Fatalf("tags=%v", got)
		}
	})
}

func TestVerifyResolvedOutputTagValidation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		datasets        []string
		tagName, prefix string
		invalid         bool
	}{
		{"name newline", []string{"users"}, "bad\ntag", "", true},
		{"prefix newline", []string{"users", "workflows"}, "", "bad\ntag", true},
		{"name too long", []string{"users"}, strings.Repeat("a", ingest.MAX_TAG_LENGTH+1), "", true},
		{"prefix too long", []string{"users", "workflows"}, "", strings.Repeat("a", ingest.MAX_TAG_LENGTH-len("-workflows")+1), true},
		{"name exact", []string{"users"}, strings.Repeat("a", ingest.MAX_TAG_LENGTH), "", false},
		{"prefix exact", []string{"users", "workflows"}, "", strings.Repeat("a", ingest.MAX_TAG_LENGTH-len("-workflows")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: tc.datasets, MultiTagConfig: hosted.MultiTagConfig{Tag_Name: tc.tagName, Tag_Prefix: tc.prefix}}
			err := conf.Verify()
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "invalid tag for Dataset") {
					t.Fatalf("Verify error=%v, want output tag rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify exact-length tag: %v", err)
			}
		})
	}
}

func TestTagConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		datasets  []string
		tagName   string
		tagPrefix string
		wantTags  []string
	}{
		{
			name: "explicit all dataset tags", datasets: catalogNames(),
			wantTags: []string{"n8n-audit", "n8n-creds", "n8n-executions", "n8n-projects", "n8n-tags", "n8n-users", "n8n-variables", "n8n-workflows"},
		},
		{
			name: "custom prefix", datasets: []string{"users", "workflows"}, tagPrefix: "custom",
			wantTags: []string{"custom-users", "custom-workflows"},
		},
		{
			name: "single dataset tag name", datasets: []string{"users"}, tagName: "users-only",
			wantTags: []string{"users-only"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{
				Base_URL:        "http://127.0.0.1:12345/api/v1",
				Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: tc.datasets,
				MultiTagConfig: hosted.MultiTagConfig{Tag_Name: tc.tagName, Tag_Prefix: tc.tagPrefix},
			}
			if err := conf.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if conf.Ingester_UUID != defaultIngesterUUIDStr {
				t.Fatalf("Ingester-UUID=%q want %q", conf.Ingester_UUID, defaultIngesterUUIDStr)
			}
			if conf.Request_Interval != defaultInterval || conf.Requests_Per_Minute != defaultRequestsPerMin || conf.Page_Size != defaultPageSize || conf.Max_Pages != defaultMaxPages {
				t.Fatalf("defaults changed: %+v", conf)
			}
			if got := conf.Tags(); !slices.Equal(got, tc.wantTags) {
				t.Fatalf("Tags=%v want %v", got, tc.wantTags)
			}
		})
	}
}

func TestCredentialDatasetContract(t *testing.T) {
	definition := datasetCatalog["credentials"]
	if definition.Method != "GET" || definition.Path != "/credentials" || definition.CursorField != "nextCursor" {
		t.Fatalf("unexpected credentials contract: %+v", definition)
	}
}

func TestCredentialConfigurationModes(t *testing.T) {
	credentialFile := testCredentialFile(t)
	tests := []struct {
		name           string
		credential     string
		credentialFile string
		want           string
		wantError      string
	}{
		{name: "inline only", credential: "  inline-test-key  ", want: "inline-test-key"},
		{name: "file only", credentialFile: credentialFile, want: "synthetic-test-key"},
		{name: "both valid", credential: "inline-test-key", credentialFile: credentialFile, wantError: "exactly one"},
		{name: "neither", wantError: "exactly one"},
		{name: "both invalid file", credential: "inline-test-key", credentialFile: filepath.Join(t.TempDir(), "missing"), wantError: "exactly one"},
		{name: "configured file fails closed", credentialFile: filepath.Join(t.TempDir(), "missing"), wantError: "Credential-File"},
		{name: "blank inline", credential: " \t", credentialFile: credentialFile, want: "synthetic-test-key"},
		{name: "blank file", credential: "inline-test-key", credentialFile: " \t", want: "inline-test-key"},
		{name: "both blank", credential: " \t", credentialFile: " \n", wantError: "exactly one"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{
				Base_URL: "http://127.0.0.1:12345/api/v1", Credential: tc.credential, Credential_File: tc.credentialFile,
				Allow_Plaintext: true, Dataset: []string{"users"},
			}
			err := conf.Verify()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Verify error=%v want substring %q", err, tc.wantError)
				}
				if _, directErr := conf.credential(); directErr == nil || !strings.Contains(directErr.Error(), tc.wantError) {
					t.Fatalf("credential error=%v want substring %q", directErr, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			got, err := conf.credential()
			if err != nil {
				t.Fatalf("credential: %v", err)
			}
			if got != tc.want {
				t.Fatalf("credential=%q want %q", got, tc.want)
			}
		})
	}
}

func TestVerifyRejectsUnsupportedAndManagedConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"lookback without expanded executions", func(c *Config) { c.Lookback = 1 }, "only when Include-Execution-Data=true"},
		{"bad API path", func(c *Config) { c.Base_URL = "http://127.0.0.1:12345" }, "/api/v1"},
		{"tag name multiple", func(c *Config) { c.Tag_Name = "one" }, "exactly one Dataset"},
		{"tag name and prefix", func(c *Config) { c.Dataset = []string{"users"}; c.Tag_Name = "one"; c.Tag_Prefix = "two" }, "cannot be used together"},
		{"forced empty query", func(c *Config) { c.Base_URL = "http://127.0.0.1:12345/api/v1?" }, "query parameters"},
		{"encoded path separator", func(c *Config) { c.Base_URL = "http://127.0.0.1:12345/api/v1%2f" }, "canonical escaping"},
		{"literal space", func(c *Config) { c.Base_URL = "https://127.0.0.1:12345/a b/api/v1" }, "canonical escaping"},
		{"unnecessary escape", func(c *Config) { c.Base_URL = "https://127.0.0.1:12345/%61pi/v1" }, "canonical escaping"},
		{"plaintext without Allow-Plaintext", func(c *Config) { c.Allow_Plaintext = false }, "must use HTTPS"},
		{"userinfo", func(c *Config) { c.Base_URL = "https://user:synthetic@127.0.0.1:12345/api/v1" }, "must not contain userinfo"},
		{"port without host name", func(c *Config) { c.Base_URL = "https://:443/api/v1" }, "absolute URL"},
		{"plaintext port without host name", func(c *Config) { c.Base_URL = "http://:12345/api/v1" }, "absolute URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{Base_URL: "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: []string{"users", "workflows"}}
			tc.mutate(conf)
			if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify error=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestVerifyAcceptsExactOperationalBounds(t *testing.T) {
	conf := &Config{
		Base_URL:        "http://127.0.0.1:12345/api/v1",
		Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: []string{"users"},
		PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 1, Request_Interval: 60},
		Page_Size:     1, Max_Pages: 1,
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("minimum bounds: %v", err)
	}
	conf = &Config{
		Base_URL:        "http://127.0.0.1:12345/api/v1",
		Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: []string{"users"},
		PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 10000, Request_Interval: 60},
		Page_Size:     maximumPageSize, Max_Pages: maximumMaxPages,
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("maximum bounds: %v", err)
	}
	if strconv.IntSize >= 64 {
		maximumInterval := maximumRequestInterval
		conf.Request_Interval = int(maximumInterval)
		if err := conf.Verify(); err != nil {
			t.Fatalf("maximum Request-Interval: %v", err)
		}
	}
}

func TestRequestIntervalRepresentability(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("the platform int cannot exceed time.Duration seconds")
	}
	maximumSeconds := int64(math.MaxInt64 / int64(time.Second))
	if got := int64(maximumRequestInterval); got != maximumSeconds {
		t.Fatalf("maximumRequestInterval=%d want %d", got, maximumSeconds)
	}
	conf := &Config{
		Base_URL:        "http://127.0.0.1:12345/api/v1",
		Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: []string{"users"},
		PollingConfig: hosted.PollingConfig{Requests_Per_Minute: 1, Request_Interval: int(maximumSeconds)},
	}
	if err := conf.Verify(); err != nil {
		t.Fatalf("maximum Request-Interval: %v", err)
	}
	continuation := conf.ContinueAfterInterval()
	if continuation == nil || continuation.Delay != time.Duration(maximumSeconds)*time.Second || continuation.Delay <= 0 {
		t.Fatalf("maximum continuation=%+v", continuation)
	}
	conf.Request_Interval = int(maximumSeconds + 1)
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Request-Interval") {
		t.Fatalf("maximum-plus-one Verify error=%v", err)
	}
}

func TestSingleAndMultiDatasetValidationUsesCommonBounds(t *testing.T) {
	tests := []struct {
		name     string
		datasets []string
	}{
		{name: "single", datasets: []string{"users"}},
		{name: "multi", datasets: []string{"credentials", "users", "workflows"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newConfig := func() *Config {
				return &Config{
					Base_URL:        "http://127.0.0.1:12345/api/v1",
					Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: append([]string(nil), tc.datasets...),
					Page_Size: maximumPageSize,
				}
			}

			if err := newConfig().Verify(); err != nil {
				t.Fatalf("maximum Page-Size: %v", err)
			}
			over := newConfig()
			over.Page_Size = maximumPageSize + 1
			if err := over.Verify(); err == nil || !strings.Contains(err.Error(), "Page-Size") {
				t.Fatalf("maximum-plus-one Page-Size error=%v", err)
			}
			badEndpoint := newConfig()
			badEndpoint.Base_URL = "http://127.0.0.1:12345/not-api-v1"
			if err := badEndpoint.Verify(); err == nil || !strings.Contains(err.Error(), "/api/v1") {
				t.Fatalf("invalid endpoint error=%v", err)
			}
		})
	}
}

func TestEqualCoversOperatorConfiguration(t *testing.T) {
	left := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
	right := *left
	if !left.Equal(&right) || !left.Equal(right) {
		t.Fatal("equal configuration did not compare equal")
	}
	right.Page_Size++
	if left.Equal(&right) {
		t.Fatal("changed Page-Size compared equal")
	}
	right = *left
	right.Include_Execution_Data = true
	if left.Equal(&right) {
		t.Fatal("changed Include-Execution-Data compared equal")
	}
	right = *left
	right.Base_URL = "http://127.0.0.1:54321/api/v1"
	if left.Equal(&right) {
		t.Fatal("changed Base-URL compared equal")
	}
}

func TestEqualCoversEveryExportedOperatorField(t *testing.T) {
	configtest.CheckEqual(t, *testConfig(t, "http://127.0.0.1:12345/api/v1", "users"))
}

func TestSanitizedConfigReportsResolvedNonSecretSettings(t *testing.T) {
	conf := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
	conf.Credential = "inline-secret-must-not-appear"
	conf.Include_Execution_Data = true
	body, err := json.Marshal(conf.SanitizedConfig())
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"Dataset":["users"]`) || !strings.Contains(text, `"AllowPlaintext":true`) || !strings.Contains(text, `"IncludeExecutionData":true`) {
		t.Fatalf("sanitized config omitted operator settings: %s", text)
	}
	if strings.Contains(text, conf.Credential_File) || strings.Contains(text, conf.Credential) || strings.Contains(text, "synthetic-test-key") {
		t.Fatalf("sanitized config exposed credential material: %s", text)
	}
}

func TestStateKeyUsesRunnerOwnedScope(t *testing.T) {
	base := testConfig(t, "http://127.0.0.1:12345/api/v1", "users")
	definition := datasetCatalog["users"]
	key := base.stateKey(definition)
	const initialReleaseKey = "n8n/users/snapshot-v1"
	if key != initialReleaseKey {
		t.Fatalf("state key=%q want initial public state key %q", key, initialReleaseKey)
	}

	differentSource := *base
	differentSource.Base_URL = "http://127.0.0.1:54321/api/v1"
	if key != differentSource.stateKey(definition) {
		t.Fatal("mutable Base-URL changed runner-scoped snapshot key")
	}

	differentTag := *base
	differentTag.Tag_Prefix = "renamed"
	if key != differentTag.stateKey(definition) {
		t.Fatal("mutable resolved tag changed runner-scoped snapshot key")
	}
}

func TestCredentialFileProtection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentialFile(path); err == nil || !strings.Contains(err.Error(), "group or other") {
		t.Fatalf("permissions error=%v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentialFile(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink error=%v", err)
	}

	oversized := filepath.Join(dir, "oversized")
	if err := os.WriteFile(oversized, make([]byte, maximumCredentialFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentialFile(oversized); err == nil || !strings.Contains(err.Error(), "between 1 byte") {
		t.Fatalf("oversized error=%v", err)
	}
}

func TestCredentialFileShortReadFailsClosed(t *testing.T) {
	if _, err := readCredentialContents(strings.NewReader("short"), int64(len("complete"))); err == nil || !strings.Contains(err.Error(), "changed during read") {
		t.Fatalf("short read error=%v", err)
	}
}

func TestCredentialFileOpenRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "credential.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := openCredentialFile(fifo)
		if err == nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Credential-File open blocked on a FIFO")
	}
	if _, err := readCredentialFile(fifo); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("FIFO error=%v want regular-file rejection", err)
	}
}

func TestCredentialFileOpenRefusesSymlinkAndAllowsRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "credential")
	if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "credential-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openCredentialFile(link); err == nil {
		_ = f.Close()
		t.Fatal("Credential-File open followed a symbolic link")
	}
	if got, err := readCredentialFile(target); err != nil || got != "synthetic" {
		t.Fatalf("regular Credential-File read=%q error=%v", got, err)
	}
}

func TestCredentialFileDescriptorRevalidationRejectsRegularFilePathSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential")
	backup := filepath.Join(dir, "credential-before-swap")
	if err := os.WriteFile(path, []byte("original-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	opener := func(path string) (*os.File, error) {
		if err := os.Rename(path, backup); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte("replacement!!"), 0o600); err != nil {
			return nil, err
		}
		return openCredentialFile(path)
	}
	if _, err := readCredentialFileWithOpen(path, opener); err == nil || !strings.Contains(err.Error(), "changed during validation") {
		t.Fatalf("path-swap error=%v want descriptor revalidation failure", err)
	}
}

func TestVerifyRejectsValuesOutsideOperationalBounds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		polling   hosted.PollingConfig
		wantError string
	}{
		{name: "Requests-Per-Minute maximum", polling: hosted.PollingConfig{Requests_Per_Minute: 10000}},
		{name: "Requests-Per-Minute above maximum", polling: hosted.PollingConfig{Requests_Per_Minute: 10001}, wantError: "Requests-Per-Minute must be between 1 and 10000"},
		{name: "Request-Interval minimum", polling: hosted.PollingConfig{Request_Interval: 60}},
		{name: "Request-Interval below minimum", polling: hosted.PollingConfig{Request_Interval: 59}, wantError: "Request-Interval must be between 60 and "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{
				Base_URL:        "http://127.0.0.1:12345/api/v1",
				Credential_File: testCredentialFile(t), Allow_Plaintext: true, Dataset: []string{"users"},
				PollingConfig: tc.polling,
			}
			err := conf.Verify()
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("Verify: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Verify error=%v want %q", err, tc.wantError)
			}
		})
	}
}

func TestVerifyRejectsInvalidIngesterUUID(t *testing.T) {
	conf := &Config{
		BaseConfig: hosted.BaseConfig{Ingester_UUID: "not-a-uuid"},
		Base_URL:   "http://127.0.0.1:12345/api/v1", Credential_File: testCredentialFile(t),
		Allow_Plaintext: true, Dataset: []string{"users"},
	}
	if err := conf.Verify(); err == nil || !strings.Contains(err.Error(), "Ingester-UUID") {
		t.Fatalf("Verify error=%v want an Ingester-UUID rejection", err)
	}

	conf.Ingester_UUID = ""
	if err := conf.Verify(); err != nil {
		t.Fatalf("empty Ingester-UUID: %v", err)
	}
	if conf.Ingester_UUID != defaultIngesterUUIDStr {
		t.Fatalf("Ingester-UUID=%q want the default %q", conf.Ingester_UUID, defaultIngesterUUIDStr)
	}
}

func TestCredentialFileRejectsGroupOnlyAccess(t *testing.T) {
	for _, tc := range []struct {
		mode   os.FileMode
		accept bool
	}{
		{mode: 0o640},
		{mode: 0o620},
		{mode: 0o610},
		{mode: 0o600, accept: true},
		{mode: 0o400, accept: true},
	} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(path, []byte("synthetic-test-key\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Chmod sets the exact mode; the mode passed to WriteFile is
			// subject to the process umask.
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			got, err := readCredentialFile(path)
			if tc.accept {
				if err != nil || got != "synthetic-test-key" {
					t.Fatalf("credential=%q error=%v", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "must not be accessible by group or other users") {
				t.Fatalf("error=%v want group-or-other rejection", err)
			}
		})
	}
}

func TestCredentialFileRejectsWhitespaceOnlyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(" \n\t \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentialFile(path); err == nil || !strings.Contains(err.Error(), "Credential-File is empty") {
		t.Fatalf("error=%v want empty-file rejection", err)
	}
}

func TestCredentialFileRejectsOtherOnlyAccess(t *testing.T) {
	for _, mode := range []os.FileMode{0o604, 0o602, 0o601} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(path, []byte("synthetic-test-key\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Chmod sets the exact mode; the mode passed to WriteFile is
			// subject to the process umask.
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := readCredentialFile(path); err == nil || !strings.Contains(err.Error(), "must not be accessible by group or other users") {
				t.Fatalf("error=%v want group-or-other rejection", err)
			}
		})
	}
}

func TestExampleIngesterUUIDsAreValidAndNonzero(t *testing.T) {
	example, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatalf("read example.conf: %v", err)
	}
	matches := regexp.MustCompile(`Ingester-UUID="([^"]*)"`).FindAllStringSubmatch(string(example), -1)
	if len(matches) == 0 {
		t.Fatal("example.conf shows no Ingester-UUID")
	}
	for _, match := range matches {
		id, err := uuid.Parse(match[1])
		if err != nil {
			t.Fatalf("example Ingester-UUID %q: %v", match[1], err)
		}
		if id == uuid.Nil {
			t.Fatalf("example Ingester-UUID %q is the zero UUID", match[1])
		}
	}
}

func TestExampleDocumentsCredentialAndExecutionSecurity(t *testing.T) {
	example, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatalf("read example.conf: %v", err)
	}
	if !strings.Contains(string(example), "Allow-Plaintext=false") || !strings.Contains(string(example), "API key in cleartext") {
		t.Error("example.conf must show the secure default and warn about cleartext credentials")
	}
	if !strings.Contains(string(example), "Include-Execution-Data=false") || !strings.Contains(string(example), "node inputs and outputs") {
		t.Error("example.conf must show the execution-data default and sensitivity boundary")
	}
	if !strings.Contains(string(example), "on the first poll only") || !strings.Contains(string(example), "one-Request-Interval overlap") {
		t.Error("example.conf must say that Lookback applies to the first poll only and that later polls overlap by one Request-Interval")
	}
	for _, required := range []string{"runner-wide settings shared", "Reuse an established runner's", "[State]", "Sync"} {
		if !strings.Contains(string(example), required) {
			t.Errorf("example.conf is missing runner-wide reuse statement %q", required)
		}
	}
}
