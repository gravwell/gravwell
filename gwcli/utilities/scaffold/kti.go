package scaffold

import (
	"github.com/charmbracelet/bubbles/textinput"
)

// A KeyedTI is tuple for associating a TI with its field key and whether or not it is required
type KeyedTI struct {
	key      string          // key to look up the related field in a config map (if applicable)
	title    string          // text to display to the left of the TI
	TI       textinput.Model // ti for user modifications
	required bool            // this TI must have data in it
}

func NewKTI(key string, title string, required bool) KeyedTI {
	return KeyedTI{
		key:      key,
		title:    title,
		required: required,
	}
}

func (kti KeyedTI) Key() string {
	return kti.key
}

func (kti KeyedTI) ViewField() string {
	return kti.TI.View()
}

func (kti KeyedTI) Required() bool {
	return kti.required
}
