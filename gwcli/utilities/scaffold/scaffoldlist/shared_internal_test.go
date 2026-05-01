//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/pflag"
)

// Testing for unexported functions in shared, as many of these are critical.

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
		fs := buildFlagSet(false, nil)
		fs.Parse([]string{"-o", ""})
		if f, err := initOutFile(fs); err != nil {
			t.Error("unexpected error", testsupport.ExpectedActual(nil, err))
		} else if f != nil {
			t.Errorf("a file was created: %+v", f)
		}
	})
	t.Run("whitespace path with pretty defined", func(t *testing.T) {
		fs := buildFlagSet(true, nil)
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

		fs := buildFlagSet(false, nil)
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
		{"default, pretty", []string{}, true, formatPretty},
		{"default, no pretty", []string{}, false, formatTable},
		{"explicit pretty, pretty", []string{"--pretty"}, true, formatPretty},
		{"explicit pretty, no pretty", []string{"--pretty"}, false, formatTable},
		{"csv, pretty", []string{"--" + ft.CSV.Name()}, true, formatCSV},
		{"csv, no pretty", []string{"--" + ft.CSV.Name()}, false, formatCSV},
		{"json, pretty", []string{"--" + ft.JSON.Name()}, true, formatJSON},
		{"json, no pretty", []string{"--" + ft.JSON.Name()}, false, formatJSON},
		{"csv precedence over json", []string{"--" + ft.JSON.Name(), "--" + ft.CSV.Name()}, false, formatCSV},
		{"pretty precedence over all", []string{"--" + ft.JSON.Name(), "--" + ft.CSV.Name(), "--pretty", "--" + ft.Table.Name()}, true, formatPretty},
		{"pretty defined, but --table requested", []string{"--" + ft.Table.Name()}, true, formatTable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// generate flagset
			fs := buildFlagSet(tt.prettyDefined, nil)
			fs.Parse(tt.args)
			if got := determineFormat(fs, tt.prettyDefined); got != tt.want {
				t.Errorf("determineFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normalizeToDQ(t *testing.T) {
	defDQToAlias := map[string]string{
		"a":   "",
		"b":   "",
		"c":   "C",
		"z.1": "one",
		"z.2": "two",
	}
	defAliasToDQ := map[string]string{} // invert
	for dq, alias := range defDQToAlias {
		if alias == "" {
			continue
		}
		defAliasToDQ[alias] = dq
	}

	tests := []struct {
		name               string
		columnsToNormalize []string
		DQToAlias          map[string]string
		AliasToDQ          map[string]string
		wantNormalized     []string
		wantUnknown        []string
	}{
		{"simple",
			[]string{"a"}, defDQToAlias, defAliasToDQ,
			[]string{"a"}, nil},
		{"all DQs",
			[]string{"a", "b", "c", "z.1", "z.2"}, defDQToAlias, defAliasToDQ,
			[]string{"a", "b", "c", "z.1", "z.2"}, nil},
		{"all aliases",
			[]string{"C", "one", "two"}, defDQToAlias, defAliasToDQ,
			[]string{"c", "z.1", "z.2"}, nil},
		{"mixed, duplicated, and unknown",
			[]string{"C", "h", "z.2", "two", "a", "b", "u"}, defDQToAlias, defAliasToDQ,
			[]string{"c", "z.2", "z.2", "a", "b"}, []string{"h", "u"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNormalized, gotUnknown := normalizeToDQ(tt.columnsToNormalize, tt.DQToAlias, tt.AliasToDQ)
			if !slices.Equal(gotNormalized, tt.wantNormalized) {
				t.Error("bad normalized columns", testsupport.ExpectedActual(tt.wantNormalized, gotNormalized))
			}
			if !slices.Equal(gotUnknown, tt.wantUnknown) {
				t.Error("bad unknown columns", testsupport.ExpectedActual(tt.wantUnknown, gotUnknown))
			}
		})
	}
}

// Testing to ensure:
//
// 1) all three gets operate in order of priority
//
// 2) that all columns are fetched as DQ
//
// 3) that all columns are properly sorted
func Test_getColumns(t *testing.T) {
	DQToAlias, AliasToDQ := map[string]string{
		"Marika":    "Radagon",
		"Morgot":    "Margit",
		"Ranni":     "",
		"Alexander": "",
	}, map[string]string{
		"Radagon": "Marika",
		"Margit":  "Morgot",
	}

	t.Run("--all", func(t *testing.T) {
		fs := buildFlagSet(false, nil) // default cols shouldn't matter for this
		if err := fs.Parse([]string{"--" + ft.AllColumns.Name()}); err != nil {
			t.Fatal(err)
		}
		got, err := getColumns(fs, DQToAlias, AliasToDQ)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Alexander", "Marika", "Morgot", "Ranni"} // aliases don't affect all
		if !slices.Equal(got, want) {
			t.Fatal(testsupport.ExpectedActual(want, got))
		}
	})
	t.Run("--columns selects only DQ, duplicate columns", func(t *testing.T) {
		fs := buildFlagSet(false, nil) // default cols shouldn't matter for this

		requestedColumns := []string{"Alexander", "Ranni", "Ranni", "Marika"}

		if err := fs.Parse([]string{"--" + ft.SelectColumns.Name() + "=" + strings.Join(requestedColumns, ",")}); err != nil {
			t.Fatal(err)
		}
		got, err := getColumns(fs, DQToAlias, AliasToDQ)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, requestedColumns) {
			t.Fatal(testsupport.ExpectedActual(requestedColumns, got))
		}
	})
	t.Run("--columns selects DQ+Alias mix", func(t *testing.T) {
		fs := buildFlagSet(false, nil)

		requestedColumns := []string{"Radagon", "Alexander", "Ranni", "Margit"}

		if err := fs.Parse([]string{"--" + ft.SelectColumns.Name() + "=" + strings.Join(requestedColumns, ",")}); err != nil {
			t.Fatal(err)
		}
		got, err := getColumns(fs, DQToAlias, AliasToDQ)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Marika", "Alexander", "Ranni", "Morgot"}
		if !slices.Equal(got, want) {
			t.Fatal(testsupport.ExpectedActual(want, got))
		}
	})
	t.Run("default columns", func(t *testing.T) {
		// default columns are expected to be DQ
		defaultColumns := []string{"Morgot"}

		fs := buildFlagSet(false, defaultColumns)

		if err := fs.Parse([]string{}); err != nil {
			t.Fatal(err)
		}
		got, err := getColumns(fs, DQToAlias, AliasToDQ)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, defaultColumns) {
			t.Fatal(testsupport.ExpectedActual(defaultColumns, got))
		}
	})
}

type nuclearThrone struct {
	Plant    string
	Robot    []int
	unexport struct {
		Fish int
		Eyes float32
	}
	Export struct {
		YV *struct {
			YungCuz *string
		}
	}
	m     map[string]uint
	Rogue complex64
}

// The dot-qual map of the NuclearThrone struct as constructed by Weave.StructFields()
var ntDQs = map[string]string{
	"Export.YV.YungCuz": "",
	"Plant":             "",
	"Robot":             "",
	"Rogue":             "",
}

func Test_listOutput(t *testing.T) {
	t.Run("pretty", func(t *testing.T) {
		ppf := func(_ []string, _ map[string]string) (string, error) {
			return "pretty", nil
		}
		out, err := listOutput[struct{}](buildFlagSet(true, nil), formatPretty, nil, nil, ppf, nil)
		if err != nil {
			t.Fatal(err)
		}
		if out != "pretty" {
			t.Error(testsupport.ExpectedActual("pretty", out))
		}
	})
	s := "yung fam"

	data := []nuclearThrone{
		{Plant: "vines", Robot: nil},
		{Export: struct{ YV *struct{ YungCuz *string } }{&struct{ YungCuz *string }{YungCuz: &s}}},
		{Plant: "no popo", Rogue: complex64(9i + -1)},
	}

	dataFunc := func(fs *pflag.FlagSet) ([]nuclearThrone, error) { return data, nil }

	aliased := maps.Clone(ntDQs)
	aliased["Robot"] = "munch"

	tests := []struct {
		dqColumns []string
		format    outputFormat
		want      string
	}{
		{[]string{"Plant"}, formatCSV, "Plant\n" + "vines\n" + "\n" + "no popo"},
		{[]string{"Plant", "Robot"}, formatCSV, "Plant,munch\n" + "vines,[]\n" + ",[]\n" + "no popo,[]"},
		{[]string{"Export.YV.YungCuz", "Robot", "Rogue"}, formatJSON, `[` +
			`{"Export":{"YV":{"YungCuz":"nil"}},"Rogue":{"Real":0,"Imaginary":0},"munch":[]},` +
			`{"Export":{"YV":{"YungCuz":"yung fam"}},"Rogue":{"Real":0,"Imaginary":0},"munch":[]},` +
			`{"Export":{"YV":{"YungCuz":"nil"}},"Rogue":{"Real":-1,"Imaginary":9},"munch":[]}` +
			`]`,
		},
		{[]string{"Rogue", "Plant", "Robot", "Export.YV.YungCuz"}, formatTable,
			"┌───────────────┬───────────────┬───────────────┬───────────────┐\n" +
				"│ Rogue         │ Plant         │ munch         │ Export.YV.Yun │\n" +
				"├───────────────┼───────────────┼───────────────┼───────────────┤\n" +
				"│ (0+0i)        │ vines         │ []            │ nil           │\n" +
				"├───────────────┼───────────────┼───────────────┼───────────────┤\n" +
				"│ (0+0i)        │               │ []            │ yung fam      │\n" +
				"├───────────────┼───────────────┼───────────────┼───────────────┤\n" +
				"│ (-1+9i)       │ no popo       │ []            │ nil           │\n" +
				"└───────────────┴───────────────┴───────────────┴───────────────┘"},
	}
	for i, tt := range tests {
		t.Run(strconv.FormatInt(int64(i+1), 10), func(t *testing.T) {
			out, err := listOutput(buildFlagSet(false, nil), tt.format, tt.dqColumns, dataFunc, nil, aliased)
			if err != nil {
				t.Error(err)
			}
			if out != tt.want {
				t.Error(testsupport.ExpectedActual(tt.want, out))
			}

		})
	}

}
