// Killer provides a consistent interface for checking a uniform set of kill keys.
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
var globalKillKeys = [...]tea.KeyType{tea.KeyCtrlC}

// keys that kill the child if it exists, otherwise do nothing
var childOnlykillKeys = [...]tea.KeyType{tea.KeyEscape}

// given a message, returns if it is a global kill, a child kill, or not a kill
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
