/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadEnvVarList(t *testing.T) {
	const missing = "\x00missing" // file content marker: point _FILE at a path that does not exist
	tests := []struct {
		name    string
		cfg     []string
		env     *string // nil leaves the plain variable unset
		file    *string // nil leaves _FILE unset
		want    []string
		wantErr error  // checked with errors.Is
		errHas  string // substring the error must contain, TEST_LIST stands for the variable name
	}{
		{name: "nothing set"},
		{name: "plain single", env: new("10.0.0.1"), want: []string{"10.0.0.1"}},
		{name: "plain commas", env: new(" 10.0.0.1, ,192.168.0.0/16 ,"), want: []string{"10.0.0.1", "192.168.0.0/16"}},
		{name: "plain newlines", env: new("10.0.0.1\n10.0.0.2\r\n10.0.0.3"), want: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{name: "plain only commas", env: new(","), wantErr: ErrEmptyEnvList, errHas: "TEST_LIST"},
		{name: "plain only separators and blanks", env: new(" , \n ,\r\n"), wantErr: ErrEmptyEnvList, errHas: "TEST_LIST"},
		{name: "plain whitespace", env: new(" "), wantErr: ErrEmptyEnvList},
		{name: "plain empty is unset", env: new("")},
		{name: "plain empty does not fall through to file", env: new(""), file: new("10.0.0.1\n")},
		{name: "plain wins over file", env: new("10.0.0.1"), file: new("10.0.0.2\n"), want: []string{"10.0.0.1"}},
		{name: "plain separators with good file is still an error", env: new(","), file: new("10.0.0.2\n"), wantErr: ErrEmptyEnvList},
		{name: "config wins over plain", cfg: []string{"10.0.0.9"}, env: new("10.0.0.1"), want: []string{"10.0.0.9"}},
		{name: "config wins over file", cfg: []string{"10.0.0.9"}, file: new("10.0.0.2"), want: []string{"10.0.0.9"}},
		{name: "config wins over bad values", cfg: []string{"10.0.0.9"}, env: new(","), want: []string{"10.0.0.9"}},
		{name: "file single no newline", file: new("10.0.0.1"), want: []string{"10.0.0.1"}},
		{name: "file one per line", file: new("10.0.0.1\n10.0.0.2\n192.168.0.0/16"), want: []string{"10.0.0.1", "10.0.0.2", "192.168.0.0/16"}},
		{name: "file trailing newline", file: new("10.0.0.1\n10.0.0.2\n"), want: []string{"10.0.0.1", "10.0.0.2"}},
		{name: "file crlf", file: new("10.0.0.1\r\n10.0.0.2\r\n"), want: []string{"10.0.0.1", "10.0.0.2"}},
		{name: "file commas and newlines", file: new("10.0.0.1, 10.0.0.2\n\n  10.0.0.3 ,\n10.0.0.4"), want: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}},
		{name: "file hash is not a comment", file: new("# proxies\n10.0.0.1\n"), want: []string{"# proxies", "10.0.0.1"}},
		{name: "file empty", file: new(""), wantErr: ErrEmptyEnvFile, errHas: "TEST_LIST_FILE"},
		{name: "file whitespace only", file: new(" \n\r\n\t"), wantErr: ErrEmptyEnvFile},
		{name: "file only commas", file: new(",,,"), wantErr: ErrEmptyEnvList, errHas: "TEST_LIST_FILE"},
		{name: "file only separators", file: new(" ,\n , \r\n,"), wantErr: ErrEmptyEnvList},
		{name: "file missing", file: new(missing), wantErr: os.ErrNotExist, errHas: "TEST_LIST_FILE"},
		{name: "file too large", file: new(strings.Repeat("10.0.0.1\n", int(maxEnvListFileSize)/9+1)), errHas: "larger than"},
		{name: "file at size cap", file: new(strings.Repeat("a", int(maxEnvListFileSize))), want: []string{strings.Repeat("a", int(maxEnvListFileSize))}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// a unique name per case, since t.Setenv cannot unset a variable
			name := fmt.Sprintf("GRAVWELL_TEST_LIST_%d", i)
			if tt.env != nil {
				t.Setenv(name, *tt.env)
			}
			if tt.file != nil {
				pth := filepath.Join(t.TempDir(), "list")
				if *tt.file != missing {
					if err := os.WriteFile(pth, []byte(*tt.file), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv(name+"_FILE", pth)
			}

			lst := tt.cfg
			err := LoadEnvVar(&lst, name, nil)
			if tt.wantErr != nil || tt.errHas != `` {
				if err == nil {
					t.Fatalf("got %q, want an error", lst)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Fatalf("error %v, want %v", err, tt.wantErr)
				}
				if has := strings.ReplaceAll(tt.errHas, "TEST_LIST", name); !strings.Contains(err.Error(), has) {
					t.Fatalf("error %v, want it to contain %q", err, has)
				}
				if !reflect.DeepEqual(lst, tt.cfg) {
					t.Fatalf("list changed to %q on error", lst)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(lst, tt.want) {
				t.Fatalf("got %q, want %q", lst, tt.want)
			}
		})
	}
}

func TestLoadEnvVarListArgs(t *testing.T) {
	if err := LoadEnvVar((*[]string)(nil), "GRAVWELL_TEST_LIST", nil); !errors.Is(err, ErrInvalidArg) {
		t.Fatalf("nil list: %v", err)
	}
	// no variable name is a no-op, the same as the other types
	var lst []string
	if err := LoadEnvVar(&lst, "", nil); err != nil || lst != nil {
		t.Fatalf("empty name: %q %v", lst, err)
	}
}

// TestLoadDefaultsTargetsFile checks that the backend target lists read every
// line of their _FILE variables.
func TestLoadDefaultsTargetsFile(t *testing.T) {
	pth := filepath.Join(t.TempDir(), "targets")
	if err := os.WriteFile(pth, []byte("10.0.0.1:4023\n10.0.0.2:4023, 10.0.0.3:4023\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envClearTarget+"_FILE", pth)
	var ic IngestConfig
	if err := ic.loadDefaults(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"10.0.0.1:4023", "10.0.0.2:4023", "10.0.0.3:4023"}; !reflect.DeepEqual(ic.Cleartext_Backend_Target, want) {
		t.Fatalf("got %q, want %q", ic.Cleartext_Backend_Target, want)
	}

	t.Setenv(envEncTarget, ",")
	ic = IngestConfig{}
	if err := ic.loadDefaults(); !errors.Is(err, ErrEmptyEnvList) {
		t.Fatalf("got %v, want %v", err, ErrEmptyEnvList)
	}
}

//go:fix inline
func strPtr(s string) *string { return new(s) }
