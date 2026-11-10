package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/plugins/slack"
	ingestconfig "github.com/gravwell/gravwell/v3/ingest/config"
)

func TestSlackPluginRegistration(t *testing.T) {
	config := &slack.Config{API_Token: "synthetic-token", MultiTagConfig: hosted.MultiTagConfig{Tag_Name: "slack-audit"}}
	configs := Configs{Slack: map[string]*slack.Config{"primary": config}}
	if err := configs.Verify(); err != nil {
		t.Fatal(err)
	}
	if configs.IngesterCount() != 1 {
		t.Fatalf("count=%d", configs.IngesterCount())
	}
	if tags, err := configs.Tags(); err != nil || len(tags) != 1 || tags[0] != "slack-audit" {
		t.Fatalf("tags=%v error=%v", tags, err)
	}
	builders := 0
	for name, builder := range configs.Builders() {
		builders++
		if name != "primary" || builder.Kind() != slack.Name || builder.ID() != slack.ID || builder.Version() != slack.Version {
			t.Fatalf("builder name=%q kind=%q id=%q version=%q", name, builder.Kind(), builder.ID(), builder.Version())
		}
		if _, ok := builder.(*SlackBuilder); !ok {
			t.Fatalf("builder type=%T", builder)
		}
		if ingester, err := builder.Build(nil, func() error { return nil }); err != nil || ingester == nil {
			t.Fatalf("build ingester=%T error=%v", ingester, err)
		}
	}
	if builders != 1 {
		t.Fatalf("builders=%d", builders)
	}
}

func TestSlackPluginStanzaLoadsThroughStandardConfigLoader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack.conf")
	body := []byte(`[Slack "primary"]
Ingester-UUID="5cbb0f50-8de8-4f62-9daf-f1d0f7063030"
Host="https://api.slack.com"
API-Token="synthetic-token"
Tag-Name="slack-audit"
Lookback=168
Request-Interval=300
Requests-Per-Minute=30
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Slack map[string]*slack.Config
	}
	if err := ingestconfig.LoadConfigFile(&parsed, path); err != nil {
		t.Fatal(err)
	}
	config := parsed.Slack["primary"]
	if config == nil || config.API_Token != "synthetic-token" || config.Lookback != 168 {
		t.Fatalf("parsed config=%+v", config)
	}
	if err := config.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestSlackPluginRegistrationRejectsNilConfig(t *testing.T) {
	configs := Configs{Slack: map[string]*slack.Config{"primary": nil}}
	if err := configs.Verify(); err == nil {
		t.Fatal("nil Slack config accepted")
	}
}
