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
	ecsv "encoding/csv"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

// the struct we will be testing against as the List's type
type st struct {
	Col1 string
	Col2 uint
	Col3 int
	Col4 struct {
		SubCol1        bool
		privateSubCol2 float32
	}
}

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

func Test_showColumnsString(t *testing.T) {
	actual := showColumnsString([]string{"A.1", "B", "C.1.⌚"}, map[string]string{"C.1.⌚": "Clock", "nonexistent": "some_alias"})
	expected := "A.1,B,Clock"

	if actual != expected {
		t.Fatal(testsupport.ExpectedActual(expected, actual))
	}
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
		{"csv, pretty", []string{"--" + ft.CSV.Name()}, true, csv},
		{"csv, no pretty", []string{"--" + ft.CSV.Name()}, false, csv},
		{"json, pretty", []string{"--" + ft.JSON.Name()}, true, json},
		{"json, no pretty", []string{"--" + ft.JSON.Name()}, false, json},
		{"csv precedence over json", []string{"--" + ft.JSON.Name(), "--" + ft.CSV.Name()}, false, csv},
		{"pretty precedence over all", []string{"--" + ft.JSON.Name(), "--" + ft.CSV.Name(), "--pretty", "--" + ft.Table.Name()}, true, pretty},
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
	t.Run("specific columns to outfile", func(t *testing.T) {
		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{
				{"1", 1, -1, struct {
					SubCol1        bool
					privateSubCol2 float32
				}{true, 3.14}},
			}, nil
		}, Options{Use: "validUse"})
		filepath := path.Join(tDir, "specific_columns.csv")
		pair.Action.SetArgs([]string{"--" + ft.NoInteractive.Name(), "--" + ft.CSV.Name(), "--" + ft.SelectColumns.Name(), "Col1,Col3", "-" + ft.Output.Shorthand(), filepath})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}
		// check the data in the output file
		f, err := os.Open(filepath)
		if err != nil {
			t.Fatal(err)
		}
		csvRdr := ecsv.NewReader(f)
		records, err := csvRdr.ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 2 {
			t.Fatal("incorrect record size.", testsupport.ExpectedActual(2, len(records)))
		}
		hdr := records[0]
		wantedHdr := []string{"Col1", "Col3"}
		if !testsupport.SlicesUnorderedEqual(hdr, wantedHdr) {
			t.Fatalf("hdr mismatch (not accounting for order): %v",
				testsupport.ExpectedActual(wantedHdr, hdr))
		}
		data := records[1]
		wantedData := []string{"1", "-1"}
		if !testsupport.SlicesUnorderedEqual(data, wantedData) {
			t.Fatalf("data mismatch (not accounting for order): %v",
				testsupport.ExpectedActual(wantedData, data))
		}
	})

	t.Run("aliased columns", func(t *testing.T) {
		data := []st{
			{"1", 1, -1, struct {
				SubCol1        bool
				privateSubCol2 float32
			}{true, 3.14}},
		}

		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return data, nil
		}, Options{
			Use:           "validUse",
			ColumnAliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		pair.Action.SetArgs([]string{})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}

		// construct the expected table
		expected := weave.ToTable(data, []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}, weave.TableOptions{
			Base:    stylesheet.Table,
			Aliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		actual := strings.TrimSpace(sb.String())

		if expected != actual {
			t.Fatal(testsupport.ExpectedActual(expected, actual))
		}
	})
	t.Run("aliased columns JSON", func(t *testing.T) {
		data := []st{
			{"1", 1, -1, struct {
				SubCol1        bool
				privateSubCol2 float32
			}{true, 3.14}},
		}

		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return data, nil
		}, Options{
			Use:           "validUse",
			ColumnAliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		pair.Action.SetArgs([]string{})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		pair.Action.Flags().Set("json", "true")
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}

		// construct the expected table
		expected, err := weave.ToJSON(data, []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}, weave.JSONOptions{
			Aliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		actual := strings.TrimSpace(sb.String())

		if expected != actual {
			t.Fatal(testsupport.ExpectedActual(expected, actual))
		}
	})
	t.Run("exclude default columns", func(t *testing.T) {
		data := []st{
			{"1", 1, -1, struct {
				SubCol1        bool
				privateSubCol2 float32
			}{true, 3.14}},
		}

		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return data, nil
		}, Options{
			Use:                       "validUse",
			ExcludeColumnsFromDefault: []string{"Col1"},
		})

		// check default columns
		if la, ok := pair.Model.(*ListAction[st]); !ok {
			t.Fatal("failed to assert model to listAction")
		} else if !testsupport.SlicesUnorderedEqual(la.defaultColumns, []string{"Col2", "Col3", "Col4.SubCol1"}) {
			t.Fatal("bad default columns.", testsupport.ExpectedActual([]string{"Col2", "Col3", "Col4.SubCol1"}, la.defaultColumns))
		}

		pair.Action.SetArgs([]string{})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		//pair.Action.Flags().Set("json", "true")
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}

		// construct the expected table
		expected := weave.ToTable(data, []string{"Col2", "Col3", "Col4.SubCol1"}, weave.TableOptions{Base: stylesheet.Table})

		actual := strings.TrimSpace(sb.String())

		if expected != actual {
			t.Fatal(testsupport.ExpectedActual(expected, actual))
		}
	})

	t.Run("aliased columns CSV", func(t *testing.T) {
		data := []st{
			{"1", 1, -1, struct {
				SubCol1        bool
				privateSubCol2 float32
			}{true, 3.14}},
		}

		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return data, nil
		}, Options{
			Use:           "validUse",
			ColumnAliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		pair.Action.SetArgs([]string{})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		pair.Action.Flags().Set("csv", "true")
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}

		// construct the expected table
		expected := weave.ToCSV(data, []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}, weave.CSVOptions{
			Aliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		actual := strings.TrimSpace(sb.String())

		if expected != actual {
			t.Fatal(testsupport.ExpectedActual(expected, actual))
		}
	})

	t.Run("show columns with aliased", func(t *testing.T) {
		data := []st{
			{"1", 1, -1, struct {
				SubCol1        bool
				privateSubCol2 float32
			}{true, 3.14}},
		}

		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return data, nil
		}, Options{
			Use:           "validUse",
			ColumnAliases: map[string]string{"Col1": "C1", "Col4.SubCol1": "SC1"},
		})
		pair.Action.SetArgs([]string{"--" + ft.ShowColumns.Name()})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}

		// construct the expected output
		exploded := strings.Split(strings.TrimSpace(sb.String()), ",")
		expected := []string{"C1", "Col2", "Col3", "SC1"}
		if !testsupport.SlicesUnorderedEqual(exploded, expected) {
			t.Fatalf("columns mismatch (not accounting for order): %v",
				testsupport.ExpectedActual(expected, exploded))
		}
	})

	// column csvTests
	csvTests := []struct {
		name          string
		options       Options
		args          []string
		wantedColumns []string
	}{
		{"default to all columns", Options{}, []string{}, []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}},
		{"respect defaults option",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{}, // --no-interactive and --csv are attached in the test
			[]string{"Col1", "Col4.SubCol1"},
		},
		{"all overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--" + ft.AllColumns.Name()}, // --no-interactive and --csv are attached in the test
			[]string{"Col1", "Col2", "Col3", "Col4.SubCol1"},
		},
		{"explicit columns overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--" + ft.SelectColumns.Name(), "Col3"}, // --no-interactive and --csv are attached in the test
			[]string{"Col3"},
		},
	}
	for _, tt := range csvTests {
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
			pair.Action.SetArgs(append(tt.args, "--"+ft.NoInteractive.Name(), "--"+ft.CSV.Name()))
			// capture output
			var sb strings.Builder
			var sbErr strings.Builder
			pair.Action.SetOut(&sb)
			pair.Action.SetErr(&sbErr)
			// bolt on persistent flags that Mother would usually take care of
			pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
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

	t.Run("unknown default column", func(t *testing.T) {
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
		NewListAction(short, long, st{},
			func(fs *pflag.FlagSet) ([]st, error) { return nil, nil },
			Options{DefaultColumns: []string{"Xol1"}})
	})
	t.Run("unknown default column -- lowercase", func(t *testing.T) {
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
		NewListAction(short, long, st{},
			func(fs *pflag.FlagSet) ([]st, error) { return nil, nil },
			Options{DefaultColumns: []string{"col1"}})
	})

	t.Run("show columns", func(t *testing.T) {
		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{
				{"1", 1, -1, struct {
					SubCol1        bool
					privateSubCol2 float32
				}{true, 3.14}},
			}, nil
		}, Options{Use: "validU53"})
		pair.Action.SetArgs([]string{"--" + ft.NoInteractive.Name(), "--" + ft.CSV.Name(), "--" + ft.ShowColumns.Name()})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sbErr.String() != "" {
			t.Fatal(sbErr.String())
		}
		exploded := strings.Split(strings.TrimSpace(sb.String()), ",")
		wanted := []string{"Col1", "Col2", "Col3", "Col4.SubCol1"}
		if !testsupport.SlicesUnorderedEqual(exploded, wanted) {
			t.Fatalf("columns mismatch (not accounting for order): %v",
				testsupport.ExpectedActual(wanted, exploded))
		}
	})

	t.Run("bad column given", func(t *testing.T) {
		// generate the pair
		pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{
				{"1", 1, -1, struct {
					SubCol1        bool
					privateSubCol2 float32
				}{true, 3.14}},
			}, nil
		}, Options{Use: "validU53"})
		pair.Action.SetArgs([]string{"--" + ft.NoInteractive.Name(), "--" + ft.CSV.Name(), "--" + ft.SelectColumns.Name() + "=Xol1"})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
		if err := pair.Action.Execute(); err != nil {
			t.Fatal(err)
		} else if sb.String() != "" { // TODO confirm err
			t.Error("expected stdout to be empty due to error")
		}
		errS := strings.TrimSpace(sbErr.String())
		if !strings.Contains(errS, "Xol1") {
			t.Fatal("error does not contain expected string. Error: ")
		}
	})

	jsonTests := []struct {
		name       string
		options    Options
		args       []string
		wantedJSON string
	}{
		{"default to all columns",
			Options{},
			[]string{},
			`[{"Col1":"1","Col2":1,"Col3":-1,"Col4":{"SubCol1":"true"}}]`,
		},
		{"respect defaults option",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{}, // --no-interactive and --json are attached in the test
			`[{"Col1":"1","Col4":{"SubCol1":"true"}}]`,
		},
		{"all overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--" + ft.AllColumns.Name()}, // --no-interactive and --json are attached in the test
			`[{"Col1":"1","Col2":1,"Col3":-1,"Col4":{"SubCol1":"true"}}]`,
		},
		{"explicit columns overrides default columns",
			Options{DefaultColumns: []string{"Col1", "Col4.SubCol1"}},
			[]string{"--" + ft.SelectColumns.Name(), "Col3"}, // --no-interactive and --json are attached in the test
			`[{"Col3":-1}]`,
		},
	}
	for _, tt := range jsonTests {
		t.Run(tt.name, func(t *testing.T) {
			// generate the pair
			pair := NewListAction(short, long, st{}, func(fs *pflag.FlagSet) ([]st, error) {
				return []st{
					{"1", 1, -1, struct {
						SubCol1        bool
						privateSubCol2 float32
					}{true, 3.14}},
				}, nil
			}, tt.options)
			pair.Action.SetArgs(append(tt.args, "--"+ft.NoInteractive.Name(), "--"+ft.JSON.Name()))
			// capture output
			var sb strings.Builder
			var sbErr strings.Builder
			pair.Action.SetOut(&sb)
			pair.Action.SetErr(&sbErr)
			// bolt on persistent flags that Mother would usually take care of
			pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
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

			// compare
			actual := strings.TrimSpace(sb.String())
			if actual != tt.wantedJSON {
				t.Fatalf("bad JSON. %v", testsupport.ExpectedActual(tt.wantedJSON, actual))
			}
		})
	}

	t.Run("additional flags", func(t *testing.T) {
		pair := NewListAction("short", "long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{}, nil
		}, Options{AddtlFlags: func() pflag.FlagSet {
			fs := pflag.FlagSet{}
			fs.IPP("ipp", "p", nil, "")
			return fs
		}},
		)

		pair.Action.ParseFlags([]string{"-p", "127.0.0.1"})

		if returned, err := pair.Action.Flags().GetIP("ipp"); err != nil {
			t.Fatal(err)
		} else if returned.String() != "127.0.0.1" {
			t.Fatal("bad IP.", testsupport.ExpectedActual("127.0.0.1", returned.String()))
		}
	})
	t.Run("extra argument validation", func(t *testing.T) {
		pair := NewListAction("short", "long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{}, nil
		}, Options{
			AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.IPP("ipp", "p", nil, "must be an ip in the 127.0.0.0/8 block")
				return fs
			},
			ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				ip, err := fs.GetIP("ipp")
				if err != nil {
					return "", err
				}
				if ip4 := ip.To4(); ip4 == nil || ip4[0] != 127 {
					return "ip address must be in the 127.0.0.0/8 block", nil
				}
				return "", nil
			},
		},
		)

		pair.Action.ParseFlags([]string{"-p", "127.0.0.1"})

		if returned, err := pair.Action.Flags().GetIP("ipp"); err != nil {
			t.Fatal(err)
		} else if returned.String() != "127.0.0.1" {
			t.Fatal("bad IP.", testsupport.ExpectedActual("127.0.0.1", returned.String()))
		}
	})
	t.Run("pretty", func(t *testing.T) {
		prettyReturn := "pretty string"
		pair := NewListAction("short", "long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
			return []st{}, nil
		}, Options{Pretty: func(c *pflag.FlagSet) (string, error) { return prettyReturn, nil }})
		pair.Action.SetArgs([]string{"--" + ft.NoInteractive.Name()})
		// capture output
		var sb strings.Builder
		var sbErr strings.Builder
		pair.Action.SetOut(&sb)
		pair.Action.SetErr(&sbErr)
		// bolt on persistent flags that Mother would usually take care of
		pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "")
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
		// check that the pretty outcome is what we expect
		outcome := strings.TrimSpace(sb.String())
		if prettyReturn != outcome {
			t.Fatal("bad pretty text", testsupport.ExpectedActual(prettyReturn, outcome))
		}
	})
}

// Test the action model created by mimic'ing Mother and checking the struct after each stage.
// NOTE(rlandau): This tests is able to test all of the auxiliary aspects and fields of an interactive list action.
// However, it does not test the actual output (as this is returned as a printLineMessage, which is not exported and thus we cannot assert to).
// This could be worked around with reflection, but it isn't high enough priority to bother atm.
func TestModel(t *testing.T) {
	tDir := t.TempDir()

	// spin up the logger
	if err := clilog.Init(path.Join(tDir, "dev.log"), "debug"); err != nil {
		t.Fatal("failed to spawn logger:", err)
	}

	type flags struct {
		columns []string
		all     bool
	}
	type test struct {
		name    string
		options Options
		flags   flags
		// freeform arguments appended to the argument list
		// No additional processing is performed on them (e.g. you will need to prefix flags with '-' or '--')
		freeformArgs    []string
		wantInvalidArgs bool
	}
	tests := []test{
		{name: "default to all columns",
			options:         Options{},
			flags:           flags{},
			wantInvalidArgs: false},
		{name: "respect given columns",
			options:         Options{},
			flags:           flags{columns: []string{"Col1", "Col2"}},
			wantInvalidArgs: false,
		},
		{name: "respect all columns over defaults",
			options:         Options{DefaultColumns: []string{"Col1"}},
			flags:           flags{all: true},
			wantInvalidArgs: false,
		},
		{name: "additional flags",
			options: Options{AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("test", false, "")
				return fs
			}},
			flags:           flags{},
			wantInvalidArgs: false,
		},
		{name: "invalid flags, no extra validation",
			options: Options{AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Int("invalid", 0, "")
				return fs
			},
			},
			flags:           flags{},
			freeformArgs:    []string{"--invalid=inv"},
			wantInvalidArgs: true},
		{name: "invalid flags, w/ extra validation",
			options: Options{AddtlFlags: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Int("invalid", 0, "can only be set to 5")
				return fs
			},
				ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
					inv, err := fs.GetInt("invalid")
					if err != nil {
						return "", err
					}
					if inv != 5 {
						return "if --invalid is set, it must be set to 5", nil
					}
					return "", nil
				},
			},
			flags:           flags{},
			freeformArgs:    []string{"--invalid=2"},
			wantInvalidArgs: true},
		{name: "valid flags, w/ extra validation",
			options: Options{
				AddtlFlags: func() pflag.FlagSet {
					fs := pflag.FlagSet{}
					fs.Int("valid", 0, "can only be set to 5")
					return fs
				},
				ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
					inv, err := fs.GetInt("valid")
					if err != nil {
						return "", err
					}
					if inv != 5 {
						return "if --valid is set, it must be set to 5", nil
					}
					return "", nil
				},
			},
			flags:           flags{},
			freeformArgs:    []string{"--valid=5"},
			wantInvalidArgs: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := NewListAction("short", "long", st{}, func(fs *pflag.FlagSet) ([]st, error) {
				return []st{
					{Col1: "column", Col4: struct {
						SubCol1        bool
						privateSubCol2 float32
					}{SubCol1: false}},
					{Col1: "different column", Col3: -901},
				}, nil
			}, tt.options)

			// generate arguments list
			args := []string{}
			if tt.flags.columns != nil {
				args = append(args, "--"+ft.SelectColumns.Name()+"="+strings.Join(tt.flags.columns, ","))
			}
			if tt.flags.all {
				args = append(args, "--"+ft.AllColumns.Name())
			}
			args = append(args, tt.freeformArgs...)

			t.Logf("passing argument list: %v", args)

			// mimic mother's order of operations, validating after each step
			invalid, setArgsCmd, err := pair.Model.SetArgs(pair.Action.Flags(), args, 80, 50)
			t.Log(setArgsCmd)
			if tt.wantInvalidArgs && invalid != "" {
				return
			} else if tt.wantInvalidArgs && invalid == "" {
				t.Fatal("expected arguments to be invalid")
			} else if !tt.wantInvalidArgs && invalid != "" {
				t.Fatal("arguments were invalid: ", invalid)
			}
			if err != nil {
				t.Fatal(err)
			}
			if la, ok := pair.Model.(*ListAction[st]); !ok {
				t.Fatal("failed to assert model to listAction")
			} else {
				const pfx string = "Post-SetArgs: "
				// validate fields
				if !la.fs.Parsed() {
					t.Error(pfx + "flagset should be parsed")
				}

				// ensure available DS columns matches actual available columns
				if allColumns, err := weave.StructFields(st{}, exportedColumnsOnly); err != nil {
					t.Fatal(err)
				} else if !testsupport.SlicesUnorderedEqual(la.availDSColumns, allColumns) {
					t.Error("derived columns saved in list do not match externally derived columns.", testsupport.ExpectedActual(allColumns, la.availDSColumns))
				}

				// confirm columns were set properly
				if tt.flags.all { // prioritize all above all else
					if !testsupport.SlicesUnorderedEqual(la.columns, la.availDSColumns) {
						t.Error("derived columns saved in list do not match externally derived columns.", testsupport.ExpectedActual(la.availDSColumns, la.columns))
					}
				} else if len(tt.flags.columns) > 0 { // --columns was specified
					if !testsupport.SlicesUnorderedEqual(tt.flags.columns, la.columns) {
						t.Error("action columns do not match given columns", testsupport.ExpectedActual(tt.flags.columns, la.columns))
					}
				} else if len(tt.options.DefaultColumns) > 0 { // options.DefaultColumns was given
					if !testsupport.SlicesUnorderedEqual(tt.options.DefaultColumns, la.columns) {
						t.Error("action columns do not match default columns.", testsupport.ExpectedActual(tt.options.DefaultColumns, la.columns))
					}
				} else { // nothing was specified, check for all columns again
					if !testsupport.SlicesUnorderedEqual(la.columns, la.availDSColumns) {
						t.Error("true default columns is not all columns.", testsupport.ExpectedActual(la.availDSColumns, la.columns))
					}
				}

				// if additional flags were given, ensure they were bolted on
				if la.options.AddtlFlags != nil {
					afs := la.options.AddtlFlags()

					afs.Visit(func(f *pflag.Flag) {
						flag := la.fs.Lookup(f.Name)
						if flag == nil {
							t.Errorf(pfx+"additional flag %v does not exist", f.Name)
						}
					})
				}
				if la.outFile != nil {
					t.Error("unexpected outfile.", testsupport.ExpectedActual(nil, la.outFile))
				}

				if la.done {
					t.Errorf("list action is done prior to update")
				}
				if t.Failed() {
					t.FailNow()
				}
			}
			t.Log(pair.Model.Update(nil)) // list action does not care about messages
			if la, ok := pair.Model.(*ListAction[st]); !ok {
				t.Fatal("failed to assert model to listAction")
			} else {
				const pfx string = "Post-Update: "
				if !la.done {
					t.Errorf("list action is not done after update")
				}
				if t.Failed() {
					t.FailNow()
				}
			}
			view := pair.Model.View()
			if view != "" {
				t.Errorf("view returned data: %v", view)
			}
			// at this point we should be done
			if !pair.Model.Done() {
				t.Error("model should be done after a single cycle")
			}
			err = pair.Model.Reset()
			if err != nil {
				t.Errorf("failed to reset model")
			}
			if la, ok := pair.Model.(*ListAction[st]); !ok {
				t.Fatal("failed to assert model to listAction")
			} else {
				const pfx string = "Post-Reset: "
				if la.done {
					t.Errorf(pfx + "list action done was not reset properly")
				}
				if !testsupport.SlicesUnorderedEqual(la.columns, la.defaultColumns) {
					t.Error(pfx+"list action columns were not reset to defaults.", testsupport.ExpectedActual(la.defaultColumns, la.columns))
				}
				if la.fs.Parsed() {
					t.Error(pfx + "flagset should not be parsed")
				}
				// if additional flags were given, ensure they were bolted back on
				if la.options.AddtlFlags != nil {
					afs := la.options.AddtlFlags()

					afs.Visit(func(f *pflag.Flag) {
						flag := la.fs.Lookup(f.Name)
						if flag == nil {
							t.Errorf(pfx+"additional flag %v does not exist", f.Name)
						}
					})
				}
				if la.outFile != nil {
					t.Errorf(pfx+"outfile '%v' was not nil'd", la.outFile.Name())
				}
			}
		})
	}
	t.Run("interactive show columns", func(t *testing.T) {
		availableColumns := []string{"Column1", "column2", "sub.column.1", "Sub.column.2"}
		columnAliases := map[string]string{"Column1": "C1", "Sub.column.2": "Sc2"}

		// only sets and calls the bare minimum to test an Update that displays column
		la := ListAction[st]{
			showColumns:    true,
			availDSColumns: availableColumns,
			options: Options{
				ColumnAliases: columnAliases,
			},
		}
		expected := showColumnsString(availableColumns, columnAliases)

		tCmd := la.Update(nil)
		if tCmd == nil {
			t.Fatal("nil command")
		}
		// printLineMessages are private, so we need to reflect into it to check the value it holds
		voMsg := reflect.ValueOf(tCmd())
		if voMsg.Kind() != reflect.Struct {
			t.Fatal(testsupport.ExpectedActual(reflect.Struct, voMsg.Kind()))
		}
		if voMsg.NumField() != 1 {
			t.Fatal(testsupport.ExpectedActual(1, voMsg.NumField()))
		}
		voMessageBody := voMsg.FieldByName("messageBody")
		if voMessageBody.Kind() != reflect.String {
			t.Fatal(testsupport.ExpectedActual(reflect.String, voMessageBody.Kind()))
		}
		if expected != voMessageBody.String() {
			t.Fatal(testsupport.ExpectedActual(expected, voMessageBody.String()))
		}
	})

}
