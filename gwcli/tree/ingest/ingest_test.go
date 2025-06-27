//go:build !ci
// +build !ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"bytes"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

const (
	server   string = "localhost:80"
	username string = "admin"
	password string = "changeme"
)

func Test_autoingest(t *testing.T) {
	dir := t.TempDir()

	if err := clilog.Init(path.Join(dir, "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(dir, "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, password, "", true); err != nil {
		t.Fatal(err)
	}

	type args struct {
		pairs []pair // creates files at the given paths in a temp directory
		flags ingestFlags
	}
	tests := []struct {
		name             string
		args             args
		wantCount        uint
		expectedOutcomes map[string]bool // filename -> expectingAnError?
	}{
		{"0 pairs", args{[]pair{}, ingestFlags{script: true}},
			0, nil},
		{"1 pair", args{
			[]pair{{path: "hello", tag: "test"}},
			ingestFlags{script: true}},
			1, map[string]bool{"hello": false}},
		{"1 pair, no tag no default", args{[]pair{{"hello", ""}}, ingestFlags{script: true}},
			1, map[string]bool{"hello": true}},
		{"2 pairs",
			args{
				[]pair{{"file1", "tag1"}, {"dir/file2", "tag2"}},
				ingestFlags{script: true},
			},
			2, map[string]bool{"file1": false, "dir/file2": false}},
		{"2 pair, default tag",
			args{
				[]pair{{path: "Ironeye"}, {path: "Duchess"}},
				ingestFlags{script: true, defaultTag: "Limveld"},
			}, 2, map[string]bool{"Ironeye": false, "Duchess": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create each file we expect to succeed
			for f, expectingErr := range tt.expectedOutcomes {
				if f == "" || expectingErr {
					continue
				}

				fullPath := path.Join(dir, f)

				// create directories, if necessary
				pathParentDir, _ := path.Split(fullPath)
				if pathParentDir != "" {
					if err := os.MkdirAll(pathParentDir, 0666); err != nil {
						t.Skipf("failed to mkdir directory path '%v': %v", pathParentDir, err)
					}
				}
				t.Logf("created path '%v'", fullPath)

				if err := os.WriteFile(fullPath, []byte(randomdata.Paragraph()), 0666); err != nil {
					t.Skipf("failed to create a file '%v' for ingestion", f)
				}
			}

			// prefix each path with the temp directory
			fullPaths := make([]pair, len(tt.args.pairs))
			for i := range tt.args.pairs {
				fullPaths[i].path = path.Join(dir, tt.args.pairs[i].path)
				fullPaths[i].tag = tt.args.pairs[i].tag
			}

			ch := make(chan struct {
				string
				error
			})

			// execute autoingest and await results on the channel
			count := autoingest(ch, tt.args.flags, fullPaths)
			if count != tt.wantCount {
				t.Errorf("incorrect ingestion count.%v", testsupport.ExpectedActual(count, tt.wantCount))
			}
			// check each file
			for range count {
				res := <-ch

				// strip the testing directory off the path
				if after, found := strings.CutPrefix(res.string, dir+"/"); !found {
					t.Fatalf("expected all paths to be prefixed by the temp directory. Actual: %v", res.string)
				} else {
					res.string = after
				}

				// find the outcome we are expecting
				var found bool
				for i := range tt.args.pairs {
					// if we find a match, check the outcome
					if res.string == tt.args.pairs[i].path {
						found = true
						expectingErr := tt.expectedOutcomes[tt.args.pairs[i].path]
						if (res.error != nil) != expectingErr {
							t.Errorf("incorrect result for '%s':\nexpected error? %v\nactual error: %v", tt.args.pairs[i].path, expectingErr, res.error)
						}
					}
				}
				// if we made it this far without finding a match, something has gone terribly wrong
				if !found {
					t.Errorf("failed to find file %v in argument pairs", res.string)
				}
			}
		})
	}

	// run directory ingestion tests
	t.Run("directory ingestion", func(t *testing.T) {
		dir := t.TempDir()

		// build a directory to ingest
		// |-tempdir
		// 		|- fileA
		//		|- fileB
		//		|- fileC
		//		|- childDir
		//			|- fileZ
		//			|- grandchildDir
		//				|- fileX
		if err := os.WriteFile(path.Join(dir, "fileA"), []byte("Hello WorldA"), 0666); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(path.Join(dir, "fileB"), []byte("Hello WorldB"), 0666); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.WriteFile(path.Join(dir, "fileC"), []byte("Hello WorldC"), 0666); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.Mkdir(path.Join(dir, "childDir"), 0777); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(path.Join(dir, "childDir", "fileZ"), []byte("Hello WorldZ"), 0666); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := os.Mkdir(path.Join(dir, "childDir", "grandchildDir"), 0777); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(path.Join(dir, "childDir", "grandchildDir", "fileX"), []byte("Hello WorldX"), 0666); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		ch := make(chan struct {
			string
			error
		})

		t.Run("shallow", func(t *testing.T) {
			tag := "shallow" + randomdata.Alphanumeric(10)

			// execute autoingest and await results on the channel
			count := autoingest(ch, ingestFlags{script: true}, []pair{{path: dir, tag: tag}})
			if count != 3 {
				t.Errorf("incorrect ingestion count.%v", testsupport.ExpectedActual(3, count))
			}

			// collect responses
			// shallow should ONLY match filesA/B/C
			for range count {
				res := <-ch
				switch path.Base(res.string) {
				case "fileA", "fileB", "fileC":
					if res.error != nil {
						t.Errorf("failed to ingest %v: %v", res.string, res.error)
					}
				default: // a file that should not have been ingested was.
					t.Errorf("unexpected ingestion of file %v. Result: %v", res.string, res.error)
				}
			}

			if !verifyTagExists(t, tag) {
				t.Errorf("failed to find tag %v after ingesting files under it", tag)
			}
		})

		t.Run("recursive", func(t *testing.T) {
			tag := "recursive" + randomdata.Alphanumeric(10)

			// execute autoingest and await results on the channel
			count := autoingest(ch, ingestFlags{script: true, recursive: true}, []pair{{path: dir, tag: tag}})
			if count != 5 {
				t.Errorf("incorrect ingestion count.%v", testsupport.ExpectedActual(5, count))
			}

			// collect responses
			// shallow should match all five files
			for range count {
				res := <-ch
				switch path.Base(res.string) {
				case "fileA", "fileB", "fileC", "fileZ", "fileX":
					if res.error != nil {
						t.Errorf("failed to ingest %v: %v", res.string, res.error)
					}
				default: // a file that should not have been ingested was.
					t.Errorf("unexpected ingestion of file %v. Result: %v", res.string, res.error)
				}

			}

			if !verifyTagExists(t, tag) {
				t.Errorf("failed to find tag %v after ingesting files under it", tag)
			}
		})

	})
}

// checks that the given tag exists on the Gravwell backend.
// NOTE(rlandau): the lag time may need to be increased, as it appears to take a variable amount of time for ingested files to "commit".
// The tag will not be returned by GetTags until files under it have been committed.
func verifyTagExists(t *testing.T, tag string) bool {
	t.Helper()
	time.Sleep(10 * time.Second) // tags can take a few moments to show up
	tags, err := connection.Client.GetTags()
	if err != nil {
		t.Error(err)
		return false
	}
	for _, serverTag := range tags {
		if serverTag == tag {
			return true
		}
	}
	return false
}

func Test_parsePairs(t *testing.T) {
	var (
		p1 = randomdata.LastName()
		p2 = randomdata.LastName()
		p3 = randomdata.LastName()

		t1 = randomdata.Month()
		t2 = randomdata.Month()
		t3 = randomdata.Month()
	)

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
			if got := parsePairs(tt.args.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePairs() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNewIngestActionRun is very similar to autoingest, but includes the manual creation and execution of the cobra command.
func TestNewIngestActionRun(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, password, "", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	tests := []struct {
		name        string
		cliArgs     []string
		setup       func() (success bool)                // optionally used to perform prior set up (such as file creation)
		checkOutput func(out, err string) (success bool) // used to check stdout and stderr for expected values
	}{
		{"script; no files",
			[]string{"--script"},
			func() bool { return true },
			func(out, err string) bool {
				if out != "" {
					t.Logf("expected nil output, found \"%v\"", out)
					return false
				}
				if err == "" {
					t.Log("expected error text, found nil")
					return false
				}
				return true
			},
		},
		{"script; 1 file+tag",
			[]string{"--script", path.Join(dir, "raider") + ",Limveld"},
			func() bool {
				// create the file to ingest
				if err := os.WriteFile(path.Join(dir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Log(err)
					return false
				}

				return true
			},
			func(out, err string) bool {
				if err != "" {
					t.Logf("expected nil err output, found \"%v\"", err)
					return false
				}
				return true
			},
		},
		{"--dir given non-existent path",
			[]string{"--dir", "/nonsense_path"},
			func() (success bool) { return true },
			func(out, err string) (success bool) { return err != "" },
		},
		{"--dir given file",
			[]string{"--dir", "/nonsense_path"},
			func() (success bool) { return true },
			func(out, err string) (success bool) { return err != "" },
		},
		{"--dir given with --script",
			[]string{"--dir", "/tmp", "--script"},
			func() (success bool) { return true },
			func(out, err string) (success bool) { return err != "" },
		},
		{"invalid source",
			[]string{"--source", "badsrc", "--script"},
			func() (success bool) { return true },
			func(out, err string) (success bool) { return err != "" },
		},
		{"invalid default tag",
			[]string{"--default-tag", "some|tag", "--script"},
			func() (success bool) { return true },
			func(out, err string) (success bool) { return err != "" },
		},
		{"2 files, 1 invalid tag",
			[]string{"--ignore-timestamp", path.Join(dir, "raider,Limveld"), path.Join(dir, "recluse,bad|tag")},
			func() bool {
				// create the files to ingest
				if err := os.WriteFile(path.Join(dir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Log(err)
					return false
				}
				// this file should *not* be ingested
				if err := os.WriteFile(path.Join(dir, "recluse"), []byte(randomdata.StringNumber(40, "\n")), 0644); err != nil {
					t.Log(err)
					return false
				}

				return true
			},
			func(out, err string) bool {
				if len(strings.Split(out, "\n")) == 1 {
					t.Logf("expected expected output to have exactly 1 record, found %v from %v", len(out), out)
					return false
				}
				if err == "" {
					t.Log("expected error text, found nil")
					return false
				}
				return true
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// create the action
			ap := NewIngestAction()

			// perform root's actions
			uniques.AttachPersistentFlags(ap.Action)
			if err := ap.Action.Flags().Parse(tt.cliArgs); err != nil {
				t.Fatal(err)
			}

			// capture output
			outBuf := &bytes.Buffer{}
			ap.Action.SetOut(outBuf)
			errBuf := &bytes.Buffer{}
			ap.Action.SetErr(errBuf)

			// run set up
			if !tt.setup() {
				t.Skip("set up failed")
			}

			// invoke run
			ap.Action.Run(ap.Action, tt.cliArgs)

			t.Log("stdout:\n", outBuf)
			t.Log("stderr:\n", errBuf)

			// check output
			if success := tt.checkOutput(outBuf.String(), errBuf.String()); !success {
				t.Fatal("bad output")
			}
		})
	}

	t.Run("Gravwell JSON", func(t *testing.T) {
		var (
			tag1, tag2, tag3 = randomdata.City(), randomdata.Digits(5), randomdata.Title(0)
			gwjson           = `{"TS":"2025-06-26T23:26:56.100667099Z","Tag":"` + tag1 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRD","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100640318Z","Tag":"` + tag2 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRB","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100091382Z","Tag":"` + tag3 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRC","Enumerated":null}`
			tdir     = t.TempDir()
			jsonpath = path.Join(tdir, "test.json")
			args     = []string{jsonpath, "--script"}
		)

		// put the above JSON into a file
		if err := os.WriteFile(jsonpath, []byte(gwjson), 0600); err != nil {
			t.Fatal("failed to write test json to file:", err)
		}

		// create the action
		ap := NewIngestAction()

		// perform root's actions
		uniques.AttachPersistentFlags(ap.Action)
		if err := ap.Action.Flags().Parse(args); err != nil {
			t.Fatal(err)
		}

		// capture output
		outBuf := &bytes.Buffer{}
		ap.Action.SetOut(outBuf)
		errBuf := &bytes.Buffer{}
		ap.Action.SetErr(errBuf)

		// attempt to ingest the file
		// invoke run
		ap.Action.Run(ap.Action, args)

		t.Log("stdout:\n", outBuf.String())
		t.Log("stderr:\n", errBuf.String())

		// check output
		if errBuf.String() != "" {
			t.Errorf("expected no error output; found %v", errBuf.String())
		}
		if !strings.Contains(outBuf.String(), jsonpath) {
			t.Errorf("bad output. Expected to contain the path %v", jsonpath)
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
