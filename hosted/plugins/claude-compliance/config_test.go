package claudecompliance_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/plugins/claude-compliance"
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
	s = strings.ReplaceAll(s, "00000000-0000-0000-0000-000000000000", "00000000-0000-4000-8000-000000000321")
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
	tags, err := c.Tags()
	if err != nil || len(tags) != 1 || tags[0] != "claude-compliance-activities" {
		t.Fatalf("tags %v: %v", tags, err)
	}
	n := 0
	for name, b := range c.Builders() {
		n++
		if name != "activities" || b.Kind() != claudecompliance.Name || b.ID() != claudecompliance.ID || b.Version() != claudecompliance.Version {
			t.Fatal("incorrect builder identity")
		}
		if _, err := b.Build(mockMuxer{}, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Build(nil, nil); err == nil {
			t.Fatal("missing muxer accepted")
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

func TestRegisteredLookbackCompatibility(t *testing.T) {
	b, err := os.ReadFile("example.conf")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("synthetic-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	base := strings.ReplaceAll(string(b), "/opt/gravwell/secrets/claude-compliance-key", key)
	base = strings.ReplaceAll(base, "00000000-0000-0000-0000-000000000000", "00000000-0000-4000-8000-000000000321")
	for value, want := range map[string]int64{"24": 24, "24h": 24, "1d": 24, "7d": 168, "1d12h": 36} {
		t.Run(value, func(t *testing.T) {
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
				t.Fatal(err)
			}
			if err := c.Configs.Verify(); err != nil {
				t.Fatal(err)
			}
			got := c.ClaudeCompliance["activities"]
			if got == nil || int64(got.Lookback) != want || got.PollingConfig.Lookback != int(want) {
				t.Fatalf("Lookback = %#v", got)
			}
		})
	}
	for _, value := range []string{"-1", "1.5h", "24m", "1h1d", "d", "h", "18446744073709551615h", "18446744073709551615d"} {
		t.Run("reject-"+value, func(t *testing.T) {
			s := strings.Replace(base, "Lookback=24", "Lookback="+value, 1)
			p := filepath.Join(t.TempDir(), "invalid.conf")
			if err := os.WriteFile(p, []byte(s), 0600); err != nil {
				t.Fatal(err)
			}
			var c struct {
				Global config.IngestConfig
				State  storage.BoltConfig
				plugins.Configs
			}
			if err := config.LoadConfigFile(&c, p); err == nil {
				t.Fatalf("accepted Lookback=%s", value)
			}
		})
	}
}

func TestCatalogRemainsSixTags(t *testing.T) {
	tags := map[string]bool{}
	for _, d := range claudecompliance.Catalog() {
		tags[d.Tag] = true
	}
	if len(claudecompliance.Catalog()) != 26 || len(tags) != 6 {
		t.Fatal("unexpected source or tag expansion")
	}
	for _, name := range []string{"activities", "directory", "conversations", "projects", "artifacts", "sessions"} {
		if !tags[name] {
			t.Fatal("missing family", name)
		}
	}
}
