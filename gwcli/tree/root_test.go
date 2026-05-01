//go:build ci

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
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/cfgdir"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
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

func TestGatherCredentials(t *testing.T) {
	tDir := t.TempDir()
	tests := []struct {
		name              string // description of this test case
		args              []string
		setupFunc         func(t *testing.T)
		wantUsername      string
		wantPasswordNil   bool
		wantPassword      string
		wantAPIKeyNil     bool
		wantAPIKey        string
		wantNoInteractive bool
		wantErr           bool
	}{
		{"zilch should return no data and no error",
			nil, nil,
			"",
			true, "",
			true, "",
			false,
			false,
		},
		{"only username",
			[]string{"--username=naru"}, nil,
			"naru", true, "",
			true, "",
			false,
			false,
		},
		{"apikey",
			[]string{"--api", "mykey", "--no-interactive"}, nil,
			"",
			true, "",
			false, "mykey",
			true,
			false,
		},
		{"username, passfile, eapikey",
			[]string{"--eapi", "-p", path.Join(tDir, "pass.txt"), "-u=user"},
			func(t *testing.T) {
				// set apikey in environment
				t.Setenv(cfgdir.EnvKeyAPI, "mykey2")
				// create and fill password file
				if f, err := os.Create(path.Join(tDir, "pass.txt")); err != nil {
					t.Fatal(err)
				} else if _, err := f.WriteString("mypass"); err != nil {
					t.Fatal(err)
				}
			},
			"user",
			false, "mypass",
			false, "mykey2",
			false,
			false,
		},
		{"epass",
			[]string{"-u=user"},
			func(t *testing.T) {
				// set apikey in environment
				t.Setenv(cfgdir.EnvKeyPassword, "enviropass")
			},
			"user",
			false, "enviropass",
			true, "",
			false,
			false,
		},
		{"epass but no username supplied", // shouldn't error, but also shouldn't pick up the password
			[]string{""},
			func(t *testing.T) {
				// set apikey in environment
				t.Setenv(cfgdir.EnvKeyPassword, "enviropass")
			},
			"",
			true, "",
			true, "",
			false,
			false,
		},
		{"passfile but no username supplied",
			[]string{"-p=" + path.Join(tDir, "pass.txt")}, // shouldn't matter if this file actually exists
			nil,
			"",
			true, "",
			true, "",
			false,
			true,
		},
		{"passfile DNE",
			[]string{"-p=" + path.Join(tDir, "dne.txt")}, // shouldn't matter if this file actually exists
			nil,
			"",
			true, "",
			true, "",
			false,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// prepare the environment
			if tt.setupFunc != nil {
				tt.setupFunc(t)
			}

			cmd := &cobra.Command{}
			uniques.AttachPersistentFlags(cmd)
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatal(err)
			}
			gotUsername, gotPassword, gotAPIKey, gotNoInteractive, gotErr := GatherCredentials(cmd.Flags())
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("GatherCredentials() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("GatherCredentials() succeeded unexpectedly")
			}

			if gotUsername != tt.wantUsername {
				t.Error("incorrect username", testsupport.ExpectedActual(tt.wantUsername, gotUsername))
			}
			if tt.wantPasswordNil && gotPassword != nil {
				t.Error("expected nil password, got", gotPassword)
			} else if !tt.wantPasswordNil && gotPassword == nil {
				t.Error("did not expect nil password")
			} else if !tt.wantPasswordNil && (tt.wantPassword != *gotPassword) {
				t.Error("incorrect password", testsupport.ExpectedActual(tt.wantPassword, *gotPassword))
			}
			if tt.wantAPIKeyNil && gotAPIKey != nil {
				t.Error("expected nil api key, got", gotAPIKey)
			} else if !tt.wantAPIKeyNil && gotAPIKey == nil {
				t.Error("did not expect nil api key")
			} else if !tt.wantAPIKeyNil && (tt.wantAPIKey != *gotAPIKey) {
				t.Error("incorrect api key", testsupport.ExpectedActual(tt.wantPassword, *gotAPIKey))
			}

			if tt.wantNoInteractive != gotNoInteractive {
				t.Error("incorrect no interactive", testsupport.ExpectedActual(tt.wantNoInteractive, gotNoInteractive))
			}
		})
	}
}
