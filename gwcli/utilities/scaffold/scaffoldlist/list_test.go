//go:build ci

package scaffoldlist_test

import (
	"maps"
	"regexp"
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
		expected := strings.Join([]string{"a", "Clock", "long.dq.field"}, scaffoldlist.ShowColumnSep)

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
				Pretty:                         func(DQColumns []string, DQToAlias map[string]string) (string, error) { return "pretty", nil },
				DefaultColumnsFromExcludeRegex: []*regexp.Regexp{regexp.MustCompile("Plant")},
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
			strings.Join([]string{"Export.YV.YungCuz", "Fast", "Robot", "Rogue"}, scaffoldlist.ShowColumnSep),
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
		{"user selected unknown columns",
			scaffoldlist.Options{ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
				if fs.NArg() != 1 {
					return phrases.Exactly1ArgRequired("token"), nil
				}
				return
			}},
			[]string{"--columns=Export.YungVenuz,Fast,Rogue,Fish", "--csv", "tokens"}, // this should pass the validate
			true, false,
			"",
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

// Collection of tests to check that the "CommonFields." and "AutomationCommonFields." prefixes are not visible to a user.
func TestAutoAliasPrefix(t *testing.T) {
	tests := []struct {
		name string

		opts scaffoldlist.Options

		SetArgsTokens  []string
		wantSetArgsInv bool
		wantSetArgsErr bool

		wantOut string // the string output we want
	}{
		{"csv with exclude default",
			scaffoldlist.Options{
				DefaultColumnsFromExcludeRegex: []*regexp.Regexp{
					regexp.MustCompile(`^CommonFields\.LastModifiedBy`),
					regexp.MustCompile(`^CommonFields\.Can.*`),
					regexp.MustCompile(`^AutomationCommonFields\.Timezone`),
					regexp.MustCompile("^LatestResults.*"),
				}},
			[]string{"--csv"},
			false, false,
			"BackfillEnabled,Disabled,Schedule,Description,ItemID,Labels,Name,Owner.Admin,Owner.DefaultSearchGroups,Owner.Email,Owner.Groups,Owner.ID,Owner.Locked,Owner.MFA.RecoveryCodes.Codes,Owner.MFA.RecoveryCodes.Enabled,Owner.MFA.RecoveryCodes.Remaining,Owner.MFA.TOTP.Enabled,Owner.MFA.TOTP.Seed,Owner.MFA.TOTP.URL,Owner.Name,Owner.SearchPriority,Owner.SSOUser,Owner.Username,OwnerID,ParentID,Readers.GIDs,Readers.Global,Type,Version,Writers.GIDs,Writers.Global,Flow\n" +
				"false,false,,,0,[],Name_0,false,[],,[],0,false,[],false,0,false,,,,0,false,,0,,[1 100],true,,0,[],false,Flow_0\n" +
				"false,false,,,1,[],Name_1,false,[],,[],0,false,[],false,0,false,,,,0,false,,0,,[1 100],true,,0,[],false,Flow_1\n" +
				"false,false,,,2,[],Name_2,false,[],,[],0,false,[],false,0,false,,,,0,false,,0,,[1 100],true,,0,[],false,Flow_2\n" +
				"false,false,,,3,[],Name_3,false,[],,[],0,false,[],false,0,false,,,,0,false,,0,,[1 100],true,,0,[],false,Flow_3\n" +
				"false,false,,,4,[],Name_4,false,[],,[],0,false,[],false,0,false,,,,0,false,,0,,[1 100],true,,0,[],false,Flow_4",
		},
		{"csv with exclude default CommonFields.* (should exclude all CommonFields and AutomationCommonFields)",
			scaffoldlist.Options{
				DefaultColumnsFromExcludeRegex: []*regexp.Regexp{
					regexp.MustCompile(`CommonFields\..*`),
					regexp.MustCompile("^LatestResults.*"),
				}},
			[]string{"--csv"},
			false, false,
			"Flow\n" +
				"Flow_0\n" +
				"Flow_1\n" +
				"Flow_2\n" +
				"Flow_3\n" +
				"Flow_4",
		},
		{"json with exclude default CommonFields.* (should exclude all CommonFields and AutomationCommonFields)",
			scaffoldlist.Options{
				DefaultColumnsFromExcludeRegex: []*regexp.Regexp{
					regexp.MustCompile("CommonFields.*"),
					regexp.MustCompile("^LatestResults.*"),
				}},
			[]string{"--json"},
			false, false,
			`[` +
				`{"Flow":"Flow_0"},` +
				`{"Flow":"Flow_1"},` +
				`{"Flow":"Flow_2"},` +
				`{"Flow":"Flow_3"},` +
				`{"Flow":"Flow_4"}` + `]`,
		},
		{"json exclude defaults ignored by --columns",
			scaffoldlist.Options{
				DefaultColumnsFromExcludeRegex: []*regexp.Regexp{
					regexp.MustCompile("CommonFields.*"),
					regexp.MustCompile("^LatestResults.*"),
				}},
			[]string{"--json", "--columns=ItemID,Name,Flow"},
			false, false,
			`[` +
				`{"Flow":"Flow_0","ItemID":"0","Name":"Name_0"},` +
				`{"Flow":"Flow_1","ItemID":"1","Name":"Name_1"},` +
				`{"Flow":"Flow_2","ItemID":"2","Name":"Name_2"},` +
				`{"Flow":"Flow_3","ItemID":"3","Name":"Name_3"},` +
				`{"Flow":"Flow_4","ItemID":"4","Name":"Name_4"}` + `]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldlist.NewListAction("test function", "this is a test function",
				types.Flow{}, func(fs *pflag.FlagSet) ([]types.Flow, error) {
					// generate some garbage data
					ms := make([]types.Flow, 5)
					for i := range 5 {
						iStr := strconv.FormatInt(int64(i), 10)
						ms[i] = types.Flow{
							CommonFields: types.CommonFields{
								Name:      "Name_" + iStr,
								CreatedAt: time.Unix(17500000, 0),
								ID:        iStr,
								Readers:   types.ACL{GIDs: []int32{1, 100}, Global: true},
							},
							Flow: "Flow_" + iStr}
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
