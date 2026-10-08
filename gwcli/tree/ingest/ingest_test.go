//go:build ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

// Carries tests that can be run without a backend.

import (
	"errors"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

func Test_parsePairs(t *testing.T) {
	tDir := t.TempDir()
	t.Chdir(tDir)
	var (
		p1 = randomdata.LastName()
		p2 = randomdata.LastName()
		p3 = randomdata.LastName()

		t1 = randomdata.Month()
		t2 = randomdata.Month()
		t3 = randomdata.Month()
	)

	// create files that parsePairs can stat
	if err := os.WriteFile(p1, []byte(randomdata.Alphanumeric(3)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte(randomdata.Alphanumeric(3)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p3, []byte(randomdata.Alphanumeric(3)), 0644); err != nil {
		t.Fatal(err)
	}

	type args struct {
		args []string
	}
	tests := []struct {
		name string
		args args
		want []pair
	}{
		{"none", args{[]string{}}, []pair{}},
		{"empty strings", args{[]string{"", "", ""}}, []pair{}},
		{"all w/ tags", args{[]string{p1 + "," + t1, p2 + "," + t2, p3 + "," + t3}}, []pair{{p1, t1}, {p2, t2}, {p3, t3}}},
		{"mixed", args{[]string{p1 + "," + t1, p2}}, []pair{{p1, t1}, {path: p2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := parsePairs(tt.args.args); err != nil {
				t.Error(err)
			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePairs() = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("fail on first error", func(t *testing.T) {
		_, err := parsePairs([]string{p2, p1 + "," + randomdata.Alphanumeric(9), "DNE,mytag"})
		if err == nil {
			t.Error("a non-existent file did not proc an error!")
		} else if !strings.Contains(err.Error(), "DNE") {
			// ensure that the error points specifically to our fake file
			t.Error("error does not reference our fake file. Error: ", err)
		}
	})
}

func Test_collectPathsForIngestions(t *testing.T) {
	// create a directory structure to test on
	// |-tempdir
	// 		|- fileA
	//		|- fileB
	//		|- fileC
	//		|- childDir
	//			|- fileZ
	//			|- grandchildDir
	//				|- fileX
	dir := t.TempDir()
	if err := os.MkdirAll(path.Join(dir, "childDir", "grandchildDir"), 0700); err != nil {
		t.Fatal("failed to create test directories:", err)
	}
	if err := os.WriteFile(path.Join(dir, "fileA"), []byte("Hello WorldA"), 0622); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(path.Join(dir, "fileB"), []byte("Hello WorldB"), 0622); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(path.Join(dir, "fileC"), []byte("Hello WorldC"), 0622); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(path.Join(dir, "childDir", "fileZ"), []byte("Hello WorldZ"), 0622); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.WriteFile(path.Join(dir, "childDir", "grandchildDir", "fileX"), []byte("Hello WorldX"), 0622); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	type args struct {
		pathToIngest string
		recur        bool
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]bool
		wantErr bool
	}{
		{"shallow", args{dir, false}, map[string]bool{
			path.Join(dir, "fileA"): true,
			path.Join(dir, "fileB"): true,
			path.Join(dir, "fileC"): true,
		}, false},
		{"shallow subdir", args{path.Join(dir, "childDir"), false}, map[string]bool{
			path.Join(dir, "childDir", "fileZ"): true,
		}, false},
		{"single file", args{path.Join(dir, "fileA"), false}, map[string]bool{
			path.Join(dir, "fileA"): true,
		}, false},
		{"recursive", args{dir, true}, map[string]bool{
			path.Join(dir, "fileA"):                              true,
			path.Join(dir, "fileB"):                              true,
			path.Join(dir, "fileC"):                              true,
			path.Join(dir, "childDir", "fileZ"):                  true,
			path.Join(dir, "childDir", "grandchildDir", "fileX"): true,
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectPathsForIngestions(tt.args.pathToIngest, tt.args.recur)
			if (err != nil) != tt.wantErr {
				t.Errorf("collectPathsForIngestions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("incorrect path counts.%v", testsupport.ExpectedActual(len(tt.want), len(got)))
			}
			for path := range got {
				if _, exists := tt.want[path]; !exists {
					t.Errorf("extraneous path %v in actual", path)
				}
			}
		})
	}
}

// autoingest tests use only empty files, which are rejected before any request is made, so no backend is required.
func Test_autoingest_noBackend(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "test.log"), "DEBUG"); err != nil {
		t.Fatal(err)
	}
	// reads results until ch is closed.
	// places a timer on the operation to catch hangs early
	collectResults := func(t *testing.T, ch <-chan ingestResult) map[string]error {
		t.Helper()
		got := map[string]error{}
		if ch == nil {
			return got
		}
		timeout := time.After(5 * time.Second)
		for {
			select {
			case res, ok := <-ch:
				if !ok {
					return got
				}
				got[res.string] = res.error
			case <-timeout:
				t.Fatalf("timed out waiting for the channel to close; received %d results", len(got))
			}
		}
	}
	mkEmptyFiles := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, n := range names {
			if err := os.WriteFile(path.Join(dir, n), nil, 0666); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("no pairs", func(t *testing.T) {
		if ch := autoingest(ingestFlags{}, nil); ch != nil {
			t.Error("expected a nil channel when given no pairs")
		}
	})
	t.Run("empty directory should have no results", func(t *testing.T) {
		if ch := autoingest(ingestFlags{}, []pair{{path: t.TempDir(), tag: "t"}}); cap(ch) != 0 {
			t.Errorf("expected zero results for empty directory, got %d", cap(ch))
		}
	})
	t.Run("directory of only subdirectories is empty without --recursive", func(t *testing.T) {
		tDir := t.TempDir()
		if err := os.Mkdir(path.Join(tDir, "sub"), 0777); err != nil {
			t.Fatal(err)
		}
		mkEmptyFiles(t, path.Join(tDir, "sub"), "a")
		if ch := autoingest(ingestFlags{}, []pair{{path: tDir, tag: "t"}}); cap(ch) != 0 {
			t.Errorf("expected zero results sans --recursive, got %d", cap(ch))
		}
		if results := collectResults(t, autoingest(ingestFlags{recursive: true}, []pair{{path: tDir, tag: "t"}})); len(results) != 1 {
			t.Errorf("expected 1 result with --recursive, got %#v", results)
		}
	})
	t.Run("files reachable via multiple pairs are counted once", func(t *testing.T) {
		dir := t.TempDir()
		mkEmptyFiles(t, dir, "a", "b")
		pairs := []pair{{path: dir, tag: "t"}, {path: path.Join(dir, "a"), tag: "t"}, {path: path.Join(dir, "a"), tag: "t"}}
		results := collectResults(t, autoingest(ingestFlags{}, pairs))
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %#v", results)
		}
		for _, name := range []string{"a", "b"} {
			if err, found := results[path.Join(dir, name)]; !found {
				t.Errorf("no result for %v", name)
			} else if !errors.Is(err, errEmptyFile) {
				t.Errorf("expected errEmptyFile for %v, got %v", name, err)
			}
		}
	})
	t.Run("collection errors are reported, not dropped", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("cannot make a directory unreadable as root")
		}
		dir := t.TempDir()
		mkEmptyFiles(t, dir, "a")
		locked := path.Join(dir, "locked")
		if err := os.Mkdir(locked, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0777) })

		results := collectResults(t, autoingest(ingestFlags{}, []pair{{path: dir, tag: "t"}, {path: locked, tag: "t"}}))
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if err := results[locked]; err == nil {
			t.Error("expected an error for the unreadable directory")
		}
		if err := results[path.Join(dir, "a")]; !errors.Is(err, errEmptyFile) {
			t.Errorf("expected errEmptyFile for a, got %v", err)
		}
	})
	t.Run("channel buffers all elements and closes itself", func(t *testing.T) {
		dir := t.TempDir()
		mkEmptyFiles(t, dir, "a", "b", "c")
		ch := autoingest(ingestFlags{}, []pair{{path: dir, tag: "t"}})
		// do not read until everything has finished; the buffer must hold every result and the channel must close
		deadline := time.After(5 * time.Second)
		for len(ch) < cap(ch) {
			select {
			case <-deadline:
				t.Fatalf("only %d of %d results were buffered", len(ch), cap(ch))
			default:
				time.Sleep(time.Millisecond)
			}
		}
		if got := collectResults(t, ch); len(got) != 3 {
			t.Errorf("expected 3 results, got %d", len(got))
		}
	})
}

// Covers a file that is not Gravwell JSON; determineTag opens it to check, and must close it again.
func Test_determineTag_nonGWJSON(t *testing.T) {
	f := path.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(f, []byte("hello world"), 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := determineTag(f, "", ""); !errors.Is(err, errNoTagSpecified) {
		t.Errorf("expected errNoTagSpecified, got %v", err)
	}
	if tag, err := determineTag(f, "", "dflt"); err != nil || tag != "dflt" {
		t.Errorf("expected the default tag, got %q (err: %v)", tag, err)
	}
}
