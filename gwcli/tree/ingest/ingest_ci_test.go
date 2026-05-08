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
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

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
