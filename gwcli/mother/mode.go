/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

// Valid modes for mother

import "fmt"

type mode int

const (
	handoff   mode = iota // Mother hands control off to child each cycle
	prompting             // default; Mother is processing user inputs alone
	quitting              // Mother is in the process of exitting
)

func (m mode) String() string {
	s := ""
	switch m {
	case prompting:
		s = "prompting"
	case quitting:
		s = "quitting"
	case handoff:
		s = "handoff"
	default:
		s = fmt.Sprintf("unknown (%d)", m)
	}
	return s
}
