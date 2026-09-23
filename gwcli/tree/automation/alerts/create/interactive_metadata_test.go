//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package alertscreate

import (
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/sigils"
	"github.com/stretchr/testify/assert"
)

// The hotkey legend, as it is displayed at the bottom of the metadata form.
const wantLegend = sigils.UpDown + " up/down • " + sigils.Enter + " invoke • space select"

// The metadata form footers itself with the shared legend, so changes to hotkeys.Model.ShortHelp()
// land here too.
// Select ("space") is of particular note: it is the only way to flip the Enable checkbox.
// See gwcli#2545.
func TestMetadataView_HotkeyLegend(t *testing.T) {
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.Plain()

	m := NewMetadata()

	t.Run("legend is the footer", func(t *testing.T) {
		v := m.View()
		assert.True(t, strings.HasSuffix(v, wantLegend),
			"legend is not the trailing content of the view\n"+testsupport.Uncloak(v))
	})

	t.Run("select toggles the enable checkbox", func(t *testing.T) {
		m.selected = metaEnable
		enabled := m.enable
		if _, done := m.Update(testsupport.SendHotkey(hotkeys.Select)); done {
			t.Fatal("metadata unexpectedly considers itself done")
		}
		assert.Equal(t, !enabled, m.enable, testsupport.ExpectedActual(!enabled, m.enable))
	})
}
