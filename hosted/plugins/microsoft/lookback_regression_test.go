package microsoft

import (
	"fmt"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

func TestOfficialDecoderAcceptsMicrosoftLookbackUnits(t *testing.T) {
	for _, tc := range []struct {
		value string
		hours int
	}{{"24h", 24}, {"7d", 168}, {"1d12h", 36}, {"24", 24}} {
		t.Run(tc.value, func(t *testing.T) {
			base := validConfig(t)
			var cfg struct{ Microsoft map[string]*Config }
			raw := fmt.Sprintf("[Microsoft \"test\"]\nIngester-UUID=%q\nTenant-ID=%q\nClient-ID=%q\nClient-Secret-File=%q\nApi=entra-signins\nLookback=%q\n", base.Ingester_UUID, base.Tenant_ID, base.Client_ID, base.Client_Secret_File, tc.value)
			if err := config.LoadConfigBytes(&cfg, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			c := cfg.Microsoft["test"]
			if c == nil {
				t.Fatal("named stanza missing")
			}
			if err := c.Verify(); err != nil {
				t.Fatal(err)
			}
			if int(c.Lookback) != tc.hours {
				t.Fatalf("lookback=%v, want=%d", c.Lookback, tc.hours)
			}
		})
	}
}

func TestOfficialDecoderRejectsInvalidMicrosoftLookback(t *testing.T) {
	for _, value := range []string{"-1h", "1.5h", "30m", "1h1d", "0h", "999999999999999999999d", "29d", "673", "-1"} {
		t.Run(value, func(t *testing.T) {
			base := validConfig(t)
			var cfg struct{ Microsoft map[string]*Config }
			raw := fmt.Sprintf("[Microsoft \"test\"]\nIngester-UUID=%q\nTenant-ID=%q\nClient-ID=%q\nClient-Secret-File=%q\nApi=entra-signins\nLookback=%q\n", base.Ingester_UUID, base.Tenant_ID, base.Client_ID, base.Client_Secret_File, value)
			if err := config.LoadConfigBytes(&cfg, []byte(raw)); err == nil && cfg.Microsoft["test"].Verify() == nil {
				t.Fatal("invalid lookback accepted")
			}
		})
	}
}
