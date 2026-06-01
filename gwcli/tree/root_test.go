/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package tree

import (
	"maps"
	"os"
	"path"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/pflag"
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
		{"nil path", args{"", false}, false, ""},
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

func Test_checkNoColor(t *testing.T) {
	// clilog needs to be spinning
	if err := clilog.Init(path.Join(t.TempDir(), t.Name()+".log"), "DEBUG"); err != nil {
		t.Fatal("failed to spin up logger: ", err)
	}

	tests := []struct {
		name        string
		args        []string
		envs        map[string]string
		wantNoColor bool
	}{
		{"none", []string{"some", "bare", "arguments"}, nil, false},
		{"NO_COLOR env", []string{"some", "bare", "arguments"}, map[string]string{"NO_COLOR": "1"}, true},
		{"bad env", []string{"some", "bare", "arguments"}, map[string]string{"NONE_COLOR": "1"}, false},
		{"no-color (not a flag)", []string{ft.NoColor.Name(), "bare", "arguments"}, nil, false},
		{"--no-color ", []string{"--" + ft.NoColor.Name(), "bare", "arguments"}, nil, true},
		{"--no-interactive", []string{"--" + ft.NoInteractive.Name()}, nil, true},
		{"all", []string{ft.NoInteractive.Name(), "Something"}, map[string]string{"NO_COLOR": "1", "NONE_COLOR": "1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			// build and parse the required flagset
			fs := new(pflag.FlagSet)
			ft.NoColor.Register(fs)
			ft.NoInteractive.Register(fs)
			fs.Parse(tt.args)

			// prep environment variables

			for key, value := range maps.All(tt.envs) {
				if err := os.Setenv(key, value); err != nil {
					t.Fatalf("failed to set env var: %v", err)
				}
			}

			if gotColorEnabled := isNoColor(fs); gotColorEnabled != tt.wantNoColor {
				t.Errorf("checkNoColor() = %v, want %v", gotColorEnabled, tt.wantNoColor)
			}
		})
	}
}
