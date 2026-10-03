package secretfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(" synthetic-secret \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, "API-Token-File")
	if err != nil {
		t.Fatal(err)
	}
	if got != " synthetic-secret " {
		t.Fatalf("secret=%q", got)
	}
}

func TestReadRejectsMultipleLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, "Token-File"); err == nil {
		t.Fatal("accepted a multiline secret")
	}
}
