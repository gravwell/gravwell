/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

// miscellaneous styles

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
