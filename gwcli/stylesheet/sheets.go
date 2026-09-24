/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

import "github.com/charmbracelet/lipgloss"

// this file just holds pre-created themes/sheets.

func Classic() Sheet {
	return Palette{
		PrimaryColor:   tropicalIndigo,
		SecondaryColor: lavenderFloral,
		TertiaryColor:  violetWeb,
		AccentColor1:   atomicTangerine,
		AccentColor2:   aquamarine,
	}.GenerateSheet()
}

// Plain returns a sheet with no colors or special characters, for maximal compatibility.
func Plain() Sheet {
	s := NewSheet()

	s.ComposableSty.FocusedBorder = lipgloss.NewStyle().BorderStyle(lipgloss.ASCIIBorder())
	s.ComposableSty.UnfocusedBorder = lipgloss.NewStyle().BorderStyle(lipgloss.HiddenBorder())
	s.ComposableSty.ComplimentaryBorder = lipgloss.NewStyle().BorderStyle(lipgloss.ASCIIBorder())

	s.TableSty = struct {
		HeaderCells lipgloss.Style
		EvenCells   lipgloss.Style
		OddCells    lipgloss.Style
		BorderType  lipgloss.Border
		BorderStyle lipgloss.Style
	}{
		HeaderCells: lipgloss.NewStyle().
			Padding(0, 1).
			AlignHorizontal(lipgloss.Center).
			AlignVertical(lipgloss.Center).Bold(true),
		EvenCells:  lipgloss.NewStyle().Padding(0, 1),
		OddCells:   lipgloss.NewStyle().Padding(0, 1),
		BorderType: lipgloss.ASCIIBorder(),
	}

	return s
}
