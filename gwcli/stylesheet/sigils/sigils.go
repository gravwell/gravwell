/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package sigils contains string/rune constants.
package sigils

// Other constants we can use to enforce a consistent style across all states of the program

const (
	TAPromptPrefix = "" // text *area* prefix
	Up             = "↑"
	Down           = "↓"
	UpDown         = Up + "/" + Down
	Left           = "←"
	Right          = "→"
	LeftRight      = Left + "/" + Right
	Enter          = "↵"
	Tab            = "↹"
	Indent         = "  "
)
