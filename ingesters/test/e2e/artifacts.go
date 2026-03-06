package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/client/types"
)

// ArtifactType is used to control what folder files written are into as they are saved
type ArtifactType = string

var (
	Log           ArtifactType = "/log/"
	Conf          ArtifactType = "/etc/"
	SearchResults ArtifactType = "/search-results/"
	None          ArtifactType = "/"
)

func WriteArtifact(t *testing.T, a ArtifactType, name string, content []byte) {
	path := filepath.Clean(t.ArtifactDir() + a + name)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err))
	}
	err = os.WriteFile(path, content, 0o644)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to write artifact file %s: %w", path, err))
	}
}

func WriteQueryResults(t *testing.T, name string, ent []types.StringTagEntry) {
	var buf bytes.Buffer
	for _, e := range ent {
		line := fmt.Sprintf("tag: %s, ts: %s, data: %s", e.Tag, e.TS.Format(time.RFC3339), e.String())
		if _, err := buf.Write([]byte(line + "\n")); err != nil {
			t.Fatal(fmt.Errorf("failed to write query result line %s: %w", line, err))
		}
	}
	WriteArtifact(t, SearchResults, name, buf.Bytes())
}
