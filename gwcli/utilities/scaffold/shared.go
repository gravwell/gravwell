package scaffold

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// This file provides functionality shared across multiple scaffolds.
// Typically, this means functionality for edit and create.

// A KeyedTI is tuple for associating a TI with its field key and whether or not it is required
type KeyedTI struct {
	Key        string          // key to look up the related field in a config map (if applicable)
	FieldTitle string          // text to display to the left of the TI
	TI         textinput.Model // ti for user modifications
	Required   bool            // this TI must have data in it
}

func ViewKTIs(fieldWidth uint, ktis []KeyedTI, selectedIdx uint) string {
	if fieldWidth == 0 {
		clilog.Writer.Warnf("field width is unset")
	}
	//fieldWidth := c.longestFieldLength + 3 // 1 spaces for ":", 1 for pip, 1 for padding

	var ( // styles
		leftAlignerSty = lipgloss.NewStyle().
			Width(int(fieldWidth)).
			AlignHorizontal(lipgloss.Right).
			PaddingRight(1)
	)

	var fields []string
	var TIs []string

	for i, kti := range ktis {
		var sty = stylesheet.Cur.SecondaryText
		if kti.Required {
			sty = stylesheet.Cur.PrimaryText
		}
		title := sty.Render(kti.FieldTitle + ":")

		fields = append(fields, leftAlignerSty.Render(stylesheet.Pip(selectedIdx, uint(i))+title))

		TIs = append(TIs, kti.TI.View())
	}

	// compose all fields
	f := lipgloss.JoinVertical(lipgloss.Right, fields...)

	// compose all TIs
	t := lipgloss.JoinVertical(lipgloss.Left, TIs...)

	// conjoin fields and TIs
	return lipgloss.JoinHorizontal(lipgloss.Center, f, t)
}
