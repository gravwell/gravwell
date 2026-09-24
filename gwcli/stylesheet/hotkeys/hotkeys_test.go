/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hotkeys_test

import (
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/stretchr/testify/assert"
)

func TestMoveCursor(t *testing.T) {
	// set up some TAs we can test against
	emptyTA, atTopTA, atBottomTA, atCenterTA := textarea.New(), textarea.New(), textarea.New(), textarea.New()
	emptyTA.Focus()
	t.Logf("emptyTA row: %d/%d", emptyTA.Line(), emptyTA.LineCount()-1)

	atTopTA.SetValue(
		randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n")
	atTopTA.CursorUp()
	atTopTA.CursorUp()
	atTopTA.CursorUp()
	atTopTA.Focus()
	t.Logf("atTopTA row: %d/%d", atTopTA.Line(), atTopTA.LineCount()-1)

	atBottomTA.SetValue(
		randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n")
	atBottomTA.CursorDown()
	atBottomTA.Focus()
	t.Logf("atBottomTA row: %d/%d", atBottomTA.Line(), atBottomTA.LineCount()-1)

	atCenterTA.SetValue(
		randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n" +
			randomdata.SillyName() + "\n")
	atCenterTA.CursorUp()
	atCenterTA.Focus()
	t.Logf("atCenter row: %d/%d", atCenterTA.Line(), atCenterTA.LineCount()-1)

	// one-off tests
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		msg          tea.Msg
		currentIndex uint
		fieldCount   uint
		selectedTA   *textarea.Model
		wantHandled  bool
		wantUpdateTA bool
		wantNewIndex uint
	}{
		{"wrap bottom to top",
			testsupport.SendHotkey(hotkeys.CursorDown), 8, 9, nil,
			true, false, 0,
		},
		{"wrap bottom to top, TA given",
			testsupport.SendHotkey(hotkeys.CursorDown), 8, 9, &textarea.Model{},
			true, false, 0,
		},
		{"wrap top to bottom",
			testsupport.SendHotkey(hotkeys.CursorUp), 0, 9, nil,
			true, false, 8,
		},
		{"wrap top to bottom, TA given",
			testsupport.SendHotkey(hotkeys.CursorUp), 0, 9, &textarea.Model{},
			true, false, 8,
		},
		{"not a cursor key",
			testsupport.SendHotkey(hotkeys.Invoke), 0, 9, &textarea.Model{},
			false, false, 0,
		},
		{"down on empty TA moves to next field",
			testsupport.SendHotkey(hotkeys.CursorDown), 3, 9, &emptyTA,
			true, false, 4,
		},
		{"up on empty TA moves to prev field",
			testsupport.SendHotkey(hotkeys.CursorUp), 3, 9, &emptyTA,
			true, false, 2,
		},
		{"down on atTopTA moves internally",
			testsupport.SendHotkey(hotkeys.CursorDown), 3, 9, &atTopTA,
			false, true, 3,
		},
		{"up on atTopTA moves to prev field",
			testsupport.SendHotkey(hotkeys.CursorUp), 3, 9, &atTopTA,
			true, false, 2,
		},
		{"down on atBottomTA moves to next field",
			testsupport.SendHotkey(hotkeys.CursorDown), 3, 9, &atBottomTA,
			true, false, 4,
		},
		{"up on atBottomTA moves internally",
			testsupport.SendHotkey(hotkeys.CursorUp), 3, 9, &atBottomTA,
			false, true, 3,
		},
		{"down on atCenter moves internally",
			testsupport.SendHotkey(hotkeys.CursorDown), 3, 9, &atCenterTA,
			false, true, 3,
		},
		{"up on atCenter moves internally",
			testsupport.SendHotkey(hotkeys.CursorUp), 3, 9, &atCenterTA,
			false, true, 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHandled, gotUpdateTA, gotNewIndex := hotkeys.MoveCursor(tt.msg, tt.currentIndex, tt.fieldCount, tt.selectedTA)
			assert.Equal(t, tt.wantHandled, gotHandled)
			assert.Equal(t, tt.wantUpdateTA, gotUpdateTA)
			assert.Equal(t, tt.wantNewIndex, gotNewIndex)
		})
	}

	t.Run("wrap down from 0 to 0", func(t *testing.T) {
		var cur, fieldCount uint = 0, 5
		for i := range fieldCount {
			assert.Equal(t, i, cur)
			handled, updateTA, newIndex := hotkeys.MoveCursor(testsupport.SendHotkey(hotkeys.CursorDown), cur, fieldCount, nil)
			assert.True(t, handled)
			assert.False(t, updateTA)
			cur = newIndex
		}
		assert.EqualValues(t, 0, cur)
	})
	t.Run("wrap up from 0 to 0", func(t *testing.T) {
		var cur, fieldCount uint = 4, 5
		for i := fieldCount - 1; i > 0; i -= 1 {
			assert.Equal(t, i, cur)
			handled, updateTA, newIndex := hotkeys.MoveCursor(testsupport.SendHotkey(hotkeys.CursorUp), cur, fieldCount, nil)
			assert.True(t, handled)
			assert.False(t, updateTA)
			cur = newIndex
		}
		assert.EqualValues(t, 0, cur)
	})
}
