/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package confirmation_test

import (
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/bubbles/confirmation"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/stretchr/testify/assert"
)

func TestXxx(t *testing.T) {
	m := confirmation.Model{HeaderLines: []string{"header1", "header2"}}
	m.Init([]string{"a", "b", "c"}, 80, 60)
	t.Run("initial view", func(t *testing.T) {
		v := testsupport.LinesTrimSpace(m.View())
		want := testsupport.LinesTrimSpace(`        header1
                header2

                     Return to:
                         ╭─╮
                         │a│
                         ╰─╯
         ╭──────╮        ╭─╮
        >│submit│  or    │b│
         ╰──────╯        ╰─╯
                         ╭─╮
                         │c│
                         ╰─╯
          Press esc to cancel  `)
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("ineffectual cursor up/down on submit", func(t *testing.T) {
		want := testsupport.LinesTrimSpace(`        header1
                header2

                     Return to:
                         ╭─╮
                         │a│
                         ╰─╯
         ╭──────╮        ╭─╮
        >│submit│  or    │b│
         ╰──────╯        ╰─╯
                         ╭─╮
                         │c│
                         ╰─╯
          Press esc to cancel  `)
		m.Update(testsupport.SendHotkey(hotkeys.CursorUp))
		v := testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		m.Update(testsupport.SendHotkey(hotkeys.CursorDown))
		v = testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		m.Update(testsupport.SendHotkey(hotkeys.CursorDown))
		v = testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("switch cursor left and right", func(t *testing.T) {
		m, _, done, _, _ := m.Update(testsupport.SendHotkey(hotkeys.CursorLeft)) // ineffectual
		want := testsupport.LinesTrimSpace(`        header1
                header2

                     Return to:
                         ╭─╮
                         │a│
                         ╰─╯
         ╭──────╮        ╭─╮
        >│submit│  or    │b│
         ╰──────╯        ╰─╯
                         ╭─╮
                         │c│
                         ╰─╯
          Press esc to cancel  `)
		if done {
			t.Error("should not be done yet")
		}
		v := testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}

		m, _, done, submitSelected, _ := m.Update(testsupport.SendHotkey(hotkeys.CursorRight)) // switch to choices
		if done {
			t.Error("should not be done yet")
		}
		if submitSelected {
			t.Error("submit should no longer be selected")
		}
		want = testsupport.LinesTrimSpace(`        header1
                header2

                     Return to:
                         ╭─╮
                        >│a│
                         ╰─╯
         ╭──────╮        ╭─╮
         │submit│  or    │b│
         ╰──────╯        ╰─╯
                         ╭─╮
                         │c│
                         ╰─╯
          Press esc to cancel  `)
		v = testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		m, _, done, submitSelected, _ = m.Update(testsupport.SendHotkey(hotkeys.CursorRight)) // ineffectual
		if done {
			t.Error("should not be done yet")
		}
		if submitSelected {
			t.Error("submit should not be selected")
		}
		v = testsupport.LinesTrimSpace(m.View())
		if v != want {
			t.Fatal(testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("choice selection remains after navigating to submit button and returning", func(t *testing.T) {
		// navigate down to b
		m, _, _, submitSelected, curChoice := m.Update(testsupport.SendHotkey(hotkeys.CursorRight))
		assert.Equal(t, false, submitSelected, "submit should not yet be selected")
		assert.EqualValues(t, 0, curChoice)
		m, _, _, submitSelected, curChoice = m.Update(testsupport.SendHotkey(hotkeys.CursorDown))
		assert.Equal(t, false, submitSelected, "submit should not yet be selected")
		assert.EqualValues(t, 1, curChoice)
		// navigate to submit
		m, _, _, submitSelected, curChoice = m.Update(testsupport.SendHotkey(hotkeys.CursorLeft))
		assert.Equal(t, true, submitSelected, "submit should now be selected")
		assert.EqualValues(t, 1, curChoice, "choice should not be affected by switching to select")
		// navigate right (which should return us to b)
		_, _, _, submitSelected, curChoice = m.Update(testsupport.SendHotkey(hotkeys.CursorRight))
		assert.Equal(t, false, submitSelected, "submit should not be selected any longer")
		assert.EqualValues(t, 1, curChoice, "choice should remain unaffected")
	})
	t.Run("invoke submit", func(t *testing.T) {
		_, _, done, submitSelected, _ := m.Update(testsupport.SendHotkey(hotkeys.Invoke))
		assert.Equal(t, true, submitSelected, "submit selection should not have changed from initial")
		assert.Equal(t, true, done, "the invoke hotkey should have marked this model as done")
	})
	t.Run("invoke option c", func(t *testing.T) {
		m, _, _, _, _ := m.Update(testsupport.SendHotkey(hotkeys.CursorRight))
		m, _, _, _, _ = m.Update(testsupport.SendHotkey(hotkeys.CursorUp)) // wrap to c
		_, _, done, submitSelected, choice := m.Update(testsupport.SendHotkey(hotkeys.Invoke))
		assert.Equal(t, false, submitSelected, "submit selection should not be selected")
		assert.Equal(t, true, done, "the invoke hotkey should have marked this model as done")
		assert.EqualValues(t, 2, choice, "on done, the last element should have been returned as selected")
	})
}
