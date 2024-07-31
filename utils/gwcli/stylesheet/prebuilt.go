package stylesheet

import "github.com/charmbracelet/bubbles/textinput"

/**
 * Prebuilt, commonly-used models for stylistic consistency.
 */

// Creates a textinput with common attributes.
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
