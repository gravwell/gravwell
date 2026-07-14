/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldselect_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/cmdutils"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/phrases"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)
	m.Run()
}

func TestOptions(t *testing.T) {
	t.Run("All options are applied automatically", func(t *testing.T) {
		pair := scaffoldselect.NewSelectAction("test", "test", "item",
			func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) { return nil, nil },
			func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) { return nil, nil },
			scaffoldselect.Options{
				CommonOptions: scaffold.CommonOptions{
					Aliases:   []string{"a", "b", "a"},
					AdminOnly: true,
				},
			})
		assert.Equal(t, []string{"a", "b"}, pair.Action.Aliases)
		assert.True(t, cmdutils.IsAdminOnly(pair.Action))
	})
	t.Run("Options are ignored if not set", func(t *testing.T) {
		pair := scaffoldselect.NewSelectAction("test", "test", "item",
			func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[string], error) { return nil, nil },
			func(IDs []string, addtlFlags *pflag.FlagSet) (results []scaffold.Result, _ error) { return nil, nil },
			scaffoldselect.Options{})
		assert.Equal(t, []string{}, pair.Action.Aliases)
		assert.Len(t, pair.Action.Annotations, 0)
	})
}

var errTestFatal = errors.New("fatal triggered")

func operate(IDs []int, afs *pflag.FlagSet) (_ []scaffold.Result, _ error) {
	if fatal, err := afs.GetBool("fatal"); err != nil {
		return nil, errors.New("fail to fetch fatal flag! Something is broken in the test!")
	} else if fatal {
		return nil, errTestFatal
	}
	var cursed string
	if isCursed, err := afs.GetBool("cursed"); err != nil {
		clilog.GetFlag(err)
	} else if isCursed {
		cursed = ", but they carry the crimson curse!"
	}
	results := make([]scaffold.Result, len(IDs))
	for i, ID := range IDs {
		var r = scaffold.Result{}
		if ID < 10 {
			r.Output = "recruited vagrant " + strconv.FormatInt(int64(ID), 10) + cursed
			r.Success = true
		} else {
			r.Output = "unknown vagrant (" + strconv.FormatInt(int64(ID), 10) + ") on the old road"
		}

		results[i] = r
	}
	return results, nil
}

func TestNonInteractive(t *testing.T) {
	afsFunc := func() *pflag.FlagSet {
		fs := &pflag.FlagSet{}
		fs.Bool("cursed", false,
			"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
		fs.Bool("fatal", false,
			"Another life wasted in the pursuit of glory and gold.")
		return fs
	}
	t.Run("1+", func(t *testing.T) {
		tests := []struct {
			name    string
			args    []string
			wantOut string
			wantErr string
		}{
			{"select item 1", []string{"1"}, "recruited vagrant 1", ""},
			{"select item 2", []string{"2"}, "recruited vagrant 2", ""},
			{"select item 2 with curse!", []string{"--cursed", "2"}, "recruited vagrant 2, but they carry the crimson curse!", ""},
			{"select both items", []string{"1", "2"}, "recruited vagrant 1\nrecruited vagrant 2", ""},
			{"select none", nil, "", "you must specify at least 1 argument"},
			{"select unknown", []string{"100"}, "", "unknown vagrant (100) on the old road"},
			{"select unparsable", []string{"Crusader"}, "", "Crusader is not a valid item"},
			{"invalid arguments", []string{"1", "3", "5", "7", "9"}, "", "the party may contain no more than 4 members"},
			{"trigger fatal (in op, once args are validated)", []string{"--fatal", "2"}, "", errTestFatal.Error()},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var sbOut, sbErr strings.Builder
				pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
					func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) { return nil, nil }, operate, scaffoldselect.Options{
						CommonOptions: scaffold.CommonOptions{
							AddtlFlags: afsFunc,
						},
						ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
							if fs.NArg() > 4 {
								return "the party may contain no more than 4 members", nil
							}
							return "", nil
						},
					})
				uniques.AttachPersistentFlags(pair.Action)
				pair.Action.SetOut(&sbOut)
				pair.Action.SetErr(&sbErr)
				pair.Action.SetArgs(append(tt.args, "-x"))
				err := pair.Action.Execute()
				stdout, stderr := strings.TrimSpace(sbOut.String()), strings.TrimSpace(sbErr.String())
				if tt.wantErr != "" {
					assert.NotNil(t, err)
					assert.Contains(t, stderr, tt.wantErr)
					return
				}
				assert.Nil(t, err)
				assert.Empty(t, stderr)
				assert.Equal(t, tt.wantOut, stdout)
			})
		}
	})
	t.Run("exactly 1", func(t *testing.T) {
		tests := []struct {
			name    string
			args    []string
			wantOut string
			wantErr string
		}{
			{"select item 1", []string{"1"}, "recruited vagrant 1", ""},
			{"select item 2", []string{"2"}, "recruited vagrant 2", ""},
			{"select item 2 with curse!", []string{"--cursed", "2"}, "recruited vagrant 2, but they carry the crimson curse!", ""},
			{"select both items", []string{"1", "2"}, "", phrases.Exactly1ArgRequired("item")},
			{"select none", nil, "", phrases.Exactly1ArgRequired("item")},
			{"select unknown", []string{"100"}, "", "unknown vagrant (100) on the old road"},
			{"select unparsable", []string{"Crusader"}, "", "Crusader is not a valid item"},
			{"invalid arguments", []string{"1", "3", "5", "7", "9"}, "", "the party may contain no more than 4 members"}, // validate args is checked first
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var sbOut, sbErr strings.Builder
				pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
					func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) { return nil, nil }, operate, scaffoldselect.Options{
						CommonOptions: scaffold.CommonOptions{
							AddtlFlags: afsFunc,
						},
						ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
							if fs.NArg() > 4 {
								return "the party may contain no more than 4 members", nil
							}
							return "", nil
						},
						Exactly1: true,
					})
				uniques.AttachPersistentFlags(pair.Action)
				pair.Action.SetOut(&sbOut)
				pair.Action.SetErr(&sbErr)
				pair.Action.SetArgs(append(tt.args, "-x"))
				err := pair.Action.Execute()
				stdout, stderr := strings.TrimSpace(sbOut.String()), strings.TrimSpace(sbErr.String())
				if tt.wantErr != "" {
					assert.NotNil(t, err)
					assert.Contains(t, stderr, tt.wantErr)
					return
				}
				assert.Nil(t, err)
				assert.Empty(t, stderr)
				assert.Equal(t, tt.wantOut, stdout)
			})
		}
	})

	t.Run("items collected should have no effect in a non-interactive run", func(t *testing.T) {
		var sbOut, sbErr strings.Builder
		pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
			func(*pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) {
				return nil, nil
			}, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{AddtlFlags: afsFunc}})
		uniques.AttachPersistentFlags(pair.Action)
		pair.Action.SetOut(&sbOut)
		pair.Action.SetErr(&sbErr)
		pair.Action.SetArgs([]string{"-x", "8"})
		assert.Nil(t, pair.Action.Execute())
		assert.Empty(t, sbErr.String())
		assert.Equal(t, "recruited vagrant 8", strings.TrimSpace(sbOut.String()))
	})
}

func collectItems(fs *pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) {
	sng, err := fs.GetUint("single")
	if err != nil {
		return nil, err
	}
	noData, err := fs.GetBool("no-data")
	if err != nil {
		return nil, err
	}
	data := []multiselectlist.SelectableItem[int]{
		&multiselectlist.DefaultSelectableItem[int]{
			Title_:       "Highwayman",
			Description_: "A bandit seeking redemption on an old road",
			Selected_:    false,
			ID_:          1,
		},
		&multiselectlist.DefaultSelectableItem[int]{
			Title_:       "Plague Doctor",
			Description_: "A doctor, researcher and alchemist who prefers to hang back...",
			Selected_:    false,
			ID_:          2,
		},
		&multiselectlist.DefaultSelectableItem[int]{
			Title_:       "Vestal",
			Description_: "A sister of battle. Pious and unrelenting.",
			Selected_:    true,
			ID_:          3,
		},
		&multiselectlist.DefaultSelectableItem[int]{
			Title_:       "Leper",
			Description_: "This man understands that adversity and existence are one and the same.",
			Selected_:    false,
			ID_:          4,
		},
	}

	if fs.Changed("single") && int(sng) < len(data) {
		return []multiselectlist.SelectableItem[int]{data[sng]}, nil
	} else if noData {
		return nil, nil
	}
	return data, nil
}

func TestInteractiveCycle(t *testing.T) {
	afsFunc := func() *pflag.FlagSet {
		fs := &pflag.FlagSet{}
		fs.Bool("cursed", false,
			"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
		fs.Uint("single", 0, "select a single vagabond instead of the whole wagon.")
		fs.Bool("no-data", false, "try to leave without a party in tow.")
		fs.Bool("fatal", false,
			"Another life wasted in the pursuit of glory and gold.")
		return fs
	}
	t.Run("1+", func(t *testing.T) {
		t.Run("no data returned uses custom EmptyError", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
					AddtlFlags: afsFunc,
				},
					NoItemsError: func(fs *pflag.FlagSet) string { return "you cannot leave with an empty party!" }})
			args := []string{"--no-data"}
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, false, nil, true)
		})
		t.Run("validate func is called and can fail out properly", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
					AddtlFlags: afsFunc,
				},
					ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
						switch len(fs.Args()) {
						case 1:
							return "only one argument means invalid!", nil
						case 2:
							return "", errors.New("but two arguments is a hard error!")
						}
						return "", nil
					},
				},
			)
			t.Run("invalid", func(t *testing.T) {
				args := []string{"--no-data", "one"}
				testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, true, nil, false)
			})
			t.Run("error", func(t *testing.T) {
				args := []string{"--no-data", "one", "two"}
				testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, false, nil, true)
			})

		})
		t.Run("without cursed", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
					AddtlFlags: afsFunc,
				}})
			args := []string{}
			wantInv := false
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, wantInv, nil, false)
			t.Run("check initial view", func(t *testing.T) {
				pair.Model.Update(nil)
				v := testsupport.LinesTrimSpace(pair.Model.View())
				want := testsupport.LinesTrimSpace(`   List

		4 items

		│ [ ] Highwayman
		│ A bandit seeking redemption on an old road

 		[ ] Plague Doctor
		A doctor, researcher and alchemist who prefers …

		[✓] Vestal
		A sister of battle. Pious and unrelenting.
		
		[ ] Leper
		This man understands that adversity and existen…
        



		↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
		space select • ↲ continue`)

				assert.Equal(t, want, v)
			})
			t.Run("select plague doctor and submit", func(t *testing.T) {
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				pair.Model.Update(testsupport.SendHotkey(hotkeys.Select))
				cmd := pair.Model.Update(testsupport.SendHotkey(hotkeys.Invoke))
				l1 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 0)
				l2 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 1)
				assert.Equal(t, "recruited vagrant 2", l1)
				assert.Equal(t, "recruited vagrant 3", l2)
			})
		})
		t.Run("with cursed", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
					AddtlFlags: afsFunc,
				}})
			args := []string{"--cursed"}
			wantInv := false
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, wantInv, nil, false)
			t.Run("check initial view", func(t *testing.T) {
				pair.Model.Update(nil)
				v := testsupport.LinesTrimSpace(pair.Model.View())
				want := testsupport.LinesTrimSpace(`   List

		4 items

		│ [ ] Highwayman
		│ A bandit seeking redemption on an old road

 		[ ] Plague Doctor
		A doctor, researcher and alchemist who prefers …

		[✓] Vestal
		A sister of battle. Pious and unrelenting.
		
		[ ] Leper
		This man understands that adversity and existen…
        



		↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
		space select • ↲ continue`)

				assert.Equal(t, want, v)
			})
			t.Run("select plague doctor and submit", func(t *testing.T) {
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				pair.Model.Update(testsupport.SendHotkey(hotkeys.Select))
				cmd := pair.Model.Update(testsupport.SendHotkey(hotkeys.Invoke))
				l1 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 0)
				l2 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 1)
				assert.Equal(t, "recruited vagrant 2, but they carry the crimson curse!", l1)
				assert.Equal(t, "recruited vagrant 3, but they carry the crimson curse!", l2)
			})
		})
	})
	t.Run("exactly 1", func(t *testing.T) {
		t.Run("no data returned uses custom EmptyError", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{
					CommonOptions: scaffold.CommonOptions{
						AddtlFlags: func() *pflag.FlagSet {
							fs := &pflag.FlagSet{}
							fs.Bool("cursed", false,
								"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
							fs.Uint("single", 0, "select a single vagabond instead of the whole wagon.")
							fs.Bool("no-data", false, "try to leave without a party in tow.")
							return fs
						},
					},
					NoItemsError: func(fs *pflag.FlagSet) string { return "you cannot leave with an empty party!" },
					Exactly1:     true,
				})
			args := []string{"--no-data"}
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, false, nil, true)
		})
		t.Run("without cursed", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{
					CommonOptions: scaffold.CommonOptions{
						AddtlFlags: afsFunc,
					},
					Exactly1: true,
				})
			args := []string{}
			wantInv := false
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, wantInv, nil, false)
			t.Run("check initial view", func(t *testing.T) {
				pair.Model.Update(nil)
				v := testsupport.LinesTrimSpace(pair.Model.View())
				want := testsupport.LinesTrimSpace(`
				4 items

        │ Highwayman
        │ A bandit seeking redemption on an old road

        Plague Doctor
        A doctor, researcher and alchemist who prefers …

        Vestal
        A sister of battle. Pious and unrelenting.

        Leper
        This man understands that adversity and existen…





        ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more`)
				assert.Equal(t, want, v)
			})
			t.Run("select plague doctor and submit", func(t *testing.T) {
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				cmd := pair.Model.Update(testsupport.SendHotkey(hotkeys.Select))
				require.NotNil(t, cmd)
				l1 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 0)
				assert.Equal(t, "recruited vagrant 2", l1)
			})
		})
		t.Run("with cursed", func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{
					CommonOptions: scaffold.CommonOptions{
						AddtlFlags: afsFunc,
					},
					Exactly1: true,
				})
			args := []string{"--cursed"}
			wantInv := false
			testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, args, 50, 20, wantInv, nil, false)
			t.Run("check initial view", func(t *testing.T) {
				pair.Model.Update(nil)
				v := testsupport.LinesTrimSpace(pair.Model.View())
				want := testsupport.LinesTrimSpace(`
				4 items

        │ Highwayman
        │ A bandit seeking redemption on an old road

        Plague Doctor
        A doctor, researcher and alchemist who prefers …

        Vestal
        A sister of battle. Pious and unrelenting.

        Leper
        This man understands that adversity and existen…





        ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more`)

				assert.Equal(t, want, v)
			})
			t.Run("select leper and submit", func(t *testing.T) {
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
				cmd := pair.Model.Update(testsupport.SendHotkey(hotkeys.Select))
				require.NotNil(t, cmd)
				l1 := testsupport.ExtractPrintLineMessageString(t, cmd, true, 0)
				assert.Equal(t, "recruited vagrant 4, but they carry the crimson curse!", l1)
			})
		})
	})
}

// These tests ensure that SetArgs can operate autonomously if given enough information.
func TestInteractiveDirectInvoke(t *testing.T) {

	tests := []struct {
		name               string
		exactly1           bool
		args               []string
		wantInv            string
		wantOnStartStrings []string
		wantErr            string
	}{
		{"exactly1: 1 arg",
			true,
			[]string{"3"}, "", []string{"recruited vagrant 3"}, ""},
		{"exactly1: 1 arg and --cursed",
			true,
			[]string{"--cursed", "1"}, "", []string{"recruited vagrant 1, but they carry the crimson curse!"}, ""},
		{"exactly1: 3 args",
			true,
			[]string{"1", "2", "4"}, phrases.Exactly1ArgRequired("item"), nil, ""},
		{"1 arg",
			false,
			[]string{"3"}, "", []string{"recruited vagrant 3"}, ""},
		{"1 arg and --cursed",
			false,
			[]string{"--cursed", "1"}, "", []string{"recruited vagrant 1, but they carry the crimson curse!"}, ""},
		{"3 args",
			false,
			[]string{"1", "2", "4"}, "", []string{"recruited vagrant 1", "recruited vagrant 2", "recruited vagrant 4"}, ""},
		{"3 args and --cursed",
			false,
			[]string{"--cursed", "1", "2", "4"}, "", []string{
				"recruited vagrant 1, but they carry the crimson curse!",
				"recruited vagrant 2, but they carry the crimson curse!",
				"recruited vagrant 4, but they carry the crimson curse!"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item",
				collectItems, operate, scaffoldselect.Options{
					CommonOptions: scaffold.CommonOptions{
						AddtlFlags: func() *pflag.FlagSet {
							fs := &pflag.FlagSet{}
							fs.Bool("cursed", false,
								"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
							fs.Uint("single", 0, "select a single vagabond instead of the whole wagon.")
							fs.Bool("no-data", false, "try to leave without a party in tow.")
							fs.Bool("fatal", false,
								"Another life wasted in the pursuit of glory and gold.")
							return fs
						},
					},
					Exactly1: tt.exactly1,
				})
			inv, onStart, err := pair.Model.SetArgs(nil, tt.args, 50, 20)
			assert.Equal(t, tt.wantInv, inv)
			if len(tt.wantOnStartStrings) < 1 {
				assert.Nil(t, onStart)
			} else {
				assert.NotNil(t, onStart)
				for i, s := range tt.wantOnStartStrings {
					got := testsupport.ExtractPrintLineMessageString(t, onStart, true, uint(i))
					assert.Equal(t, s, got)
				}
			}
			if tt.wantErr == "" {
				assert.Nil(t, err)
			} else {
				require.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
