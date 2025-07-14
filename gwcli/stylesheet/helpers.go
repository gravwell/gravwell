package stylesheet

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gravwell/gravwell/v4/gwcli/action"
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
		return Cur.Action.Render(c.Name())
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

// ViewSubmitButton displays... a submit button.
// It displays the error if set.
// Same for the result.
// If not displaying either, it displays a box with "submit" in it.
func ViewSubmitButton(selected bool, result, errStr string) string {
	var (
		str string
		pip = strings.Repeat(" ", lipgloss.Width(Cur.Pip()))
	)
	if errStr != "" {
		str = Cur.ComposableSty.ComplimentaryBorder.Render(Cur.ErrorText.Render(errStr))
	} else if result != "" {
		str = Cur.ComposableSty.ComplimentaryBorder.Render(result)
	} else {
		str = Button("submit")
	}
	if selected {
		pip = Cur.Pip()
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, pip, str)
}

// SubmitString displays either the key-bind to submit the action on the current tab or the input error,
// if one exists, as well as the result string, beneath the submit-string/input-error.
func SubmitString(keybind, inputErr, result string, width int) string {
	alignerSty := lipgloss.NewStyle().
		PaddingTop(1).
		AlignHorizontal(lipgloss.Center).
		Width(width)
	var (
		inputErrOrAltEnterColor = Cur.ExampleText.GetForeground()
		inputErrOrAltEnterText  = "Press " + keybind + " to submit"
	)
	if inputErr != "" {
		inputErrOrAltEnterColor = Cur.ErrorText.GetForeground()
		inputErrOrAltEnterText = inputErr
	}

	return lipgloss.JoinVertical(lipgloss.Center,
		alignerSty.Foreground(inputErrOrAltEnterColor).Render(inputErrOrAltEnterText),
		alignerSty.Foreground(Cur.SecondaryText.GetForeground()).Render(result),
	)
}

// Index returns the given number, styled as an index number in a list or table.
func Index(i int) string {
	return Cur.PrimaryText.Render(strconv.Itoa(i))
}
