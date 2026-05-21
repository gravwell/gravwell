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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldselect"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)
	m.Run()
}

type party struct {
	number int

	highwayman string
	alchemist  uint
}

func collectItems() ([]multiselectlist.SelectableItem[int], error) {
	return []multiselectlist.SelectableItem[int]{
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
	}, nil
}

func operate(ID int) (success string, _ error) {
	if ID < 10 {
		return "recruited vagrant " + strconv.FormatInt(int64(ID), 10), nil
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
		{"select both items", []string{"1", "2"}, "recruited vagrant 1\nrecruited vagrant 2", ""},
		{"select none", nil, "", "you must specify at least 1 argument"},
		{"select unknown", []string{"100"}, "", "unknown vagrant (100) on the old road"},
		{"select unparsable", []string{"Crusader"}, "", "Crusader is not a valid item"}, // TODO
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sbOut, sbErr strings.Builder
			pair := scaffoldselect.NewSelectAction("short test", "long test", "item", "items",
				collectItems, operate, scaffoldselect.Options{})
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
			func() ([]multiselectlist.SelectableItem[int], error) {
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
