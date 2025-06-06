// Package testsupport provides utility functions useful across disparate testing packages
// TT* functions are for use with tests that rely on TeaTest.
package testsupport

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// TTSendWord breaks each character into its own key message and sends each to the given TestModel.
//
// For use with TeaTests.
func TTSendWord(t *testing.T, tm *teatest.TestModel, characters []rune) {
	//testing.Coverage()
	if tm == nil {
		t.Log("test model is nil. Skipping send.")
		return
	} else if len(characters) == 0 {
		t.Log("no characters given. Skipping send.")
		return
	}

	for _, r := range characters {
		tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{r}}))
	}
	t.Logf("sent word '%v' as %v messages", string(characters), len(characters))
}

// TTSendEnter submits a single enter KeyMsg to the test model.
//
// For use with TeaTests.
func TTSendEnter(tm *teatest.TestModel) {
	tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Runes: []rune{rune(tea.KeyEnter)}}))
}
