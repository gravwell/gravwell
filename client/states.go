/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package client

type ClientState uint16

const (
	// Client states
	STATE_NEW        ClientState = iota
	STATE_AUTHED     ClientState = iota
	STATE_CLOSED     ClientState = iota
	STATE_LOGGED_OFF ClientState = iota
)

func (cs ClientState) String() string {
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
