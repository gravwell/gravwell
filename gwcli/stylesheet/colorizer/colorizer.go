/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package colorizer provides common utilities that rely on the stylesheet.
// These functions are some degree of a mish-mash, but all are in the vein of supporting a
// consistent UI. Any UI elements that look similar across actions should go here as well as
// functional applications of the rest of the stylesheet.
package colorizer

import (
	"fmt"
	"strconv"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// ErrPrintf is a tea.Printf wrapper that colors the output as an error.
func ErrPrintf(format string, a ...interface{}) tea.Cmd {
	return tea.Printf("%s", stylesheet.ErrStyle.Render(fmt.Sprintf(format, a...)))
}

// ColorCommandName returns the given command's name appropriately colored by its group (action or nav).
// Defaults to nav color.
func ColorCommandName(c *cobra.Command) string {
	if action.Is(c) {
		return stylesheet.ActionStyle.Render(c.Name())
	} else {
		return stylesheet.NavStyle.Render(c.Name())
	}
}

// Pip returns the selection rune if field == selected, otherwise it returns a space.
func Pip(selected, field uint) string {
	if selected == field {
		return lipgloss.NewStyle().Foreground(stylesheet.AccentColor2).Render(string(stylesheet.SelectionPrefix))
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
	return fmt.Sprintf("%c%c%c", leftBoundary, c, rightBoundary)
}

// SubmitString displays either the key-bind to submit the action on the current tab or the input error,
// if one exists, as well as the result string, beneath the submit-string/input-error.
func SubmitString(keybind, inputErr, result string, width int) string {
	alignerSty := lipgloss.NewStyle().
		PaddingTop(1).
		AlignHorizontal(lipgloss.Center).
		Width(width)
	var (
		inputErrOrAltEnterColor = stylesheet.TertiaryColor
		inputErrOrAltEnterText  = "Press " + keybind + " to submit"
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

// Index returns the given number, styled as an index number in a list or table.
func Index(i int) string {
	return stylesheet.IndexStyle.Render(strconv.Itoa(i))
}
