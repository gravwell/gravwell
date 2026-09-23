//go:build upstream_registration

package microsoft_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted/plugins"
	"github.com/gravwell/gravwell/v3/hosted/plugins/microsoft"
	"github.com/gravwell/gravwell/v3/hosted/plugins/tester"
	"github.com/gravwell/gravwell/v3/ingest"
	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestEveryRegisteredMicrosoftSelectorBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.secret")
	if err := os.WriteFile(path, []byte("synthetic-only"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dataset := range microsoft.Catalog() {
		t.Run(dataset.Name, func(t *testing.T) {
			for _, tc := range []struct {
				lookback      string
				interval, rpm int
				valid         bool
			}{
				{"1h", 60, 1, true}, {"24h", 3600, 10, true}, {"7d", 3600, 10, true},
				{"1d12h", 3600, 6000, true}, {"672h", 9223372036, 10, true}, {"24", 3600, 10, true},
				{"673h", 60, 1, false}, {"0h", 60, 1, false}, {"-1h", 60, 1, false},
				{"24h", 59, 10, false}, {"24h", 9223372037, 10, false}, {"24h", 3600, 6001, false},
			} {
				var cfg plugins.Configs
				text := fmt.Sprintf("[Microsoft %q]\nIngester-UUID=550e8400-e29b-41d4-a716-446655440000\nTenant-ID=11111111-1111-1111-1111-111111111111\nClient-ID=22222222-2222-2222-2222-222222222222\nClient-Secret-File=%q\nSubscription-ID=33333333-3333-4333-8333-333333333333\nApi=%q\nLookback=%q\nRequest-Interval=%d\nRequests-Per-Minute=%d\n", dataset.Name, path, dataset.Name, tc.lookback, tc.interval, tc.rpm)
				err := config.LoadConfigBytes(&cfg, []byte(text))
				if err == nil {
					err = cfg.Verify()
				}
				if (err == nil) != tc.valid {
					t.Errorf("Lookback=%s interval=%d rpm=%d error=%v", tc.lookback, tc.interval, tc.rpm, err)
				}
				if err == nil {
					if cfg.IngesterCount() != 1 {
						t.Fatal("registered stanza missing")
					}
					for _, builder := range cfg.Builders() {
						if _, err := builder.Build(&ingest.IngestMuxer{}, func() error { return nil }); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		})
	}
}

func TestRegisteredMicrosoftConfigAndBuilderPreserveExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.secret")
	if err := os.WriteFile(path, []byte("synthetic-test-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	old := &tester.Config{}
	old.Tag_Name = "existing-customer-tag"
	cfg := plugins.Configs{Tester: map[string]*tester.Config{"existing": old}}
	text := fmt.Sprintf(`[Microsoft "cloud"]
Ingester-UUID="550e8400-e29b-41d4-a716-446655440000"
Tenant-ID="11111111-1111-1111-1111-111111111111"
Client-ID="22222222-2222-2222-2222-222222222222"
Client-Secret-File=%q
Api="entra-signins"
Lookback="1d12h"
Tag-Schema="consolidated"
Normalization="disabled"
`, path)
	if err := config.LoadConfigBytes(&cfg, []byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Verify(); err != nil {
		t.Fatal(err)
	}
	if cfg.IngesterCount() != 2 || cfg.Microsoft["cloud"].Lookback != 36 {
		t.Fatal("registered config lost a stanza or duration")
	}
	tags, err := cfg.Tags()
	if err != nil || !slices.Contains(tags, "existing-customer-tag") || !slices.Contains(tags, "microsoft-entra-security") {
		t.Fatalf("tags=%v error=%v", tags, err)
	}
	seen := make(map[string]bool)
	for name, builder := range cfg.Builders() {
		seen[name] = true
		if name == "cloud" {
			if builder.UUID().String() != cfg.Microsoft[name].Ingester_UUID {
				t.Fatal("builder UUID changed")
			}
			// Construction validates the production muxer's complete writer and
			// synchronization interfaces without contacting a backend.
			if _, err := builder.Build(&ingest.IngestMuxer{}, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !seen["cloud"] || !seen["existing"] {
		t.Fatal("registered builder missing")
	}
	cfg.Microsoft = map[string]*microsoft.Config{"invalid": nil}
	if cfg.Verify() == nil {
		t.Fatal("nil Microsoft stanza accepted")
	}
}
