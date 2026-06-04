//go:build ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package testsupport provides utility functions useful across disparate testing packages
//
// TT* functions are for use with tests that rely on TeaTest.
// Friendly reminder: calling tm.Type() with "\n"/"\t"/etc does not, at the time of writing, actually trigger the corresponding key message.
package testsupport

import (
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
)

func TestSlicesUnorderedEqual(t *testing.T) {
	type args struct {
		a []string
		b []string
	}
	tests := []struct {
		name      string
		args      args
		wantEqual bool
	}{
		{"both nil", args{nil, nil}, true},
		{"unequal, same length", args{[]string{"Underdark"}, []string{"Darklands"}}, false},
		{"unequal, different lengths", args{[]string{"Underdark"}, []string{"Darklands", "Rappan Athuk"}}, false},
		{"unequal, different lengths, same starter items", args{[]string{"Darklands", "Rappan Athuk", "Vaults"}, []string{"Darklands", "Rappan Athuk"}}, false},
		{"equal, same order", args{[]string{"Nar-Voth", "Sekamina", "Orv"}, []string{"Nar-Voth", "Sekamina", "Orv"}}, true},
		{"equal, different order", args{[]string{"Nar-Voth", "Orv", "Sekamina"}, []string{"Nar-Voth", "Sekamina", "Orv"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlicesUnorderedEqual(tt.args.a, tt.args.b); got != tt.wantEqual {
				t.Errorf("SlicesUnorderedEqual() = %v, want %v", got, tt.wantEqual)
			}
		})
	}
}

func TestSendHotkey(t *testing.T) {
	tests := []struct {
		name string // description of this test case

		b    key.Binding
		want tea.KeyMsg
	}{
		{"invoke", hotkeys.Invoke, tea.KeyMsg{Type: tea.KeyEnter}},
		{"select", hotkeys.Select, tea.KeyMsg{Type: tea.KeySpace}},
		{"cursor down", hotkeys.CursorDown, tea.KeyMsg{Type: tea.KeyDown}},
		{"cursor up", hotkeys.CursorUp, tea.KeyMsg{Type: tea.KeyUp}},
		{"complete", hotkeys.Complete, tea.KeyMsg{Type: tea.KeyTab}},
		{"quit", hotkeys.SoftQuit, tea.KeyMsg{Type: tea.KeyEsc}},

		{"filter", hotkeys.Filter, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")}},
		{"cancel filter", hotkeys.CancelWhileFiltering, tea.KeyMsg{Type: tea.KeyCtrlBackslash}},

		{"arbitrary: ctrl+t",
			key.NewBinding(
				key.WithKeys("ctrl+t"), key.WithDisabled(),
			),
			tea.KeyMsg{Type: tea.KeyCtrlT},
		},
		{"arbitrary: +",
			key.NewBinding(
				key.WithKeys("+"),
			),
			tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}},
		},
		{"arbitrary: alt+t",
			key.NewBinding(
				key.WithKeys("alt+t"),
			),
			tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'t'}},
		},
		{"arbitrary: alt+T",
			key.NewBinding(
				key.WithKeys("alt+T"),
			),
			tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'T'}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SendHotkey(tt.b)
			// check type
			if got.Type != tt.want.Type {
				t.Error("incorrect message type", ExpectedActual(tt.want.Type.String(), got.Type.String()))
			} else if tt.want.Type == tea.KeyRunes && slices.Compare(tt.want.Runes, got.Runes) != 0 {
				t.Error("rune set mismatch", ExpectedActual(string(tt.want.Runes), string(got.Runes)))
			}
		})
	}
}
