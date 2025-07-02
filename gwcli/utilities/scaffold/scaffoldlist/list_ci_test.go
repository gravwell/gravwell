/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

// Tests that do not require a backend and thus can be run from a pipeline

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/pflag"
)

func Test_initOutFile(t *testing.T) {
	tDir := t.TempDir()
	t.Run("undefined output", func(t *testing.T) {
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.Parse([]string{})
		if f, err := initOutFile(fs); err == nil {
			t.Error("nil error")
		} else if f != nil {
			t.Errorf("a file was created: %+v", f)
		}
	})
	t.Run("whitespace path", func(t *testing.T) {
		fs := buildFlagSet(nil, false)
		fs.Parse([]string{"-o", ""})
		if f, err := initOutFile(fs); err != nil {
			t.Error("unexpected error", testsupport.ExpectedActual(nil, err))
		} else if f != nil {
			t.Errorf("a file was created: %+v", f)
		}
	})
	t.Run("whitespace path with pretty defined", func(t *testing.T) {
		fs := buildFlagSet(nil, true)
		fs.Parse([]string{"-o", ""})
		if f, err := initOutFile(fs); err != nil {
			t.Error("unexpected error", testsupport.ExpectedActual(nil, err))
		} else if f != nil {
			t.Errorf("a file was created: %+v", f)
		}
	})

	t.Run("truncate", func(t *testing.T) {
		var path = path.Join(tDir, "hello.world")
		orig, err := os.Create(path)
		if err != nil {
			t.Skip("failed to create file to be truncated:", err)
		}
		t.Cleanup(func() { os.Remove(path) })
		orig.WriteString("Hello World")
		orig.Sync()
		orig.Close()

		fs := buildFlagSet(nil, false)
		fs.Parse([]string{"-o", path})
		if f, err := initOutFile(fs); err != nil {
			t.Error("unexpected error", testsupport.ExpectedActual(nil, err))
		} else if f == nil {
			t.Error("a file was not created, but should have been")
		} else if stat, err := f.Stat(); err != nil {
			t.Fatal("failed to stat file:", err)
		} else if stat.Size() != 0 {
			t.Fatalf("file was not truncated (size: %v)", stat.Size())
		}
	})
}

func Test_determineFormat(t *testing.T) {
	// spin up the logger
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal("failed to spawn logger:", err)
	}

	tests := []struct {
		name          string
		args          []string
		prettyDefined bool
		want          outputFormat
	}{
		{"default, pretty", []string{}, true, pretty},
		{"default, no pretty", []string{}, false, tbl},
		{"explicit pretty, pretty", []string{"--pretty"}, true, pretty},
		{"explicit pretty, no pretty", []string{"--pretty"}, false, tbl},
		{"csv, pretty", []string{"--csv"}, true, csv},
		{"csv, no pretty", []string{"--csv"}, false, csv},
		{"json, pretty", []string{"--json"}, true, json},
		{"json, no pretty", []string{"--json"}, false, json},
		{"csv precedence over json", []string{"--json", "--csv"}, false, csv},
		{"pretty precedence over all", []string{"--json", "--csv", "--pretty", "--table"}, true, pretty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// generate flagset
			fs := buildFlagSet(nil, tt.prettyDefined)
			fs.Parse(tt.args)
			if got := determineFormat(fs, tt.prettyDefined); got != tt.want {
				t.Errorf("determineFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Mostly just tests that options are properly reflected in the returned command and model.
func TestNewListAction(t *testing.T) {
	tDir := t.TempDir()
	// spin up the logger
	if err := clilog.Init(path.Join(tDir, "dev.log"), "debug"); err != nil {
		t.Fatal("failed to spawn logger:", err)
	}

	short, long := "a test action", "a test action's longer description"
	t.Run("non-struct dataStruct", func(t *testing.T) {
		var recovered bool
		defer func() {
			if !recovered {
				t.Errorf("test did not recover from panic")
			}
		}()
		defer func() { // recover from the expected panic and note that we recovered
			recover()
			recovered = true
		}()
		NewListAction(short, long, 5, func(fs *pflag.FlagSet) ([]int, error) { return nil, nil }, Options{})
	})
	t.Run("non alphanumerics in use", func(t *testing.T) {
		use := "<action|"
		type st struct {
		}

		var recovered bool
		defer func() {
			if !recovered {
				t.Errorf("test did not recover from panic")
			}
		}()
		defer func() { // recover from the expected panic and note that we recovered
			recover()
			recovered = true
		}()
		NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) { return nil, nil }, Options{Use: use})
	})

	// column tests
	tests := []struct {
		name          string
		options       Options
		args          []string
		wantedColumns []string
	}{
		{"default to all columns", Options{}, []string{"--script", "--csv"}, []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}},
		{"respect defaults option",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--script", "--csv"},
			[]string{"Col1", "Col4.SubCol1"},
		},
		{"all overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--script", "--csv", "--all"},
			[]string{"Col1", "Col2", "Col3", "Col4.SubCol1"},
		},
		{"explicit columns overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--script", "--csv", "--columns", "Col3"},
			[]string{"Col3"},
		},
	}
	type st struct { // the struct we will be testing against
		Col1 string
		Col2 uint
		Col3 int
		Col4 struct {
			SubCol1        bool
			privateSubCol2 float32
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// generate the pair
			pair := NewListAction("test short", "test long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
				return []st{
					{"1", 1, -1, struct {
						SubCol1        bool
						privateSubCol2 float32
					}{true, 3.14}},
				}, nil
			}, tt.options)
			pair.Action.SetArgs(tt.args)
			// capture output
			var sb strings.Builder
			var sbErr strings.Builder
			pair.Action.SetOut(&sb)
			pair.Action.SetErr(&sbErr)
			// bolt on persistent flags that Mother would usually take care of
			pair.Action.Flags().Bool("script", false, "")
			if err := pair.Action.Execute(); err != nil {
				t.Fatal(err)
			} else if sbErr.String() != "" {
				f, err := os.ReadFile(path.Join(tDir, "dev.log"))
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("Dev Log:\n%s", f)
				t.Fatal(sbErr.String())
			}
			// we only care about the first line of the csv
			columns, _, found := strings.Cut(sb.String(), "\n")
			if !found {
				t.Fatalf("failed to find csv header in %v", sb.String())
			}
			exploded := strings.Split(columns, ",")
			if !testsupport.SlicesUnorderedEqual(exploded, tt.wantedColumns) {
				t.Fatalf("columns mismatch (not accounting for order): %v", testsupport.ExpectedActual(tt.wantedColumns, exploded))
			}
		})
	}

	t.Run("show columns", func(t *testing.T) {
		// generate the pair
		pair := NewListAction("test short", "test long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{
				{"1", 1, -1, struct {
					SubCol1        bool
					privateSubCol2 float32
				}{true, 3.14}},
			}, nil
		}, Options{})
		pair.Action.SetArgs([]string{"--script", "--csv", "--show-columns"})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool("script", false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}
		exploded := strings.Split(strings.TrimSpace(sb.String()), " ")
		wanted := []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}
		if !testsupport.SlicesUnorderedEqual(exploded, wanted) {
			t.Fatalf("columns mismatch (not accounting for order): %v",
				testsupport.ExpectedActual(wanted, exploded))
		}
	})

}
