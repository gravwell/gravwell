/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package phrases provides strings and functions for consistent text across multiple actions.
// It is intended specifically to prevent minor inconsistencies such as one action printing: "successfully wrote 5 bytes to file"
// and another action printing: "Wrote 5 bytes to path/to/file"
package phrases

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
)

// SuccessfullyWroteToFile returns a string to be printed to a user when data is successfully written to a file.
func SuccessfullyWroteToFile(n int, fileName string) string {
	return fmt.Sprintf("successfully wrote %d bytes to %s", n, fileName)
}

// SuccessfullyLoadedFile states that the file at the given path has been loaded.
func SuccessfullyLoadedFile(path string) string {
	return "successfully loaded file " + path
}

// Exactly1ArgRequired states the user must specify a single, bare argument.
// argName should be what this argument is (ex: "flow ID", "resource ID", "macro name", ...).
func Exactly1ArgRequired(argName string) string {
	return "you must specify exactly 1 argument (" + argName + ")"
}

// AtLeast1ArgRequired states the user must at least one bare argument.
// argNamePlural should be what these arguments are (ex: "flow IDs", "resource IDs", "macro names", ...).
func AtLeast1ArgRequired(argNamePlural string) string {
	return "you must specify at least 1 argument (" + argNamePlural + ")"
}

// InteractivityNYI returns a coloured tea.Println stating that interactivity for this action is not ready yet.
//
// Should be returned by SetArgs' onStart return.
func InteractivityNYI() tea.Cmd {
	return stylesheet.ErrPrintf("interactivity not yet implemented")
}

// SuccessfullyCreatedItem states that an item of type itemSingular was created and can be identified with ID.
//
// Example: "alert", 1 -> "successfully created alert (ID: 1)".
func SuccessfullyCreatedItem(itemSingular string, ID string) string {
	return "successfully created " + itemSingular + " (ID: " + ID + ")"
}
