//go:build noci

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
	"strings"
	"testing"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

const (
	username string = "admin"
)

var (
	password string = "changeme"
	server   string
)

func init() {
	server = testsupport.Server()
}

func Test_autoingest(t *testing.T) {
	dir := t.TempDir()

	if err := clilog.Init(path.Join(dir, "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(dir, "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, &password, nil, true); err != nil {
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
		{"0 pairs", args{[]pair{}, ingestFlags{noInteractive: true}},
			0, nil},
		{"1 pair", args{
			[]pair{{path: "hello", tag: "test"}},
			ingestFlags{noInteractive: true}},
			1, map[string]bool{"hello": false}},
		{"1 pair, no tag no default", args{[]pair{{"hello", ""}}, ingestFlags{noInteractive: true}},
			1, map[string]bool{"hello": true}},
		{"2 pairs",
			args{
				[]pair{{"file1", "tag1"}, {"dir/file2", "tag2"}},
				ingestFlags{noInteractive: true},
			},
			2, map[string]bool{"file1": false, "dir/file2": false}},
		{"2 pair, default tag",
			args{
				[]pair{{path: "Ironeye"}, {path: "Duchess"}},
				ingestFlags{noInteractive: true, defaultTag: "Limveld"},
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
			count := autoingest(ch, ingestFlags{noInteractive: true}, []pair{{path: dir, tag: tag}})
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
			count := autoingest(ch, ingestFlags{noInteractive: true, recursive: true}, []pair{{path: dir, tag: tag}})
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

// TestNewIngestActionRun is very similar to autoingest, but includes the manual creation and execution of the cobra command.
func TestNewIngestActionRun(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, &password, nil, true); err != nil {
		t.Fatal(err)
	}
	tDir := t.TempDir()

	tests := []struct {
		name        string
		cliArgs     []string
		setup       func(t *testing.T) // optionally used to perform prior set up (such as file creation)
		checkOutput func(t *testing.T, out string, runErr error, err string)
	}{
		{"noInteractive; no files",
			[]string{"--" + ft.NoInteractive.Name()},
			func(t *testing.T) {},
			func(t *testing.T, out string, runErr error, err string) {
				if out != "" {
					t.Errorf("expected nil stdout, found \"%v\"", out)
				}
				if runErr == nil {
					t.Error("expected run to return an error")
				}
				if err != "" {
					t.Errorf("expected nil stderr, found \"%v\"", out)
				}
			},
		},
		{"noInteractive; 1 file+tag",
			[]string{"--" + ft.NoInteractive.Name(), path.Join(tDir, "raider") + ",Limveld"},
			func(t *testing.T) {
				// create the file to ingest
				if err := os.WriteFile(path.Join(tDir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Fatal(err)
				}
			},
			func(t *testing.T, out string, runErr error, err string) {
				if err != "" {
					t.Errorf("expected nil stderr, found \"%v\"", err)
				}
				if runErr != nil {
					t.Errorf("expected nil runErr, got %v", runErr)
				}
			},
		},
		{"--dir given non-existent path",
			[]string{"--dir", "/nonsense_path"},
			func(t *testing.T) {},
			func(t *testing.T, out string, runErr error, err string) {
				if err == "" && runErr == nil {
					t.Error("expected error, but stderr is empty and runErr is nil")
				}
			},
		},
		{"--dir given file",
			[]string{"--dir", path.Join(tDir, "file")},
			func(t *testing.T) {
				f, err := os.Create(path.Join(tDir, "file"))
				if err != nil {
					t.Fatal("failed to create dummy file")
				}
				f.Close()
			},
			func(t *testing.T, out string, runErr error, err string) {
				if runErr == nil {
					t.Error("expected runErr error")
				}
			},
		},
		{"--dir given with --noInteractive", // --dir is an interactive-only flag.
			[]string{"--dir", "/tmp", "--" + ft.NoInteractive.Name()},
			func(t *testing.T) {},
			func(t *testing.T, out string, runErr error, err string) {
				if err != "" {
					t.Error("found data on stderr: ", err)
				}
				if runErr == nil {
					t.Error("expected runErr error")
				}
			},
		},
		{"invalid source",
			[]string{"--source", "badsrc", "--" + ft.NoInteractive.Name()},
			func(t *testing.T) {},
			func(t *testing.T, out string, runErr error, err string) {
				if runErr == nil {
					t.Error("expected runErr error")
				}
			},
		},
		{"invalid default tag",
			[]string{"--default-tag", "some|tag", "--" + ft.NoInteractive.Name()},
			func(t *testing.T) {},
			func(t *testing.T, out string, runErr error, err string) {
				if runErr == nil {
					t.Error("expected runErr error")
				}
			},
		},
		{"2 files, 1 invalid tag",
			[]string{"--ignore-timestamp", path.Join(tDir, "raider,Limveld"), path.Join(tDir, "recluse,bad|tag")},
			func(t *testing.T) {
				// create the files to ingest
				if err := os.WriteFile(path.Join(tDir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Error(err)
					return
				}
				// this file should *not* be ingested
				if err := os.WriteFile(path.Join(tDir, "recluse"), []byte(randomdata.StringNumber(40, "\n")), 0644); err != nil {
					t.Error(err)
					return
				}
			},
			func(t *testing.T, out string, runErr error, err string) {
				if len(strings.Split(out, "\n")) == 1 {
					t.Errorf("expected expected output to have exactly 1 record, found %v from %v", len(out), out)
				}
				if runErr == nil {
					t.Error("expected runErr error")
				}
				if err == "" {
					t.Error("expected error text, found nil")
				}
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
			tt.setup(t)
			if t.Failed() {
				t.Skip("set up failed")
			}

			// invoke run
			runErr := ap.Action.RunE(ap.Action, tt.cliArgs)

			t.Log("stdout:\n", outBuf)
			t.Log("stderr:\n", errBuf)

			// check output
			tt.checkOutput(t, outBuf.String(), runErr, errBuf.String())
		})
	}

	t.Run("Gravwell JSON", func(t *testing.T) {
		var (
			tag1, tag2, tag3 = "Millhouse", randomdata.Digits(5), randomdata.LastName()
			gwjson           = `{"TS":"2025-06-26T23:26:56.100667099Z","Tag":"` + tag1 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRD","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100640318Z","Tag":"` + tag2 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRB","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100091382Z","Tag":"` + tag3 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRC","Enumerated":null}`
			tdir     = t.TempDir()
			jsonpath = path.Join(tdir, "test.json")
			args     = []string{jsonpath, "--" + ft.NoInteractive.Name()}
		)

		// put the above JSON into a file
		if err := os.WriteFile(jsonpath, []byte(gwjson), 0600); err != nil {
			t.Fatal("failed to write test json to file:", err)
		}

		t.Log("tags: ", tag1, tag2, tag3)

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
		ap.Action.RunE(ap.Action, args)

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
	t.Run("stdin, embedded tag", func(t *testing.T) {
		var (
			tag1, tag2, tag3 = "Millhouse", randomdata.Digits(5), randomdata.LastName()
			gwjson           = `{"TS":"2025-06-26T23:26:56.100667099Z","Tag":"` + tag1 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRD","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100640318Z","Tag":"` + tag2 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRB","Enumerated":null}
{"TS":"2025-06-26T23:26:56.100091382Z","Tag":"` + tag3 + `","SRC":"172.17.0.1","Data":"SGVsbG8gV29ybGRC","Enumerated":null}`
			args = []string{"--stdin"}
		)

		t.Log("tags: ", tag1, tag2, tag3)

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

		// run and feed data into stdin
		origSTDIN, curSTDIN := writeSTDIN(t, gwjson)
		ap.Action.RunE(ap.Action, args)
		curSTDIN.Close()
		os.Stdin = origSTDIN

		t.Log("stdout:\n", outBuf.String())
		t.Log("stderr:\n", errBuf.String())

		// check output
		if errBuf.String() != "" {
			t.Errorf("expected no error output; found %v", errBuf.String())
		}
		if !(strings.Contains(outBuf.String(), "STDIN") &&
			strings.Contains(outBuf.String(), "success") &&
			strings.Contains(outBuf.String(), tag1) &&
			strings.Contains(outBuf.String(), tag2) &&
			strings.Contains(outBuf.String(), tag3)) {
			t.Errorf("bad output. Expected to contain \"STDIN\"")
		}
	})
	t.Run("stdin, default tag", func(t *testing.T) {
		var (
			args = []string{"--stdin", "--default-tag=nuts"}
		)

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

		// run and feed data into stdin
		origSTDIN, curSTDIN := writeSTDIN(t, "some garbage data")
		err := ap.Action.RunE(ap.Action, args)
		if err != nil {
			t.Error("Run returned an error: ", err)
		}
		curSTDIN.Close()
		os.Stdin = origSTDIN

		t.Log("stdout:\n", outBuf.String())
		t.Log("stderr:\n", errBuf.String())

		// check output
		if errBuf.String() != "" {
			t.Errorf("expected no error output; found %v", errBuf.String())
		}
		if !(strings.Contains(outBuf.String(), "STDIN") &&
			strings.Contains(outBuf.String(), "success") &&
			strings.Contains(outBuf.String(), "nuts")) {
			t.Errorf("bad output. Expected to contain \"STDIN\"")
		}
	})
}

// writeSTDIN writes the given data into a file and pipes that file into stdin.
// Remember to reset (curSTDIN.Close(); os.Stdin = origSTDIN) when you are done!
//
// Based on: https://stackoverflow.com/a/46365584
func writeSTDIN(t *testing.T, data string) (origSTDIN *os.File, curSTDIN *os.File) {
	tmpfile, err := os.Create(path.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpfile.WriteString(data); err != nil {
		t.Fatal(err)
	}

	if _, err := tmpfile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	oldStdin := os.Stdin

	os.Stdin = tmpfile
	return oldStdin, tmpfile
}
