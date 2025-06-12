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
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

const (
	server   string = "localhost:80"
	username string = "admin"
	password string = "changeme"
)

func Test_autoingest(t *testing.T) {
	testsupport.StartSingletons(t, server, username, password, "", true)

	t.Run("zero files, one tag", func(t *testing.T) {
		fp, tags, src := []string{}, []string{"tag1"}, ""

		wantErr := true

		if err := autoingest(nil, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
	})
	t.Run("single file, zero tags", func(t *testing.T) {
		fp := []string{"somefile.txt"}
		tags := []string{}
		src := ""

		wantErr := true

		if err := autoingest(nil, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
	})
	t.Run("single file, many tags", func(t *testing.T) {
		fp := []string{"somefile.txt"}
		tags := []string{"tag1", "tag2", "tag3"}
		src := ""

		wantErr := true

		if err := autoingest(nil, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
	})

	t.Run("single file, single tag", func(t *testing.T) {
		fn := path.Join(t.TempDir(), "dummyfile")
		// create a dummy file for ingestion
		if err := os.WriteFile(fn, []byte(randomdata.Paragraph()), 0666); err != nil {
			t.Skip("failed to create a dummy file for ingestion")
		}

		fp, tags, src := []string{fn}, []string{"tag1"}, ""
		wantErr, wantOutcomes := false, map[string]bool{fn: false} // filename -> errorExpected?
		ch := make(chan struct {
			string
			error
		})

		if err := autoingest(ch, fp, tags, false, false, src); (err != nil) != wantErr {
			t.Errorf("autoingest() error = %v, wantErr %v", err, wantErr)
		}
		if !wantErr {
			for range len(fp) {
				res := <-ch
				// figure out what we want from this file
				file := res.string
				expectedErr := wantOutcomes[file]
				if (res.error != nil) != expectedErr {
					t.Errorf("incorrect result for '%s':\nexpected error? %v\nactual error: %v", file, expectedErr, res.error)
				}
			}

		}
	})

	/*type args struct {
		res chan<- struct {
			string
			error
		}
		filepaths []string
		tags      []string
		ignoreTS  bool
		localTime bool
		src       string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := autoingest(tt.args.res, tt.args.filepaths, tt.args.tags, tt.args.ignoreTS, tt.args.localTime, tt.args.src); (err != nil) != tt.wantErr {
				t.Errorf("autoingest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}*/
}
