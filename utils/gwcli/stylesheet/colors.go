// Colors provides constants used to provide uniform, readable colors to the styles
package stylesheet

import "github.com/charmbracelet/lipgloss"

// The Primary+Accents are based on a triadic scheme with #9c7af7 at the head
// The secondary, tertiary colors are analogous to the primary

const (
	PrimaryColor   = lipgloss.Color("#9c7af7")
	SecondaryColor = lipgloss.Color("#bb7af7")
	TertiaryColor  = lipgloss.Color("#f77af4")
	AccentColor1   = lipgloss.Color("#f79c7a")
	AccentColor2   = lipgloss.Color("#7af79c")
	ErrorColor     = lipgloss.Color("#f77a96")
	NavColor       = SecondaryColor
	ActionColor    = AccentColor1
	FocusedColor   = AccentColor2   // an element currently in focus
	UnfocusedColor = SecondaryColor // complimentary elements to the focused element
)

const ( // table colors
	borderColor = PrimaryColor
	row1Color   = SecondaryColor
	row2Color   = TertiaryColor
)
