/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package killer provides a consistent interface for checking a uniform set of kill keys.
// Used by Mother and interactive models Cobra spins up outside of Mother.
package killer

import tea "github.com/charmbracelet/bubbletea"

type Kill = uint

const (
	None Kill = iota
	Global
	Child
)

// keys kill the program in Update no matter its other states
var globalKillKeys = [...]tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD}

// GlobalKillKeys returns the list of bubble tea combinations that act as global kills by Mother.
func GlobalKillKeys() [2]tea.KeyType {
	return globalKillKeys
}

// keys that kill the child if it exists, otherwise do nothing
var childOnlykillKeys = [...]tea.KeyType{tea.KeyEscape}

// ChildKillKeys returns the list of bubble tea combinations that act as child-only kills by Mother.
func ChildKillKeys() [1]tea.KeyType {
	return childOnlykillKeys
}

// CheckKillKeys returns if the given message is a global kill key, a child kill key, or not a kill key (or even key message at all).
func CheckKillKeys(msg tea.Msg) Kill {
	keyMsg, isKeyMsg := msg.(tea.KeyMsg)
	if !isKeyMsg {
		return None
	}

	// check global keys
	for _, kKey := range globalKillKeys {
		if keyMsg.Type == kKey {
			return Global
		}
	}

	for _, kKey := range childOnlykillKeys {
		if keyMsg.Type == kKey {
			return Child
		}
	}

	return None
}
