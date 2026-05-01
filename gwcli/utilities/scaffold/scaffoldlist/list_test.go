//go:build ci

package scaffoldlist_test

import (
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)

	m.Run()
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
		data := []nuclearThrone{} // the data itself doesn't matter

		aliased := maps.Clone(ntDQs)
		// add some aliases
		aliased["Export.YV.YungCuz"] = "YC"

		pair := scaffoldlist.NewListAction("test function", "this is a test function",
			nuclearThrone{}, func(fs *pflag.FlagSet) ([]nuclearThrone, error) {
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
	data := []nuclearThrone{
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
	aliased := maps.Clone(ntDQs)
	aliased["Plant"] = "Fast"

	tests := []struct {
		name string

		opts scaffoldlist.Options

		SetArgsTokens  []string
		wantSetArgsInv bool
		wantSetArgsErr bool

		wantOut string // the string output we want
	}{
		{"as CSV with default columns",
			scaffoldlist.Options{DefaultColumns: []string{"Plant", "Rogue"}}, // default columns should be sorted by the action
			[]string{"--csv"},
			false, false,
			"Fast,Rogue\n" +
				"plant,(0+3.14i)\n" +
				"plant2,(3.14-2.4i)",
		},
		{"as CSV with default columns out of order",
			scaffoldlist.Options{DefaultColumns: []string{"Rogue", "Plant"}}, // default columns should be sorted by the action
			[]string{"--csv"},
			false, false,
			"Fast,Rogue\n" +
				"plant,(0+3.14i)\n" +
				"plant2,(3.14-2.4i)",
		},
		{"default pretty",
			scaffoldlist.Options{Pretty: func(DQColumns []string, DQToAlias map[string]string) (string, error) { return "pretty", nil }},
			[]string{},
			false, false,
			"pretty",
		},
		{"pretty defined, but --table given",
			scaffoldlist.Options{Pretty: func(DQColumns []string, DQToAlias map[string]string) (string, error) { return "pretty", nil }},
			[]string{"--table"},
			false, false,
			"┌───────────────┬───────────────┬───────────────┬───────────────┐\n" +
				"│ Export.YV.Yun │ Fast          │ Robot         │ Rogue         │\n" +
				"├───────────────┼───────────────┼───────────────┼───────────────┤\n" +
				"│ yung cuz      │ plant         │ [1 2 3]       │ (0+3.14i)     │\n" +
				"├───────────────┼───────────────┼───────────────┼───────────────┤\n" +
				"│ yung cuz      │ plant2        │ [2 3 4]       │ (3.14-2.4i)   │\n" +
				"└───────────────┴───────────────┴───────────────┴───────────────┘",
		},
		{"as JSON with excluded defaults and pretty defined",
			scaffoldlist.Options{
				Pretty:                    func(DQColumns []string, DQToAlias map[string]string) (string, error) { return "pretty", nil },
				ExcludeColumnsFromDefault: []string{"Plant"},
			},
			[]string{"--json"}, // TODO
			false, false,
			`[` +
				`{"Export":{"YV":{"YungCuz":"yung cuz"}},"Robot":[1,2,3],"Rogue":{"Real":0,"Imaginary":3.14}},` +
				`{"Export":{"YV":{"YungCuz":"yung cuz"}},"Robot":[2,3,4],"Rogue":{"Real":3.14,"Imaginary":-2.4}}` +
				`]`, // should prefer the explicitly provided --json
		},
		{"validate args: requires exactly 1 bare arg (fail)",
			scaffoldlist.Options{ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("token"), nil
				}
				return
			}},
			[]string{},
			true, false,
			"",
		},
		{"validate args: requires exactly 1 bare arg; should continue to show-columns",
			scaffoldlist.Options{ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("token"), nil
				}
				return
			}},
			[]string{"--show-columns"}, // this should pass the validate
			false, false,
			strings.Join([]string{"Export.YV.YungCuz", "Fast", "Robot", "Rogue"}, string(scaffoldlist.ShowColumnSep)),
		},
		{"validate args: requires exactly 1 bare arg; should pass",
			scaffoldlist.Options{ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("token"), nil
				}
				return
			}},
			[]string{"--columns=Export.YV.YungCuz", "--csv", "tokens"}, // this should pass the validate
			false, false,
			"Export.YV.YungCuz\n" +
				"yung cuz\n" +
				"yung cuz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldlist.NewListAction("test function", "this is a test function",
				nuclearThrone{}, func(fs *pflag.FlagSet) ([]nuclearThrone, error) {
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
				t.Errorf("bad invalid state. invalid: \"%v\"%v", inv, testsupport.ExpectedActual(tt.wantSetArgsInv, (inv != "")))
			}

			if tt.wantSetArgsErr || tt.wantSetArgsInv {
				return
			}

			var gotOut = testsupport.ExtractPrintLineMessageString(t, pair.Model.Update(nil), false, 0)
			// out should be a table
			if gotOut != tt.wantOut {
				t.Error("bad Update output", testsupport.ExpectedActual(tt.wantOut, gotOut))
			}
		})

	}
}

// Collection of tests to check that the "CommonFields." prefix is not visible to a user.
func TestAutoAliasCommonFieldsPrefix(t *testing.T) {
	tests := []struct {
		name string

		opts scaffoldlist.Options

		SetArgsTokens  []string
		wantSetArgsInv bool
		wantSetArgsErr bool

		wantOut string // the string output we want
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldlist.NewListAction("test function", "this is a test function",
				types.Macro{}, func(fs *pflag.FlagSet) ([]types.Macro, error) {
					// generate some garbage data
					ms := make([]types.Macro, 5)
					for i := range 5 {
						iStr := strconv.FormatInt(int64(i), 10)
						ms[i] = types.Macro{
							CommonFields: types.CommonFields{
								Name:      "Name_" + iStr,
								CreatedAt: time.Unix(17500000, 0),
								ID:        iStr,
								Readers:   types.ACL{GIDs: []int32{1, 100}, Global: true},
							},
							Expansion: "Expansion_" + iStr}
					}

					return ms, nil
				},
				map[string]string{"CommonFields.ID": "ItemID"},
				tt.opts)

			uniques.AttachPersistentFlags(pair.Action)
			inv, _, err := pair.Model.SetArgs(pair.Action.Flags(), tt.SetArgsTokens, 80, 60)
			if (err != nil) != tt.wantSetArgsErr {
				t.Errorf("bad error state. error: \"%v\"%v", err, testsupport.ExpectedActual(tt.wantSetArgsErr, (err != nil)))
			}
			if (inv != "") != tt.wantSetArgsInv {
				t.Errorf("bad invalid state. invalid: \"%v\"%v", inv, testsupport.ExpectedActual(tt.wantSetArgsInv, (inv != "")))
			}

			if tt.wantSetArgsErr || tt.wantSetArgsInv {
				return
			}

			var gotOut = testsupport.ExtractPrintLineMessageString(t, pair.Model.Update(nil), false, 0)
			// out should be a table
			if gotOut != tt.wantOut {
				t.Error("bad Update output", testsupport.ExpectedActual(tt.wantOut, gotOut))
			}
		})
	}
}

// Tests to check that the generated help text properly reflects the options of each modifier.
func TestHelpGeneration(t *testing.T) {
	// generate help text we can parse
	pair := scaffoldlist.NewListAction("test function", "this is a test function",
		types.Macro{}, func(fs *pflag.FlagSet) ([]types.Macro, error) {
			// generate some garbage data
			ms := make([]types.Macro, 5)
			for i := range 5 {
				iStr := strconv.FormatInt(int64(i), 10)
				ms[i] = types.Macro{
					CommonFields: types.CommonFields{
						Name:      "Name_" + iStr,
						CreatedAt: time.Unix(17500000, 0),
						ID:        iStr,
						Readers:   types.ACL{GIDs: []int32{1, 100}, Global: true},
					},
					Expansion: "Expansion_" + iStr}
			}

			return ms, nil
		},
		map[string]string{"CommonFields.ID": "ItemID"},
		scaffoldlist.Options{
			CommonOptions:  scaffold.CommonOptions{Example: "use tkn1 tkn2 --csv"},
			DefaultColumns: []string{"CommonFields.ID", "CommonFields.Name", "Expansion"},
		},
	)

	var help string
	{
		var sb strings.Builder
		pair.Action.SetOut(&sb)
		uniques.Help(pair.Action, nil)
		help = sb.String()
	}
	if help == "" {
		t.Fatal("no help was returned")
	}

	t.Run("default columns are shown inline with --columns", func(t *testing.T) {
		// identify the end of flag usage, as default values should be listed directly after
		usageLines := strings.Split(ft.SelectColumns.Usage(), "\n")
		_, after, found := strings.Cut(help, usageLines[len(usageLines)-1])
		if !found {
			t.Fatal("failed to find the end of the usage line")
		}
		// we should find defaults appended to this line

		wantDefaultColumns := "ItemID,Name,Expansion" // should be aliases, including auto-aliasing for "CommonFields."
		if !strings.Contains(after, wantDefaultColumns) {
			t.Fatalf("default columns are not properly represented."+
				"Substring \"%v\" not found in default columns chunk \"%v\"",
				wantDefaultColumns, after)
		}
	})
	t.Run("custom example shown", func(t *testing.T) {
		// find the "example" line
		_, after, found := strings.Cut(help, "Example")
		if !found {
			t.Fatal("failed to find \"Example\" in help text")
		}
		exampleLine, _, _ := strings.Cut(after, "\n")
		if !strings.Contains(exampleLine, "use tkn1 tkn2 --csv") {
			t.Fatalf("custom example text not found in example line: %v", exampleLine)
		}
	})
}
