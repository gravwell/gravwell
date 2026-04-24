/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package stylesheet

import "time"

// Other constants we can use to enforce a consistent style across all states of the program

const (
	TIWidth         = 60
	TAPromptPrefix  = "" // text *area* prefix
	UpSigil         = "↑"
	DownSigil       = "↓"
	UpDownSigils    = UpSigil + "/" + DownSigil
	LeftSigil       = "←"
	RightSigil      = "→"
	EnterSigil      = "↵"
	TabSigil        = "↹"
	LeftRightSigils = LeftSigil + "/" + RightSigil
	Indent          = "  "
)

const ( // list bubble modifiers
	// How long should a status message appear in a list bubble
	StatusMessageLifetime = 3 * time.Second
)
