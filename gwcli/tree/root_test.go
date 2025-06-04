/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package tree

import (
	"os"
	"path"
	"testing"
)

const (
	password string = "firekeeper"
)

func Test_skimPassfile(t *testing.T) {
	type args struct {
		path       string
		pathExists bool // does a file actually exist at this location
	}
	tests := []struct {
		name             string
		args             args
		wantErr          bool
		expectedPassword string
	}{
		{"valid path", args{path.Join(t.TempDir(), "pf"), true}, false, password},
		{"invalid path", args{path.Join(t.TempDir(), "pf_invalid"), false}, true, ""},
		{"nil path", args{"", false}, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// if the path is supposed to exist, prepopulate the file
			if tt.args.pathExists {
				f, err := os.Create(tt.args.path)
				if err != nil {
					t.Skip(err)
				}
				if _, err := f.WriteString(password); err != nil {
					t.Skip(err)
				}
				if err := f.Sync(); err != nil {
					t.Skip(err)
				}
				if err := f.Close(); err != nil {
					t.Skip(err)
				}
			}
			// attempt to skim file
			pw, err := skimPassFile(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Login() error = %v, wantErr %v", err, tt.wantErr)
			} else if !tt.wantErr && pw != tt.expectedPassword {
				t.Fatalf("original password ('%v') does not equal skimmed password ('%v')", password, pw)
			}

		})

	}

}
