/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

/**
 * Prebuilt, commonly-used models for stylistic consistency.
 */

import "github.com/charmbracelet/bubbles/textinput"

// NewTI creates a textinput with common attributes.
func NewTI(defVal string, optional bool) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Width = 20
	ti.Blur()
	ti.SetValue(defVal)
	ti.KeyMap.WordForward.SetKeys("ctrl+right", "alt+right", "alt+f")
	ti.KeyMap.WordBackward.SetKeys("ctrl+left", "alt+left", "alt+b")
	if optional {
		ti.Placeholder = "(optional)"
	}
	return ti
}
