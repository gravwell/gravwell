/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ingest

import (
	"os"
	"path"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
)

const (
	server   string = "localhost:80"
	username string = "admin"
	password string = "changeme"
)

func Test_autoingest(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, password, "", true); err != nil {
		t.Fatal(err)
	}

	type args struct {
		filenames []string // all files are created in the temp directory
		tags      []string
		ignoreTS  bool
		localTime bool
		src       string
	}
	tests := []struct {
		name             string
		args             args
		wantInitialErr   bool            // want autoingest to return an error
		expectedOutcomes map[string]bool // filename -> expectingAnError?
	}{
		{"0 files, 1 tag", args{nil, []string{randomdata.LastName()}, false, false, ""}, true, nil},
		{"1 file, 0 tags", args{[]string{randomdata.LastName()}, nil, false, false, ""}, true, nil},
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPaths := make([]string, 0)

			// create each file we expect to succeed
			for f, expectingErr := range tt.expectedOutcomes {
				if f == "" {
					continue
				}
				p := path.Join(t.TempDir(), f)
				fullPaths = append(fullPaths, p)

				if expectingErr {
					continue
				}

				if err := os.WriteFile(p, []byte(randomdata.Paragraph()), 0666); err != nil {
					t.Skipf("failed to create a file '%v' for ingestion", f)
				}
			}

			ch := make(chan struct {
				string
				error
			})

			if err := autoingest(
				ch,
				fullPaths,
				tt.args.tags,
				tt.args.ignoreTS,
				tt.args.localTime, tt.args.src); (err != nil) != tt.wantInitialErr {
				t.Errorf("autoingest() error = %v, wantErr %v", err, tt.wantInitialErr)
			}
			if !tt.wantInitialErr {
				for _, f := range tt.args.filenames {
					if f == "" {
						continue
					}
					res := <-ch
					// figure out what we want from this file
					file := res.string
					expectingErr := tt.expectedOutcomes[file]
					if (res.error != nil) != expectingErr {
						t.Errorf("incorrect result for '%s':\nexpected error? %v\nactual error: %v", file, expectingErr, res.error)
					}
				}
			}
		})
	}
}
