// colorizer provides common utilities that rely on the stylesheet.
// These functions are some degree of a mish-mash, but all are in the vein of supporting a
// consistent UI. Any UI elements that look similar across actions should go here as well as
// functional applications of the rest of the stylesheet.
package colorizer

import (
	"fmt"
	"gwcli/action"
	"gwcli/stylesheet"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// tea.Printf wrapper that colors the output as an error
func ErrPrintf(format string, a ...interface{}) tea.Cmd {
	return tea.Printf(stylesheet.ErrStyle.Render(fmt.Sprintf(format, a...)))
}

// Given a command, returns its name appropriately colored by its group (action or nav).
// Defaults to nav color.
func ColorCommandName(c *cobra.Command) string {
	if action.Is(c) {
		return stylesheet.ActionStyle.Render(c.Name())
	} else {
		return stylesheet.NavStyle.Render(c.Name())
	}
}

// if field == selected, returns the selection rune.
// otherwise, returns a space.
func Pip(selected, field uint) string {
	if selected == field {
		return lipgloss.NewStyle().Foreground(stylesheet.AccentColor2).Render(string(stylesheet.SelectionPrefix))
	}
	return " "
}

// Returns a simple checkbox with angled edges.
// If val is true, a check mark will be displayed
func Checkbox(val bool) string {
	return box(val, '[', ']')
}

// Checkbox() but with rounded edges.
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
	return fmt.Sprintf("%c%c%c", leftBoundary, c, rightBoundary)
}

// Displays either the key-bind to submit the action on the current tab or the input error,
// if one exists, as well as the result string, beneath the submit-string/input-error
func SubmitString(keybind, inputErr, result string, width int) string {
	alignerSty := lipgloss.NewStyle().
		PaddingTop(1).
		AlignHorizontal(lipgloss.Center).
		Width(width)
	var (
		inputErrOrAltEnterColor        = stylesheet.TertiaryColor
		inputErrOrAltEnterText  string = "Press " + keybind + " to submit"
	)
	if inputErr != "" {
		inputErrOrAltEnterColor = stylesheet.ErrorColor
		inputErrOrAltEnterText = inputErr
	}

	return lipgloss.JoinVertical(lipgloss.Center,
		alignerSty.Foreground(inputErrOrAltEnterColor).Render(inputErrOrAltEnterText),
		alignerSty.Foreground(stylesheet.SecondaryColor).Render(result),
	)
}

// Return the given number, styled as an index number in a list or table.
func Index(i int) string {
	return stylesheet.IndexStyle.Render(strconv.Itoa(i))
}
