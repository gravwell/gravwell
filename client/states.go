/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

type clientState uint16

const (
	// Client states
	STATE_NEW        clientState = iota
	STATE_AUTHED     clientState = iota
	STATE_CLOSED     clientState = iota
	STATE_LOGGED_OFF clientState = iota
)

func (cs clientState) String() string {
	switch cs {
	case STATE_NEW:
		return "NEW"
	case STATE_AUTHED:
		return "AUTHED"
	case STATE_CLOSED:
		return "CLOSED"
	case STATE_LOGGED_OFF:
		return "LOGGED_OFF"
	default:
	}
	return "UNKNOWN"
}
