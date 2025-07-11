/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

// this file just holds pre-created themes/sheets.

/*
func softPink() Sheet {
	return Sheet{
		Nav:    lipgloss.NewStyle().Foreground(amaranthPurple),
		Action: lipgloss.NewStyle().Foreground(melon),

		Composable: struct {
			FocusedBorder       lipgloss.Style
			UnfocusedBorder     lipgloss.Style
			ComplimentaryBorder lipgloss.Style
			ModifierText        lipgloss.Style
		}{
			FocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(amethyst),
			UnfocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.HiddenBorder()),
			ComplimentaryBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(mistyRose),
			ModifierText: lipgloss.NewStyle().Foreground(melon),
		},

		Table: struct {
			HeaderCells lipgloss.Style
			EvenCells   lipgloss.Style
			OddCells    lipgloss.Style
			BorderType  lipgloss.Border
			BorderStyle lipgloss.Style
		}{
			HeaderCells: lipgloss.NewStyle().
				Foreground(amethyst).
				AlignHorizontal(lipgloss.Center).
				AlignVertical(lipgloss.Center).Bold(true),
			EvenCells:   lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(melon),
			OddCells:    lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(mistyRose),
			BorderType:  lipgloss.NormalBorder(),
			BorderStyle: lipgloss.NewStyle().Foreground(amethyst),
		},

		ErrorText:    lipgloss.NewStyle().Foreground(bloodRed),
		ExampleText:  lipgloss.NewStyle().Foreground(satinSheenGold),
		DisabledText: lipgloss.NewStyle().Faint(true),

		PromptText: lipgloss.NewStyle().Foreground(amethyst),

		PrimaryText:   lipgloss.NewStyle().Foreground(amethyst),
		SecondaryText: lipgloss.NewStyle().Foreground(melon),

		Spinner: lipgloss.NewStyle().Foreground(amethyst),
	}
}

func tritonePlus() Sheet {
	nav, action := lipgloss.NewStyle().Foreground(steelBlue), lipgloss.NewStyle().Foreground(bittersweet)

	one, two, three := darkViolet, sunglow, yellowGreen

	return Sheet{
		Nav: nav, Action: action,

		Composable: struct {
			FocusedBorder       lipgloss.Style
			UnfocusedBorder     lipgloss.Style
			ComplimentaryBorder lipgloss.Style
			ModifierText        lipgloss.Style
		}{
			FocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(one),
			UnfocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.HiddenBorder()),
			ComplimentaryBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(three),
			ModifierText: lipgloss.NewStyle().Foreground(three),
		},

		Table: struct {
			HeaderCells lipgloss.Style
			EvenCells   lipgloss.Style
			OddCells    lipgloss.Style
			BorderType  lipgloss.Border
			BorderStyle lipgloss.Style
		}{
			HeaderCells: lipgloss.NewStyle().
				Foreground(one).
				AlignHorizontal(lipgloss.Center).
				AlignVertical(lipgloss.Center).Bold(true),
			EvenCells:   lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(two),
			OddCells:    lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(three),
			BorderType:  lipgloss.NormalBorder(),
			BorderStyle: lipgloss.NewStyle().Foreground(one),
		},

		ErrorText:    lipgloss.NewStyle().Foreground(bloodRed),
		ExampleText:  lipgloss.NewStyle().Foreground(yellowGreen),
		DisabledText: lipgloss.NewStyle().Faint(true),

		PromptText: lipgloss.NewStyle().Foreground(one),

		PrimaryText:   lipgloss.NewStyle().Foreground(one),
		SecondaryText: lipgloss.NewStyle().Foreground(two),

		Spinner: lipgloss.NewStyle().Foreground(one),
	}
}

func classic() Sheet {
	var (
		primaryColor   = tropicalIndigo
		secondaryColor = lavender_floral
		tertiaryColor  = violet_web
		//accentColor1   = atomicTangerine
		accentColor2 = aquamarine
	)

	nav := lipgloss.NewStyle().Foreground(secondaryColor)
	action := lipgloss.NewStyle().Foreground(tertiaryColor)

	return Sheet{
		Nav: nav, Action: action,

		Composable: struct {
			FocusedBorder       lipgloss.Style
			UnfocusedBorder     lipgloss.Style
			ComplimentaryBorder lipgloss.Style
			ModifierText        lipgloss.Style
		}{
			FocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(primaryColor),
			UnfocusedBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.HiddenBorder()),
			ComplimentaryBorder: lipgloss.NewStyle().
				Align(lipgloss.Left, lipgloss.Center).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(accentColor2),
			ModifierText: lipgloss.NewStyle().Foreground(primaryColor),
		},

		Table: struct {
			HeaderCells lipgloss.Style
			EvenCells   lipgloss.Style
			OddCells    lipgloss.Style
			BorderType  lipgloss.Border
			BorderStyle lipgloss.Style
		}{
			HeaderCells: lipgloss.NewStyle().
				Foreground(primaryColor).
				AlignHorizontal(lipgloss.Center).
				AlignVertical(lipgloss.Center).Bold(true),
			EvenCells:   lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(secondaryColor),
			OddCells:    lipgloss.NewStyle().Padding(0, 1).Width(30).Foreground(tertiaryColor),
			BorderType:  lipgloss.NormalBorder(),
			BorderStyle: lipgloss.NewStyle().Foreground(primaryColor),
		},

		ErrorText:    lipgloss.NewStyle().Foreground(bloodRed),
		ExampleText:  lipgloss.NewStyle().Foreground(accentColor2),
		DisabledText: lipgloss.NewStyle().Faint(true),

		PromptText: lipgloss.NewStyle().Foreground(primaryColor),

		PrimaryText:   lipgloss.NewStyle().Foreground(primaryColor),
		SecondaryText: lipgloss.NewStyle().Foreground(secondaryColor),

		Spinner: lipgloss.NewStyle().Foreground(primaryColor),
	}
}
*/

func classic() Sheet {
	return Palette{
		PrimaryColor:   tropicalIndigo,
		SecondaryColor: lavender_floral,
		TertiaryColor:  violet_web,
		AccentColor1:   atomicTangerine,
		AccentColor2:   aquamarine,
	}.GenerateSheet()
}

// NoColor returns a sheet with no colors or special characters, for maximal compatibility.
func NoColor() Sheet {
	return NewSheet(
		func() string { return ">" },
		func() string { return "#" },
		func(s string) string { return s },
	)
}
