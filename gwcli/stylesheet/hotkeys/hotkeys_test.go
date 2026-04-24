/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package hotkeys_test

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
)

func TestNewModel(t *testing.T) {
	m := hotkeys.NewModel()
	short := m.View()
	// check for both up and down keys
	if !strings.Contains(short, m.CursorDown.Help().Key) {
		t.Error("short view does not contain CursorDown key")
	}
	if !strings.Contains(short, m.CursorDown.Help().Desc) {
		t.Error("short view does not contain CursorDown description")
	}

	m.CursorDown.SetEnabled(false)
	short = m.View()
	// should not include the down sigil or down help text
	if strings.Contains(short, m.CursorDown.Help().Key) {
		t.Error("short view contains CursorDown key")
	}
	if strings.Contains(short, m.CursorDown.Help().Desc) {
		t.Error("short view contain CursorDown description")
	}
}
