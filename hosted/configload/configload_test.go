package configload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testSource struct {
	Lookback         int
	Initial_Lookback string
}

type testConfig struct {
	Source map[string]*testSource
}

func TestLoadFileLookbackDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lookback.conf")
	contents := `[Source "duration"]
Lookback="1d12h" # explicit duration
Initial-Lookback=7d

[Source "legacy"]
Lookback=24
Initial-Lookback="24"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var got testConfig
	if err := LoadFile(&got, path); err != nil {
		t.Fatal(err)
	}
	if source := got.Source["duration"]; source == nil || source.Lookback != 36 || source.Initial_Lookback != "168h" {
		t.Fatalf("duration source=%+v", source)
	}
	if source := got.Source["legacy"]; source == nil || source.Lookback != 24 || source.Initial_Lookback != "24h" {
		t.Fatalf("legacy source=%+v", source)
	}
}

func TestLoadOverlaysUsesLexicalOrder(t *testing.T) {
	directory := t.TempDir()
	for name, contents := range map[string]string{
		"10-first.conf": `[Source "test"]` + "\n" + `Lookback=24h` + "\n",
		"20-last.conf":  `[Source "test"]` + "\n" + `Lookback=2d` + "\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var got testConfig
	if err := LoadOverlays(&got, directory); err != nil {
		t.Fatal(err)
	}
	if source := got.Source["test"]; source == nil || source.Lookback != 48 {
		t.Fatalf("overlay source=%+v", source)
	}
}

func TestNormalizeRejectsUnsupportedUnits(t *testing.T) {
	_, err := Normalize([]byte("[Source \"test\"]\nLookback=1w\n"))
	if err == nil || !strings.Contains(err.Error(), "use whole hours or days") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeRejectsNegativeLookback(t *testing.T) {
	if _, err := Normalize([]byte("[Source \"test\"]\nLookback=-5\n")); err == nil || !strings.Contains(err.Error(), "negative value") {
		t.Fatalf("Lookback: unexpected error: %v", err)
	}
	if _, err := Normalize([]byte("[Source \"test\"]\nInitial-Lookback=-5\n")); err == nil || !strings.Contains(err.Error(), "negative value") {
		t.Fatalf("Initial-Lookback: unexpected error: %v", err)
	}
}

func TestLoadOverlaysAllowsMissingDirectory(t *testing.T) {
	var got testConfig
	if err := LoadOverlays(&got, filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
}
