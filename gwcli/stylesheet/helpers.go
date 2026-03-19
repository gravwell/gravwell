package stylesheet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/spf13/cobra"
)

// ErrPrintf is a tea.Printf wrapper that colors the output as an error.
func ErrPrintf(format string, a ...interface{}) tea.Cmd {
	return tea.Printf("%s", Cur.ErrorText.Render(fmt.Sprintf(format, a...)))
}

// ColorCommandName returns the given command's name appropriately colored by its group (action or nav).
// Defaults to nav color.
func ColorCommandName(c *cobra.Command) string {
	if action.Is(c) {
		return Cur.Action.Render(c.Name())
	} else {
		return Cur.Nav.Render(c.Name())
	}
}

// Pip returns the selection rune if field == selected, otherwise it returns a space.
func Pip(selected, field uint) string {
	if selected == field {
		return Cur.Pip()
	}
	return " "
}

// Checkbox returns a simple checkbox with angled edges.
// If val is true, a check mark will be displayed
func Checkbox(val bool) string {
	return box(val, '[', ']')
}

// Radiobox is Checkbox but with rounded edges.
func Radiobox(val bool) string {
	return box(val, '(', ')')
}

// Returns a simple checkbox.
// If val is true, a check mark will be displayed
func box(val bool, leftBoundary, rightBoundary rune) string {
	c := ' '
	if val {
		c = '✓'
	}
	return fmt.Sprintf("%c%s%c", leftBoundary, Cur.SecondaryText.Render(string(c)), rightBoundary)
}

// Button returns the text stylized as a selectable button.
// Leaves a cell on the left for the pip (which is drawn if pip is set).
func Button(text string) string {
	btn := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Cur.SecondaryText.GetForeground()).
		Foreground(Cur.PrimaryText.GetForeground()).
		Render(text)
	return btn
}

var minSubmitButtonWidth = Cur.ComposableSty.ComplimentaryBorder.GetBorderLeftSize() + Cur.ComposableSty.ComplimentaryBorder.GetBorderRightSize() +
	len("submit") + 1 + 2 // +1 expected pip width & +2 padding

// ViewSubmitButton displays... a submit button.
// It displays one of the errors if set, 1, then 2.
// If not displaying either, it displays a box with "submit" in it.
//
// The returned object will be centered relative to width.
// Width should be > 4 to ensure text is wrapped properly without screwing up the border.
func ViewSubmitButton(selected bool, paneWidth int, errors ...string) string {
	// sanity check width
	if paneWidth < minSubmitButtonWidth {
		clilog.Writer.Warnf("pane width is below minimum (%v); overriding to minimum", minSubmitButtonWidth)
		paneWidth = minSubmitButtonWidth
	}
	var (
		pip    string
		btnTxt string
	)

	// configure pip
	if selected {
		pip = Cur.Pip()
	} else {
		pip = strings.Repeat(" ", lipgloss.Width(Cur.Pip()))
	}
	// find the first non-empty error
	for _, e := range errors {
		if e != "" {
			btnTxt = e
			break
		}
	}
	if btnTxt == "" { // if no valid error was found, return a submit button
		return lipgloss.NewStyle().AlignHorizontal(lipgloss.Center).Width(paneWidth).Render(
			lipgloss.JoinHorizontal(lipgloss.Center,
				pip,
				Button("submit"),
			))
	}

	// sets the width the error text can take up.
	// textWidth <= paneWidth-2
	textWidth := (paneWidth * 3) / 5 // enable the text to take up most, but not all, of the pane
	// wrap and style text
	btnTxt = Cur.ErrorText.AlignHorizontal(lipgloss.Center).Width(textWidth).Render(btnTxt)
	// box the text
	btnTxt = Cur.ComposableSty.ComplimentaryBorder.Render(btnTxt)
	// pip, then pad and center
	return lipgloss.NewStyle().AlignHorizontal(lipgloss.Center).Width(paneWidth).Render(
		lipgloss.JoinHorizontal(lipgloss.Center,
			pip,
			btnTxt,
		),
	)
}

// Index returns the given number, styled as an index number in a list or table.
func Index(i int) string {
	return Cur.PrimaryText.Render(strconv.Itoa(i))
}

// TitledBorder returns content wrapped in a border (according to borderStyle) with a title in the top border.
/*func TitledBorder(borderStyle lipgloss.Style, titleTextStyle lipgloss.Style, title string, contents string) string {
	topWidth := lipgloss.Width(contents)

	var (
		bs     = borderStyle.GetBorderStyle()
		topSty = lipgloss.NewStyle().Foreground(borderStyle.GetBorderTopForeground())
		div    = strings.Repeat(bs.Top, (topWidth-len(title))/2) // the lines on either side of title
	)

	// compensate for odd lengths
	rightDiv := div
	if (topWidth-len(title))%2 == 1 {
		rightDiv += bs.Top
	}
	// generate the top
	top := topSty.Render(bs.TopLeft+div) + titleTextStyle.Render(title) + topSty.Render(rightDiv+bs.TopRight)

	// wrap the contents in a border and prefix the top
	return top + "\n" +
		borderStyle.Border(bs, false, true, true, true).Width(topWidth).Render(contents)
}*/

func SegmentedBorder(borderStyle lipgloss.Style, width int, segments ...struct {
	StylizedTitle string
	Contents      string
}) (string, error) {
	if len(segments) == 0 {
		return "", errors.New("cannot draw a segmented border with no segments")
	} else if width <= 0 {
		return "", errors.New("width must be > 0")
	}

	// prepare the data we need across all iterations
	var (
		bs = borderStyle.GetBorderStyle()
		// style used for the head of each segment (where the titles are)
		splitterSty = lipgloss.NewStyle().Foreground(borderStyle.GetBorderTopForeground())
	)

	var sb strings.Builder
	for i, segment := range segments {
		var (
			titleLen = lipgloss.Width(segment.StylizedTitle)
			div      = strings.Repeat(bs.Top, (width-titleLen)/2) // the lines on either side of title
			leftDiv  = div
			rightDiv = div
		)
		// prepare divider halves
		{
			// compensate for odd lengths
			if (width-titleLen)%2 == 1 {
				rightDiv += bs.Top
			}

			if i == 0 {
				leftDiv = bs.TopLeft + leftDiv
				rightDiv += bs.TopRight
			} else {
				leftDiv = bs.MiddleLeft + leftDiv
				rightDiv += bs.MiddleRight
			}
		}

		// generate the segment head
		head := splitterSty.Render(leftDiv) + segment.StylizedTitle + splitterSty.Render(rightDiv)
		sb.WriteString(head + "\n")

		var footer bool
		if i == len(segments)-1 { // use the border with a footer
			footer = true
		}
		if segment.Contents != "" {
			sb.WriteString(borderStyle.Border(bs, false, true, footer, true).Width(width).Render(segment.Contents) + "\n")
		}
	}

	// wrap the contents in a border and prefix the top
	return sb.String(), nil
}

// RequiredTitle is just a helper function to consistently attach a colon and color the given text as the primary color.
func RequiredTitle(s string) string {
	return Cur.PrimaryText.Render(s + ":")
}

// OptionalTitle is just a helper function to consistently attach a colon and color the given text as the secondary color.
func OptionalTitle(s string) string {
	return Cur.SecondaryText.Render(s + ":")
}
