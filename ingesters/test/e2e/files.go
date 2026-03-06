package e2e

import (
	"bytes"
	"context"
	"fmt"
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
			return nil, err
		}
		var buf bytes.Buffer
		written, err := io.Copy(&buf, file)
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

func SaveTestFiles(t *testing.T, con *tc.DockerContainer, prefix ArtifactType, paths []string) {
	ctx := context.Background()
	contents, err := files(ctx, con, paths)
	if err != nil {
		t.Fatal(fmt.Errorf("error getting file contents: %v", err))
	}
	res, err := con.Inspect(ctx)
	if err != nil {
		t.Fatal(fmt.Errorf("error inspecting container name: %v", err))
	}
	for p, file := range contents {
		name := res.Name + "/" + filepath.Base(p)
		WriteArtifact(t, prefix, name, file)
	}
}

func NewConfigReader(t *testing.T, config string, data any) io.Reader {
	path := filepath.Clean(config)
	base := filepath.Base(path)
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to read config template file %s: %w", path, err))
	}

	temp, err := template.New(base).Parse(string(file))
	if err != nil {
		t.Fatal(fmt.Errorf("failed to parse config template file %s: %w", path, err))
	}

	var buf bytes.Buffer
	if err = temp.Execute(&buf, data); err != nil {
		t.Fatal(fmt.Errorf("failed to execute config template: %w", err))
	}

	content := buf.Bytes()
	WriteArtifact(t, Conf, base, content)
	return bytes.NewReader(content)
}
