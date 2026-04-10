package e2e

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	tc "github.com/testcontainers/testcontainers-go"
)

func files(ctx context.Context, con *tc.DockerContainer, paths []string) (map[string][]byte, error) {
	f := make(map[string][]byte)
	for _, path := range paths {
		file, err := con.CopyFileFromContainer(ctx, path)
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		written, err := io.Copy(&buf, file)
		file.Close()
		if err != nil {
			return nil, err
		}
		if written == 0 {
			buf.WriteString("no content")
		}
		f[path] = buf.Bytes()
	}
	return f, nil
}

// SaveTestFiles extracts the specified files from a given container and writes them as artifacts.
// Calls should be split for each different ArtifactType.
func SaveTestFiles(t *testing.T, con *tc.DockerContainer, prefix ArtifactType, paths []string) {
	t.Helper()
	if con == nil {
		return
	}
	ctx := context.Background() // Don't use t.Context() in case this is during test cleanup
	contents, err := files(ctx, con, paths)
	if err != nil {
		t.Fatalf("error getting file contents: %v", err)
	}
	res, err := con.Inspect(ctx)
	if err != nil {
		t.Fatalf("error inspecting container name: %v", err)
	}
	for _, file := range paths {
		name := res.Name + "/" + filepath.Base(file)
		content, exists := contents[file]
		if !exists {
			content = []byte("did not exist")
		}
		WriteArtifact(t, prefix, name, content)
	}
}

// NewConfigReader takes a config path relative to the current test package and uses text/template to render the config.
// Useful when needing dynamic configuration in a config file.
func NewConfigReader(t *testing.T, config string, data any) io.Reader {
	path := filepath.Clean(config)
	base := filepath.Base(path)
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config template file %s: %v", path, err)
	}

	temp, err := template.New(base).Parse(string(file))
	if err != nil {
		t.Fatalf("failed to parse config template file %s: %v", path, err)
	}

	var buf bytes.Buffer
	if err = temp.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute config template: %v", err)
	}

	content := buf.Bytes()
	WriteArtifact(t, Conf, base, content)
	return bytes.NewReader(content)
}
