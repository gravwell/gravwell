package scaffold

import (
	"github.com/charmbracelet/bubbles/textinput"
)

// A KeyedTI is tuple for associating a TI with its field key and whether or not it is required
type KeyedTI struct {
	Key      string          // key to look up the related field in a config map (if applicable)
	Title    string          // text to display to the left of the TI
	TI       textinput.Model // ti for user modifications
	Required bool            // this TI must have data in it
}

func NewKTI(key string, title string, required bool) KeyedTI {
	return KeyedTI{
		Key:      key,
		Title:    title,
		Required: required,
	}
}
