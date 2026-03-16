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

import "fmt"

// StringWriteToFileSuccess returns a string to be printed to a user when data is successfully written to a file.
func WriteToFileSuccess(n int, fileName string) string { // TODO rename
	return fmt.Sprintf("successfully wrote %d bytes to %s", n, fileName)
}

// Exactly1ArgRequired states the user must specify a single, bare argument.
// argName should be what this argument is (ex: "flow ID", "resource ID", "macro name", ...).
func Exactly1ArgRequired(argName string) string {
	return "you must specify exactly 1 argument (" + argName + ")"
}
