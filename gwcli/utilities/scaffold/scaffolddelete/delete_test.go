//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffolddelete_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/bubbles/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffolddelete"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	clilog.InitializeFromArgs(nil)
	m.Run()
}

// #region test helpers

func del(dryrun bool, ID string) error {
	if strings.Contains(ID, "bad") {
		return errors.New("unknown item (" + ID + ") in the collection")
	}
	return nil
}

func collectItems() ([]multiselectlist.SelectableItem[string], error) {
	return []multiselectlist.SelectableItem[string]{
		&multiselectlist.DefaultSelectableItem[string]{
			Title_:       "Alpha",
			Description_: "first item",
			ID_:          "alpha",
		},
		&multiselectlist.DefaultSelectableItem[string]{
			Title_:       "Beta",
			Description_: "second item",
			ID_:          "beta",
		},
		&multiselectlist.DefaultSelectableItem[string]{
			Title_:       "Gamma",
			Description_: "third item",
			ID_:          "gamma",
		},
	}, nil
}

// #endregion

func TestNonInteractive(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr string
	}{
		{"delete single item", []string{"alpha"}, fmt.Sprintf(scaffolddelete.DeleteSuccessTextF, "widget", "alpha"), ""},
		{"delete multiple items", []string{"alpha", "beta"}, fmt.Sprintf(scaffolddelete.DeleteSuccessTextF, "widget", "alpha") + "\n" + fmt.Sprintf(scaffolddelete.DeleteSuccessTextF, "widget", "beta"), ""},
		{"dryrun single item", []string{"--dryrun", "alpha"}, fmt.Sprintf(scaffolddelete.DryrunSuccessTextF, "widget", "alpha"), ""},
		{"dryrun multiple items", []string{"--dryrun", "alpha", "gamma"},
			fmt.Sprintf(scaffolddelete.DryrunSuccessTextF, "widget", "alpha") + "\n" + fmt.Sprintf(scaffolddelete.DryrunSuccessTextF, "widget", "gamma"), ""},
		{"delete none", nil, "", "you must specify at least 1 argument"},
		{"delete unknown item", []string{"bad"}, "", "unknown item (bad) in the collection"},
		{"multiple unknown items", []string{"bad", "bad2"}, "", "failed to delete widget (ID bad): unknown item (bad) in the collection\nfailed to delete widget (ID bad2): unknown item (bad2) in the collection\nError: all operations failed all operations failed"},
		{"one good one bad", []string{"alpha", "bad"}, fmt.Sprintf(scaffolddelete.DeleteSuccessTextF, "widget", "alpha"), "unknown item (bad)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sbOut, sbErr strings.Builder
			pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
			uniques.AttachPersistentFlags(pair.Action)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)
			pair.Action.SetArgs(append(tt.args, "-x"))
			err := pair.Action.Execute()
			stdout, stderr := strings.TrimSpace(sbOut.String()), strings.TrimSpace(sbErr.String())
			if tt.wantErr != "" {
				errStr := stderr
				if err != nil {
					errStr = errStr + " " + err.Error()
				}
				assert.Contains(t, errStr, tt.wantErr)
				return
			}
			assert.Nil(t, err)
			assert.Empty(t, stderr)
			assert.Equal(t, tt.wantOut, stdout)
		})
	}
}

func TestInteractiveCycle(t *testing.T) {
	t.Run("no data returns done with message", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del,
			func() ([]multiselectlist.SelectableItem[string], error) { return nil, nil },
			scaffolddelete.Options{})
		inv, cmd, err := pair.Model.SetArgs(nil, []string{}, 50, 20)
		assert.Empty(t, inv)
		assert.Nil(t, err)
		assert.NotNil(t, cmd)
		assert.True(t, pair.Model.Done())
	})

	t.Run("fetch error", func(t *testing.T) {
		fetchErr := errors.New("network error")
		pair := scaffolddelete.NewDeleteAction("widget", del,
			func() ([]multiselectlist.SelectableItem[string], error) { return nil, fetchErr },
			scaffolddelete.Options{})
		_, _, err := pair.Model.SetArgs(nil, []string{}, 50, 20)
		assert.ErrorIs(t, err, fetchErr)
	})

	t.Run("with items, no flags (interactive mode)", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
		testsupport.CheckSetArgs(t, pair.Model.SetArgs, nil, []string{}, 50, 20, false, nil, false)

		t.Run("check initial view", func(t *testing.T) {
			pair.Model.Update(nil)
			v := testsupport.LinesTrimSpace(pair.Model.View())
			assert.Contains(t, v, "Alpha")
			assert.Contains(t, v, "Beta")
			assert.Contains(t, v, "Gamma")
		})

		t.Run("select Beta and submit through confirmation", func(t *testing.T) {
			// Move down to Beta and select it
			pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
			pair.Model.Update(testsupport.SendHotkey(hotkeys.Select))
			// Submit selection (Invoke continues to confirmation)
			pair.Model.Update(testsupport.SendHotkey(hotkeys.Invoke))
			// check confirmation view for the Beta ID
			v := pair.Model.View()
			assert.Contains(t, v, "Deleting 1 widget:")
			assert.Contains(t, v, "Beta")
			// Now in confirmation mode - submit the confirmation
			cmd := pair.Model.Update(testsupport.SendHotkey(hotkeys.Invoke))
			assert.True(t, pair.Model.Done())
			if cmd != nil {
				msg := testsupport.ExtractPrintLineMessageString(t, cmd, true, 0)
				assert.Contains(t, msg, "beta")
			}
		})
	})

	t.Run("IDs via bare args skip interactive", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
		inv, cmd, err := pair.Model.SetArgs(nil, []string{"alpha", "beta"}, 50, 20)
		assert.Empty(t, inv)
		assert.Nil(t, err)
		assert.True(t, pair.Model.Done())
		assert.NotNil(t, cmd)
	})

	t.Run("bad flags returns invalid", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
		inv, _, err := pair.Model.SetArgs(nil, []string{"--nonexistent"}, 50, 20)
		assert.Nil(t, err)
		assert.NotEmpty(t, inv)
	})
}

func TestModelLifecycle(t *testing.T) {
	t.Run("reset after done", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
		_, _, err := pair.Model.SetArgs(nil, []string{"alpha"}, 50, 20)
		assert.Nil(t, err)
		assert.True(t, pair.Model.Done())

		assert.Nil(t, pair.Model.Reset())
		assert.False(t, pair.Model.Done())
	})

	t.Run("repeated use", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})

		// First use
		_, _, err := pair.Model.SetArgs(nil, []string{"alpha"}, 50, 20)
		assert.Nil(t, err)
		assert.True(t, pair.Model.Done())
		assert.Nil(t, pair.Model.Reset())

		// Second use with different args
		_, _, err = pair.Model.SetArgs(nil, []string{"beta", "gamma"}, 50, 20)
		assert.Nil(t, err)
		assert.True(t, pair.Model.Done())
	})

	t.Run("view when done", func(t *testing.T) {
		pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})
		_, _, err := pair.Model.SetArgs(nil, []string{"alpha"}, 50, 20)
		assert.Nil(t, err)
		assert.NotEmpty(t, pair.Model.View())
	})
}

func TestOptions(t *testing.T) {
	pair := scaffolddelete.NewDeleteAction("widget", del, collectItems, scaffolddelete.Options{})

	assert.Equal(t, "delete", pair.Action.Use)
	assert.Contains(t, pair.Action.Short, "widgets")
	assert.NotNil(t, pair.Action.Flags().Lookup("dryrun"))
}
