//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package attach

import (
	"fmt"
	"path"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

// Attach hands its dimensions to datascope when the user picks a search, which can be long after
// SetArgs stashed them, so a resize in between must be picked up.
// Without accurate dimensions datascope draws a placeholder for its first frame (see issue #2748).
//
// SetArgs itself reaches the backend to resolve the search, so it is left to the noci suite.
func Test_TerminalDimensions(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}

	dims := func(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

	tests := []struct {
		name          string
		msg           tea.Msg
		width, height int // expected, starting from 80x50
	}{
		{"resize", tea.WindowSizeMsg{Width: 120, Height: 40}, 120, 40},
		{"key press", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, 80, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Initial()
			a.width, a.height = 80, 50

			a.Update(tt.msg)

			if a.width != tt.width || a.height != tt.height {
				t.Error("bad dimensions after "+tt.name, testsupport.ExpectedActual(
					dims(tt.width, tt.height), dims(a.width, a.height)))
			}
		})
	}
}
