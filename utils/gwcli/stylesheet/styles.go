// miscellaneous styles

package stylesheet

import "github.com/charmbracelet/lipgloss"

var (
	NavStyle    = lipgloss.NewStyle().Foreground(NavColor)
	ActionStyle = lipgloss.NewStyle().Foreground(ActionColor)
	ErrStyle    = lipgloss.NewStyle().Foreground(ErrorColor)

	// styles useful when displaying multiple, composed models
	Composable = struct {
		Unfocused lipgloss.Style
		Focused   lipgloss.Style
	}{
		Unfocused: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.HiddenBorder()),
		Focused: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(AccentColor1),
	}
	Header1Style   = lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
	Header2Style   = lipgloss.NewStyle().Foreground(SecondaryColor)
	GreyedOutStyle = lipgloss.NewStyle().Faint(true)
	// Mother's prompt (text prefixed to user input)
	PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor))
	// used for displaying indices
	IndexStyle   = lipgloss.NewStyle().Foreground(AccentColor1)
	ExampleStyle = lipgloss.NewStyle().Foreground(AccentColor2)
)
