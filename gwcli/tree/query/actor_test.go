//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package query

import (
	"fmt"
	"path"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

// The query actor must retain the terminal dimensions Mother hands it, and keep them current as the
// terminal is resized, so it can pass them down when it eventually constructs datascope.
// Without them datascope draws a placeholder for its first frame (see issue #2748).
func Test_TerminalDimensions(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}

	// dims returns the actor's stashed dimensions in a form fit for an error message.
	dims := func(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

	t.Run("stashed by SetArgs", func(t *testing.T) {
		tests := []struct {
			name          string
			width, height int
		}{
			{"typical terminal", 80, 50},
			{"wide terminal", 200, 60},
			// Mother does not know her own size until BubbleTea tells her, and BubbleTea never
			// does when output is not a TTY. The actor must take the zeroes rather than choke.
			{"size not yet known", 0, 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				q := Initial()
				// an empty query boots the editor view, which does not touch the backend
				invalid, _, err := q.SetArgs(nil, []string{}, tt.width, tt.height)
				if err != nil {
					t.Fatal(err)
				} else if invalid != "" {
					t.Fatal("unexpected invalid response: " + invalid)
				}
				if q.termWidth != tt.width || q.termHeight != tt.height {
					t.Error("bad stashed dimensions", testsupport.ExpectedActual(
						dims(tt.width, tt.height), dims(q.termWidth, q.termHeight)))
				}
			})
		}
	})

	t.Run("refreshed on resize", func(t *testing.T) {
		q := Initial()
		if _, _, err := q.SetArgs(nil, []string{}, 80, 50); err != nil {
			t.Fatal(err)
		}
		q.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		if q.termWidth != 120 || q.termHeight != 40 {
			t.Error("resize did not update the stashed dimensions",
				testsupport.ExpectedActual(dims(120, 40), dims(q.termWidth, q.termHeight)))
		}
	})

	t.Run("untouched by other messages", func(t *testing.T) {
		q := Initial()
		if _, _, err := q.SetArgs(nil, []string{}, 80, 50); err != nil {
			t.Fatal(err)
		}
		q.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		if q.termWidth != 80 || q.termHeight != 50 {
			t.Error("a non-resize message altered the stashed dimensions",
				testsupport.ExpectedActual(dims(80, 50), dims(q.termWidth, q.termHeight)))
		}
	})
}
