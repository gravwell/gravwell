package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	tc "github.com/testcontainers/testcontainers-go"
)

// ArtifactType is used to control what folder files written are into as they are saved
type ArtifactType = string

const (
	Log           ArtifactType = "/log/"
	Conf          ArtifactType = "/etc/"
	SearchResults ArtifactType = "/search-results/"
	None          ArtifactType = "/"
)

// WriteArtifact will save a file as an artifact for persistence if the `-artifacts` flag is passed to the test command.
func WriteArtifact(t *testing.T, a ArtifactType, name string, content []byte) {
	path := filepath.Clean(t.ArtifactDir() + a + name)
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		t.Fatalf("failed to create directory %s: %v", filepath.Dir(path), err)
	}
	err = os.WriteFile(path, content, 0o644)
	if err != nil {
		t.Fatalf("failed to write artifact file %s: %v", path, err)
	}
}

// WriteQueryResults saves entries as an artifact.
func WriteQueryResults(t *testing.T, name string, ent []types.StringTagEntry) {
	var buf bytes.Buffer
	for _, e := range ent {
		fmt.Fprintf(&buf, "tag: %s, ts: %s, data: %s\n", e.Tag, e.TS.Format(time.RFC3339), e.String())
	}
	WriteArtifact(t, SearchResults, name, buf.Bytes())
}

func WriteLogs(t *testing.T, c *tc.DockerContainer) {
	if c == nil {
		return
	}

	t.Helper()
	ctx := t.Context()
	if ctx.Err() != nil { // create new context if during shutdown
		ctx = context.Background()
	}
	info, err := c.Inspect(ctx)
	if err != nil {
		t.Fatalf("failed to inspect docker container: %v", err)
	}
	r, err := c.Logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	WriteArtifact(t, Log, info.Name+"/logs", b)
}
