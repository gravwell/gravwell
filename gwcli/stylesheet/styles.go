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
// The stylesheet package should also be used for maintaining consistent visuals via stylized skeletons and pre-built elements.
package stylesheet

// miscellaneous styles

import "github.com/charmbracelet/lipgloss"

var (
	NavStyle    = lipgloss.NewStyle().Foreground(NavColor)
	ActionStyle = lipgloss.NewStyle().Foreground(ActionColor)
	ErrStyle    = lipgloss.NewStyle().Foreground(ErrorColor)

	// styles useful when displaying multiple, composed models
	Composable = struct {
		Unfocused lipgloss.Style // for a blurred model that could be focused at some point
		Focused   lipgloss.Style // for a focused model that could be blurred at some point
		Primary   lipgloss.Style // for a model that does not change focus and is the center of attention
		Secondary lipgloss.Style // for a model that does not change focus and is related to Primary
	}{
		Unfocused: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.HiddenBorder()),
		Focused: lipgloss.NewStyle().
			Align(lipgloss.Left, lipgloss.Center).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(AccentColor1),
		// NOTE(rlandau): the other styles are set in init()
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

func init() {
	Composable.Primary = Composable.Focused.BorderStyle(lipgloss.RoundedBorder())
	Composable.Secondary = Composable.Focused.BorderStyle(lipgloss.RoundedBorder()).BorderForeground(PrimaryColor)

}
