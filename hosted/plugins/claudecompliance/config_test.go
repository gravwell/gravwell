package claudecompliance_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/configtest"
	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/plugins/claudecompliance"
	"github.com/gravwell/gravwell/v3/hosted/storage"
	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

type mockMuxer struct{}

func (mockMuxer) NegotiateTag(string) (entry.EntryTag, error)      { return 1, nil }
func (mockMuxer) SyncContext(context.Context, time.Duration) error { return nil }

func TestRegisteredExample(t *testing.T) {
	b, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "/opt/gravwell/secrets/claude-compliance-key", key)
	p := filepath.Join(t.TempDir(), "example.conf")
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
	var c struct {
		Global config.IngestConfig
		State  storage.BoltConfig
		plugins.Configs
	}
	if err := config.LoadConfigFile(&c, p); err != nil {
		t.Fatal(err)
	}
	if err := c.Configs.Verify(); err != nil {
		t.Fatal(err)
	}
	if c.IngesterCount() != 1 {
		t.Fatal("incorrect registration count")
	}
	// The shipped stanza selects four datasets across three families, so it
	// negotiates one tag per family and not one per endpoint.
	tags, err := c.Tags()
	want := []string{"claude-compliance-activities", "claude-compliance-conversations", "claude-compliance-directory"}
	if err != nil || len(tags) != len(want) {
		t.Fatalf("tags %v: %v", tags, err)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags %v, want %v", tags, want)
		}
	}
	n := 0
	for name, b := range c.Builders() {
		n++
		if name != "claude" || b.Kind() != claudecompliance.Name || b.ID() != claudecompliance.ID || b.Version() != claudecompliance.Version {
			t.Fatal("incorrect builder identity")
		}
		if _, err := b.Build(mockMuxer{}, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		// A muxer with no ingest delivery barrier must be refused: a queued
		// write is not a delivered one, so a checkpoint must never advance
		// on the strength of it.
		if _, err := b.Build(nil, nil); err == nil {
			t.Fatal("a muxer with no delivery barrier was accepted")
		}
		if _, ok := b.Config().(hosted.Config); !ok {
			t.Fatal("missing config interface")
		}
	}
	if n != 1 {
		t.Fatal("builder not registered")
	}
	if err := (plugins.Configs{ClaudeCompliance: map[string]*claudecompliance.Config{"bad": nil}}).Verify(); err == nil {
		t.Fatal("nil configuration accepted")
	}
}

// Lookback is the standard polling setting in whole hours: the shared
// parser rejects anything that is not an integer, and Verify rejects a value
// that could not be converted to a duration.
func TestRegisteredLookback(t *testing.T) {
	b, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	base := strings.ReplaceAll(string(b), "/opt/gravwell/secrets/claude-compliance-key", key)

	load := func(t *testing.T, value string) (*claudecompliance.Config, error) {
		t.Helper()
		s := strings.Replace(base, "Lookback=24", "Lookback="+value, 1)
		p := filepath.Join(t.TempDir(), "lookback.conf")
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
		var c struct {
			Global config.IngestConfig
			State  storage.BoltConfig
			plugins.Configs
		}
		if err := config.LoadConfigFile(&c, p); err != nil {
			return nil, err
		}
		if err := c.Configs.Verify(); err != nil {
			return nil, err
		}
		return c.ClaudeCompliance["claude"], nil
	}

	for value, want := range map[string]int{
		"1": 1, "24": 24, "168": 168,
		// An explicit zero means "unset", as it does for every other
		// zero-valued polling setting, and takes the documented default.
		"0": 24,
		// The exact representability boundary is accepted.
		"2562047": 2562047,
	} {
		t.Run(value, func(t *testing.T) {
			got, err := load(t, value)
			if err != nil {
				t.Fatalf("Lookback=%s rejected: %v", value, err)
			}
			if got == nil || got.PollingConfig.Lookback != want {
				t.Fatalf("Lookback=%s resolved to %#v, want %d hours", value, got, want)
			}
		})
	}

	for _, value := range []string{"-1", "1.5", "24h", "1d", "d", "", "2562048", "9223372036854775808"} {
		t.Run("reject-"+value, func(t *testing.T) {
			if got, err := load(t, value); err == nil {
				t.Fatalf("accepted Lookback=%s as %d hours", value, got.PollingConfig.Lookback)
			}
		})
	}
}

func TestCatalogRemainsSixTags(t *testing.T) {
	tags := map[string]bool{}
	for _, d := range claudecompliance.Datasets {
		tags[d.Tag] = true
	}
	if len(claudecompliance.Datasets) != 26 || len(tags) != 6 {
		t.Fatal("unexpected source or tag expansion")
	}
	for _, name := range []string{"activities", "directory", "conversations", "projects", "artifacts", "sessions"} {
		if !tags[name] {
			t.Fatal("missing family", name)
		}
	}
}

// TestConfigEqualCoversEveryField uses the shared reflective check so a
// field added later cannot quietly escape Equal, which would leave the
// runner unable to notice a real configuration change on reload.
func TestConfigEqualCoversEveryField(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	configtest.CheckEqual(t, claudecompliance.Config{
		BaseConfig:     hosted.BaseConfig{Ingester_UUID: "00000000-0000-4000-8000-000000000321"},
		MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "claude"},
		PollingConfig: hosted.PollingConfig{
			Lookback: 24, Requests_Per_Minute: 30, Request_Interval: 300,
		},
		Host:            "https://api.anthropic.com",
		Dataset:         []string{"activities"},
		Credential:      "synthetic",
		Credential_File: key,
		Parameter:       []string{"organization_id:o1"},
		Page_Size:       100,
		Max_Pages:       1000,
		Max_Retries:     4,
		Overlap_Seconds: 300,
		Follow_Children: "enabled",
		Max_Children:    100,
		Max_Pending:     10000,
	})
}
