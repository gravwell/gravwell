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
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
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
		wantInitialErr   bool            // want autoingest to return an error
		expectedOutcomes map[string]bool // filename -> expectingAnError?
	}{
		{"0 pairs", args{[]pair{}, ingestFlags{script: true}},
			true, nil},
		{"1 pair", args{
			[]pair{{path: "hello", tag: "test"}},
			ingestFlags{script: true}},
			false, map[string]bool{"hello": false}},
		{"1 pair, no tag no default", args{[]pair{{"hello", ""}}, ingestFlags{script: true}},
			false, map[string]bool{"hello": true}},
		{"2 pairs", args{[]pair{{"file1", "tag1"}, {"dir/file2", "tag2"}}, ingestFlags{script: true}},
			false, map[string]bool{"file1": false, "dir/file2": false}},
		/*{"2 pair, default tag"},
		{"2 pairs,1 default 1 specified"},
		{"4 pairs,1 specified, no default"},
		{""},
		{"Gravwell SJON"},

		{"1 file, 5 tags",
			args{
				[]string{"Ironeye"},
				[]string{randomdata.Day(), randomdata.Day(), randomdata.Day(), randomdata.Day(), randomdata.Day()},
				false,
				false,
				""}, true, map[string]bool{"Ironeye": true}},
		{"1 file, 1 tag",
			args{
				[]string{"Duchess"},
				[]string{randomdata.Month()},
				false,
				false,
				"",
			}, false, map[string]bool{"Duchess": false},
		},
		{"3 files, 3 tags",
			args{
				[]string{"Revenant", "Wylder", "Guardian"},
				[]string{randomdata.Month(), randomdata.Month(), randomdata.Month()},
				true,
				true,
				randomdata.IpV6Address(),
			}, false, map[string]bool{"Revenant": false, "Wylder": false, "Guardian": false},
		},*/
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
			if err := autoingest(ch, tt.args.flags, fullPaths); (err != nil) != tt.wantInitialErr {
				t.Errorf("autoingest() error = %v, wantErr %v", err, tt.wantInitialErr)
			}
			if !tt.wantInitialErr {
				// check each file
				for _, pair := range tt.args.pairs {
					if pair.path == "" {
						continue
					}
					res := <-ch

					// the path returned by autoingest will be the full path, including temp, so we need to find the item it is referring to.
					for i := range tt.args.pairs {
						// if we find a match, check the outcome
						if res.string == tt.args.pairs[i].path {
							expectingErr := tt.expectedOutcomes[tt.args.pairs[i].path]
							if (res.error != nil) != expectingErr {
								t.Errorf("incorrect result for '%s':\nexpected error? %v\nactual error: %v", pair.path, expectingErr, res.error)
							}
							continue
						}
					}

				}
			}
		})
	}
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
		want []struct {
			path string
			tag  string
		}
	}{
		{"none", args{[]string{}}, []struct {
			path string
			tag  string
		}{}},
		{"empty strings", args{[]string{"", "", ""}}, []struct {
			path string
			tag  string
		}{}},
		{"all w/ tags", args{[]string{p1 + "," + t1, p2 + "," + t2, p3 + "," + t3}}, []struct {
			path string
			tag  string
		}{{p1, t1}, {p2, t2}, {p3, t3}}},
		{"mixed", args{[]string{p1 + "," + t1, p2}}, []struct {
			path string
			tag  string
		}{{p1, t1}, {path: p2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePairs(tt.args.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePairs() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
					t.Logf("expected nil output, found %v", out)
					return false
				}
				if err == "" {
					t.Log("expected error text, found nil")
					return false
				}
				return true
			},
		},
		{"script; 1 file, 1 tag",
			[]string{"--script", "--tags=Limveld", path.Join(dir, "raider")},
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
					t.Logf("expected nil err output, found %v", err)
					return false
				}
				return true
			},
		},
		{"2 files, 1 tag",
			[]string{"--tags=Limveld", path.Join(dir, "raider"), path.Join(dir, "recluse")},
			func() bool {
				// create the files to ingest
				if err := os.WriteFile(path.Join(dir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Log(err)
					return false
				}
				if err := os.WriteFile(path.Join(dir, "recluse"), []byte(randomdata.StringNumber(40, "\n")), 0644); err != nil {
					t.Log(err)
					return false
				}

				return true
			},
			func(out, err string) bool {
				if err != "" {
					t.Logf("expected nil err output, found %v", err)
					return false
				}
				return true
			},
		},
		{"2 files, 2 tags, with bools",
			[]string{"--tags=Limveld,Night", "--ignore-timestamp", path.Join(dir, "raider"), path.Join(dir, "recluse")},
			func() bool {
				// create the files to ingest
				if err := os.WriteFile(path.Join(dir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Log(err)
					return false
				}
				if err := os.WriteFile(path.Join(dir, "recluse"), []byte(randomdata.StringNumber(40, "\n")), 0644); err != nil {
					t.Log(err)
					return false
				}

				return true
			},
			func(out, err string) bool {
				if err != "" {
					t.Logf("expected nil err output, found %v", err)
					return false
				}
				return true
			},
		},
		{"2 files, 2 (invalid) tags",
			[]string{"--tags=|/,[]", "--ignore-timestamp", path.Join(dir, "raider"), path.Join(dir, "recluse")},
			func() bool {
				// create the files to ingest
				if err := os.WriteFile(path.Join(dir, "raider"), []byte(randomdata.Paragraph()), 0644); err != nil {
					t.Log(err)
					return false
				}
				if err := os.WriteFile(path.Join(dir, "recluse"), []byte(randomdata.StringNumber(40, "\n")), 0644); err != nil {
					t.Log(err)
					return false
				}

				return true
			},
			func(out, err string) bool {
				if out != "" {
					t.Logf("expected nil output, found %v", out)
					return false
				}
				if err == "" {
					t.Log("expected error text, found nil")
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
}
