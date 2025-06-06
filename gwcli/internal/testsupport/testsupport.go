// Package testsupport provides utility functions useful across disparate testing packages
// TT* functions are for use with tests that rely on TeaTest.
package testsupport

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TTSendEnter submits a single enter KeyMsg to the test model.
//
// For use with TeaTests.
func TTSendEnter(tm *teatest.TestModel) {
	tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Runes: []rune{rune(tea.KeyEnter)}}))
}
