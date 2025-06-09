/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldlist

import (
	"os"
	"path"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/spf13/pflag"
)

func Test_determineFormat(t *testing.T) {
	// spin up the logger
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal("failed to spawn logger:", err)
	}

	t.Run("unparsed", func(t *testing.T) {
		fs := listStarterFlags()
		if format := determineFormat(&fs); format != unknown {
			t.Error("incorrect format:", testsupport.ExpectedActual(unknown, format))
		}
	})
	t.Run("undefined CSV", func(t *testing.T) {
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		//fs.Bool(ft.Name.CSV, false, "")
		fs.Parse([]string{})
		if format := determineFormat(fs); format != unknown {
			t.Error("incorrect format:", testsupport.ExpectedActual(unknown, format))
		}
	})
	t.Run("CSV", func(t *testing.T) {
		fs := listStarterFlags()
		fs.Parse([]string{"--csv"})
		if format := determineFormat(&fs); format != csv {
			t.Error("incorrect format:", testsupport.ExpectedActual(csv, format))
		}
	})
	t.Run("undefined JSON", func(t *testing.T) {
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.Bool(ft.Name.CSV, false, "")
		fs.Parse([]string{})
		if format := determineFormat(fs); format != unknown {
			t.Error("incorrect format:", testsupport.ExpectedActual(unknown, format))
		}
	})
	t.Run("JSON", func(t *testing.T) {
		fs := listStarterFlags()
		fs.Parse([]string{"--json"})
		if format := determineFormat(&fs); format != json {
			t.Error("incorrect format:", testsupport.ExpectedActual(json, format))
		}
	})
	t.Run("CSV priority over JSON", func(t *testing.T) {
		fs := listStarterFlags()
		fs.Parse([]string{"--csv", "--json"})
		if format := determineFormat(&fs); format != csv {
			t.Error("incorrect format:", testsupport.ExpectedActual(csv, format))
		}
	})
	t.Run("neither", func(t *testing.T) {
		fs := listStarterFlags()
		fs.Parse([]string{})
		if format := determineFormat(&fs); format != table {
			t.Error("incorrect format:", testsupport.ExpectedActual(table, format))
		}
	})
}

func Test_initOutFile(t *testing.T) {
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
		fs := listStarterFlags()
		fs.Parse([]string{"-o", ""})
		if f, err := initOutFile(&fs); err != nil {
			t.Error("unexpected error", testsupport.ExpectedActual(nil, err))
		} else if f != nil {
			t.Errorf("a file was created: %+v", f)
		}
	})
	t.Run("truncate", func(t *testing.T) {
		var path = "hello.world"
		orig, err := os.Create(path)
		if err != nil {
			t.Skip("failed to create file to be truncated:", err)
		}
		t.Cleanup(func() { os.Remove(path) })
		orig.WriteString("Hello World")
		orig.Sync()
		orig.Close()

		fs := listStarterFlags()
		fs.Parse([]string{"-o", path})
		if f, err := initOutFile(&fs); err != nil {
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

func Test_format_String(t *testing.T) {
	tests := []struct {
		name string
		f    outputFormat
		want string
	}{
		{"JSON", json, "JSON"},
		{"CSV", csv, "CSV"},
		{"table", table, "table"},
		{"unknown", 5, "unknown format (5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.String(); got != tt.want {
				t.Errorf("format.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
