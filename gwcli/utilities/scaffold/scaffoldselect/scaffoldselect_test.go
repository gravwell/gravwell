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
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)
	m.Run()
}

func operate(ID int, afs *pflag.FlagSet) (success string, _ error) {
	isCursed, err := afs.GetBool("cursed")
	if err != nil {
		clilog.GetFlag(err)
	}
	var cursed string
	if isCursed {
		cursed = ", but they carry the crimson curse!"
	}
	if ID < 10 {
		return "recruited vagrant " + strconv.FormatInt(int64(ID), 10) + cursed, nil
	}
	return "", errors.New("unknown vagrant (" + strconv.FormatInt(int64(ID), 10) + ") on the old road")
}

func TestNonInteractive(t *testing.T) {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sbOut, sbErr strings.Builder
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item", "items",
				func(addtlFlags *pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) { return nil, nil }, operate, scaffoldselect.Options{
					CommonOptions: scaffold.CommonOptions{
						AddtlFlags: func() *pflag.FlagSet {
							fs := &pflag.FlagSet{}
							fs.Bool("cursed", false,
								"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
							return fs
						},
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

	t.Run("items collected should have no effect in a non-interactive run", func(t *testing.T) {
		var sbOut, sbErr strings.Builder
		pair := scaffoldselect.NewSelectAction("short test", "long test", "item", "items",
			func(*pflag.FlagSet) ([]multiselectlist.SelectableItem[int], error) {
				return nil, nil
			}, operate, scaffoldselect.Options{})
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
	}
	return data, nil
}

func TestInteractiveCycle(t *testing.T) {
	t.Run("without cursed", func(t *testing.T) {
		pair := scaffoldselect.NewSelectAction("short test", "long test", "item", "items",
			collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("cursed", false,
						"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
					fs.Uint("single", 0, "select a single vagabond instead of the whole wagon.")
					return fs
				},
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
		pair := scaffoldselect.NewSelectAction("short test", "long test", "item", "items",
			collectItems, operate, scaffoldselect.Options{CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("cursed", false,
						"These swarming fiends carry a pernicious plague! A sickness so virulent, so insidious, it is more a curse than a mere disease.")
					fs.Uint("single", 0, "select a single vagabond instead of the whole wagon.")
					return fs
				},
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
}
