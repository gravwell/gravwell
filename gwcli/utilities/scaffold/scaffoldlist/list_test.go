//go:build ci

package scaffoldlist_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/gravwell/gravwell/v4/utils/weave"
	"github.com/spf13/pflag"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)

	m.Run()
}

type NuclearThrone struct {
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
var NTDQs = map[string]string{
	"Export.YV.YungCuz": "",
	"Plant":             "",
	"Robot":             "",
	"Rogue":             "",
}

// ShowColumns tests that displaying all columns properly aliases, joins, and sorts them.
// Sorting must factor in aliases.
func TestShowColumns_AllFlag(t *testing.T) {
	t.Run("direct call", func(t *testing.T) {
		dqToAlias := map[string]string{"C.1.⌚": "Clock", "A": "a", "long.dq.field": ""}
		actual := scaffoldlist.ShowColumns(dqToAlias)

		// we expect that aliases are preferred (if they exist) and that columns are sorted alphabetically
		expected := strings.Join([]string{"a", "Clock", "long.dq.field"}, string(scaffoldlist.ShowColumnSep))

		if actual != expected {
			t.Fatal(testsupport.ExpectedActual(expected, actual))
		}
	})

	// now test it from the outside
	t.Run("via --"+ft.ShowColumns.Name(), func(t *testing.T) {
		data := []NuclearThrone{} // the data itself doesn't matter

		aliased := maps.Clone(NTDQs)
		// add some aliases
		aliased["Export.YV.YungCuz"] = "YC"

		pair := scaffoldlist.NewListAction("test function", "this is a test function",
			NuclearThrone{}, func(fs *pflag.FlagSet) ([]NuclearThrone, error) {
				return data, nil
			},
			maps.Clone(aliased), scaffoldlist.Options{})

		uniques.AttachPersistentFlags(pair.Action)
		if inv, _, err := pair.Model.SetArgs(pair.Action.Flags(), []string{"--" + ft.ShowColumns.Name()}, 80, 60); err != nil || inv != "" {
			t.Fatalf("bad SetArgs.\nInv: %v\nErr: %v", inv, err)
		}
		// all is returned from a tea.Cmd from Update
		cmd := pair.Model.Update(nil)
		if cmd == nil {
			t.Error("Update returned a nil command. Expected a Print cmd.")
		}

		// check result
		gotAllCols := strings.TrimSpace(testsupport.ExtractPrintLineMessageString(t, cmd, false, 0))
		wantAllCols := scaffoldlist.ShowColumns(aliased)
		if wantAllCols != gotAllCols {
			t.Error(testsupport.ExpectedActual(wantAllCols, gotAllCols))
		}

		// complete the cycle
		if shouldBeEmpty := pair.Model.View(); shouldBeEmpty != "" {
			t.Error("view from list commands should always be empty")
		}
		if err := pair.Model.Reset(); err != nil {
			t.Error("Reset failed with error: ", err)
		}
	})
}

// This test set mimics full Mother cycles for a given list action.
// It is intended to cover common operations, rather than chasing edge cases.
func TestMotherCycle(t *testing.T) {
	// generate data the tests can test against
	yc := "yung cuz"
	data := []NuclearThrone{
		{
			Plant: "plant",
			Robot: []int{1, 2, 3},
			unexport: struct {
				Fish int
				Eyes float32
			}{5, 3.14},
			Export: struct{ YV *struct{ YungCuz *string } }{
				&struct{ YungCuz *string }{YungCuz: &yc},
			},
			Rogue: complex64(3.14i),
		},
		{
			Plant: "plant2",
			Robot: []int{2, 3, 4},
			unexport: struct {
				Fish int
				Eyes float32
			}{6, 4.14},
			Export: struct{ YV *struct{ YungCuz *string } }{
				&struct{ YungCuz *string }{YungCuz: &yc},
			},
			Rogue: complex64(3.14 - 2.4i),
		},
	}

	// generate aliases we can test against
	aliased := maps.Clone(NTDQs)
	aliased["Plant"] = "Fast"

	// generate the set of expected outcomes tests can glom onto
	aliasedMinusEmpty := maps.Clone(aliased)
	for dq, aliased := range aliasedMinusEmpty {
		if aliased == "" {
			delete(aliasedMinusEmpty, dq)
		}
	}

	expectedCSV := weave.ToCSV(data, slices.Collect(maps.Keys(NTDQs)), weave.CSVOptions{Aliases: aliasedMinusEmpty})
	expectedJSON, err := weave.ToJSON(data, slices.Collect(maps.Keys(NTDQs)), weave.JSONOptions{Aliases: aliasedMinusEmpty})
	if err != nil {
		t.Fatal("failed to generated expected JSON")
	}
	expectedTable := weave.ToTable(data, slices.Collect(maps.Keys(NTDQs)), weave.TableOptions{Aliases: aliasedMinusEmpty})

	tests := []struct {
		name string

		opts scaffoldlist.Options

		SetArgsTokens  []string
		wantSetArgsInv bool
		wantSetArgsErr bool

		wantOut string // the string output we want
	}{
		{"as CSV",
			scaffoldlist.Options{},
			[]string{"--csv"},
			false, false,
			expectedCSV,
		},
		{"as CSV with specified columns with alias specified",
			scaffoldlist.Options{},
			[]string{"--csv", "--columns=Fast,Robot"},
			false, false,
			"Fast,Robot\n" + "plant,[1 2 3]\n" + "plant2,[2 3 4]", // columns should be in order specified by --columns
		},
		{"as JSON",
			scaffoldlist.Options{},
			[]string{"--json"},
			false, false,
			expectedJSON,
		},
		{"as Table",
			scaffoldlist.Options{},
			[]string{"--table"},
			false, false,
			expectedTable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldlist.NewListAction("test function", "this is a test function",
				NuclearThrone{}, func(fs *pflag.FlagSet) ([]NuclearThrone, error) {
					return data, nil
				},
				maps.Clone(aliased),
				tt.opts)

			uniques.AttachPersistentFlags(pair.Action)
			inv, _, err := pair.Model.SetArgs(pair.Action.Flags(), tt.SetArgsTokens, 80, 60)
			if (err != nil) != tt.wantSetArgsErr {
				t.Errorf("bad error state. error: \"%v\"%v", err, testsupport.ExpectedActual(tt.wantSetArgsErr, (err != nil)))
			}
			if (inv != "") != tt.wantSetArgsInv {
				t.Errorf("bad invalid state. invalid: \"%v\"%v", inv, testsupport.ExpectedActual(tt.wantSetArgsErr, (err != nil)))
			}
			var gotOut = testsupport.ExtractPrintLineMessageString(t, pair.Model.Update(nil), false, 0)
			// out should be a table
			if gotOut != tt.wantOut {
				t.Error("bad Update output", testsupport.ExpectedActual(tt.wantOut, gotOut))
			}
		})

	}

}
