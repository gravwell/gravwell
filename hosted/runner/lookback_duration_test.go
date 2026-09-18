package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v3/hosted/configload"
)

func TestSlackLookbackDurationSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-lookback.conf")
	body := `[Slack "duration"]
Lookback=7d
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var parsed cfgReadType
	if err := configload.LoadFile(&parsed, path); err != nil {
		t.Fatal(err)
	}
	if got := parsed.Slack["duration"].Lookback; got != 7*24 {
		t.Fatalf("Lookback=%d want=%d", got, 7*24)
	}
}
