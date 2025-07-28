/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package stylesheet managing the visual effects of gwcli.
// Most styling is via lipgloss and encompasses colors, alignment, borders, etc.
//
// The stylesheet package should also be used for maintaining consistent visuals.
// This is accomplished via the provided pre-built elements and the Sheet variable for pre-set styles.
//
// I don't know much about color theory or picking good palettes,
// so homegrown palettes are based on Gravwell's colors and expanded via https://coolors.co/.
package stylesheet

// miscellaneous styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// NoColor is derived and set by Mother in ppre().
// If this is true, assume stylesheet.Cur has been set to its simplest, colorless form.
// Can be read by other packages to tweak their output beyond what the stylesheet can do.
var NoColor bool

// Cur is the stylesheet currently in-use by gwcli.
// This is what other packages should reference when stylizing their elements.
var Cur Sheet

func init() {
	// set the current stylesheet
	Cur = Classic() //tritonePlus()
}

// A Sheet is a set of lipgloss.Style fields sufficient to colorize/theme gwcli.
// A single sheet is selected at start up (in init()) to provide the styling for all aspects of gwcli.
type Sheet struct {
	Nav    lipgloss.Style // style of nav/directory items while traversing the tree
	Action lipgloss.Style // style of actions/invokables while traversing the tree

	FieldText lipgloss.Style // style applied to fields, some of which will be preceded by a Pip
	Pip       func() string  // must return a single, stylized character that points to the currently selected field

	// for building multi-pane views
	ComposableSty struct {
		FocusedBorder       lipgloss.Style // stylized border for wrapping elements currently in focus
		UnfocusedBorder     lipgloss.Style // stylized border for wrapping elements that could be in focus, but are currently blurred
		ComplimentaryBorder lipgloss.Style // stylized border for wrapping complimentary elements that do not toggle focus
	}

	// for building tables
	TableSty struct {
		HeaderCells lipgloss.Style
		EvenCells   lipgloss.Style
		OddCells    lipgloss.Style
		BorderType  lipgloss.Border
		// do not set .Border in this style; it causes tbl.Render to freak out and drop results.
		// Instead, set TableSty.BorderType
		BorderStyle lipgloss.Style
	}

	ErrorText    lipgloss.Style // text that displays an error
	ExampleText  lipgloss.Style // text that display an example
	DisabledText lipgloss.Style // text/item that is currently disabled

	// user inputs (NOT including those already covered by FieldText).
	// This is primarily used for Mother's prompt and the cred/mfa prompts.
	PromptSty struct {
		Symbol func() string       // a stylized sigil (expected to be a single character) that suffixes prompt text
		Text   func(string) string // given the text prefixing the input, returns a stylized version of it
	}

	// catchall for important/focal text that does not fit into a different category.
	// This text will be used regularly to indicate significance.
	PrimaryText lipgloss.Style
	// catchall for text that does not fit into a different category and is not primary.
	// This text typically sidecars primary text.
	SecondaryText lipgloss.Style
	// rarely used style intended to supplement secondary text when yet more differentiation is necessary.
	TertiaryText lipgloss.Style

	Spinner     lipgloss.Style
	SpinnerText lipgloss.Style // text that sometimes accompanies a spinner
}

// NewSheet initializes a bare minimum sheet, ensuring required parameters are in place.
// While sheets can be built completely from scratch, calling NewSheet as the base ensures the style will not cause panics.
func NewSheet(pip func() string, promptSymbol func() string, promptText func(string) string) Sheet {
	return Sheet{
		Pip: pip,
		ComposableSty: struct {
			FocusedBorder       lipgloss.Style
			UnfocusedBorder     lipgloss.Style
			ComplimentaryBorder lipgloss.Style
		}{},
		TableSty: struct {
			HeaderCells lipgloss.Style
			EvenCells   lipgloss.Style
			OddCells    lipgloss.Style
			BorderType  lipgloss.Border
			BorderStyle lipgloss.Style
		}{BorderType: lipgloss.ASCIIBorder()},
		PromptSty: struct {
			Symbol func() string
			Text   func(string) string
		}{Symbol: promptSymbol, Text: promptText},
	}
}

func (s Sheet) Prompt(text string) string {
	return s.PromptSty.Text(text) + s.PromptSty.Symbol()
}

// Field returns the title in the form ` <title>: `, with the spacing prefix set by width-len(title).
func (s Sheet) Field(fieldTitle string, width int) string {
	pad := width - len(fieldTitle)
	if pad > 0 {
		fieldTitle = strings.Repeat(" ", pad) + fieldTitle
	}
	return Cur.FieldText.Render(fieldTitle + ": ")
}

// A Tetrad is a set of 4 colors that can be transmuted into a full sheet via GenerateSheet().
/*type Tetrad struct {

}*/

// A Palette is a set of 5 colors that can be transmuted into a full sheet via GenerateSheet().
// It allows for quicker color swaps without having to manually populate a whole style sheet.
type Palette struct {
	// The focal/most important/most common color.
	// The main prompt's text, for example, will be this color.
	PrimaryColor lipgloss.Color
	// A color complimentary to the primary.
	// This also serves as the Nav color.
	SecondaryColor lipgloss.Color
	// A color complimentary to the primary and secondary
	TertiaryColor lipgloss.Color
	// accent colors can serve as a pop of color outside of the adjacency of primary/secondary/tertiary.
	// This also serves as the Action color.
	AccentColor1 lipgloss.Color
	// accent colors can serve as a pop of color outside of the adjacency of primary/secondary/tertiary.
	// Generally speaking, AccentColor2 is rarer than AccentColor1
	AccentColor2 lipgloss.Color
}

func (p Palette) GenerateSheet() Sheet {
	pipRune := '>'
	pipSty := lipgloss.NewStyle().Foreground(p.AccentColor1)
	primaryColorSty := lipgloss.NewStyle().Foreground(p.PrimaryColor)
	secondaryColorSty := lipgloss.NewStyle().Foreground(p.SecondaryColor)
	accentColor1Sty := lipgloss.NewStyle().Foreground(p.AccentColor1)

	s := NewSheet(
		func() string { return pipSty.Render(string(pipRune)) },
		func() string { return primaryColorSty.Render(">") },
		func(s string) string { return primaryColorSty.Render(s) })
	s.Nav = secondaryColorSty
	s.Action = lipgloss.NewStyle().Foreground(p.AccentColor2)
	s.FieldText = primaryColorSty
	s.ComposableSty = struct {
		FocusedBorder       lipgloss.Style
		UnfocusedBorder     lipgloss.Style
		ComplimentaryBorder lipgloss.Style
	}{
		FocusedBorder: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(p.PrimaryColor),
		UnfocusedBorder: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.HiddenBorder()),
		ComplimentaryBorder: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(p.AccentColor1),
	}
	s.TableSty = struct {
		HeaderCells lipgloss.Style
		EvenCells   lipgloss.Style
		OddCells    lipgloss.Style
		BorderType  lipgloss.Border
		BorderStyle lipgloss.Style
	}{
		HeaderCells: lipgloss.NewStyle().
			Foreground(p.PrimaryColor).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).Bold(true),
		EvenCells:   lipgloss.NewStyle().Padding(0, 1).Width(15).Foreground(p.SecondaryColor),
		OddCells:    lipgloss.NewStyle().Padding(0, 1).Width(15).Foreground(p.TertiaryColor),
		BorderType:  lipgloss.NormalBorder(),
		BorderStyle: primaryColorSty,
	}
	s.ErrorText = lipgloss.NewStyle().Foreground(bittersweet)
	s.ExampleText = accentColor1Sty.Italic(true)
	s.DisabledText = lipgloss.NewStyle().Faint(true)
	s.PrimaryText = primaryColorSty
	s.SecondaryText = secondaryColorSty
	s.TertiaryText = lipgloss.NewStyle().Foreground(p.TertiaryColor)
	s.Spinner = primaryColorSty
	s.SpinnerText = secondaryColorSty

	return s
}
