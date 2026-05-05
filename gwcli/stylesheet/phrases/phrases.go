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
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
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

// NounNumerosity is basically a ternary shorthand for picking singular or plural based on if count==1.
func NounNumerosity(count int, singularForm, pluralForm string) string {
	if count == 1 {
		return singularForm
	}
	return pluralForm
}

// MissingRequiredField returns text stating the given field must be populated.
func MissingRequiredField(fieldName string) string {
	return "field " + fieldName + " is required"
}

// ErrBinaryBlobCoward returns a user-facing error that the given format must be output to a file.
// The value should be the format of the blob.
type ErrBinaryBlobCoward string

func (fmt ErrBinaryBlobCoward) Error() string {
	return "refusing to dump binary blob (format " + string(fmt) + ") to stdout.\n" +
		"If this is intentional, re-run with -" + ft.Output.Shorthand() + " <FILENAME>.\n" +
		"If it was not, re-run with --" + ft.CSV.Name() + " or --" + ft.JSON.Name() + " to download in a more appropriate format."
}

var _ error = ErrBinaryBlobCoward("format")

// ErrUnknownSID returns a user-facing error stating that the given sid is unknown.
// The value should be the unknown search ID itself.
type ErrUnknownSID string

func (sid ErrUnknownSID) Error() string {
	return "did not find a search associated to searchID '" + string(sid) + "'"
}

var _ error = ErrUnknownSID("")
