// Package testsupport provides utility functions useful across disparate testing packages
//
// TT* functions are for use with tests that rely on TeaTest.
// Friendly reminder: calling tm.Type() with "\n"/"\t"/etc does not, at the time of writing, actually trigger the corresponding key message.
package testsupport

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

//#region TeaTest

// TTEnter submits a single enter KeyMsg to the test model.
//
// For use with TeaTests.
func TTEnter(tm *teatest.TestModel) {
	tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Runes: []rune{rune(tea.KeyEnter)}}))
}

// TTTab submits a single tab KeyMsg to the test model.
//
// For use with TeaTests.
func TTTab(tm *teatest.TestModel) {
	tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyTab, Runes: []rune{rune(tea.KeyTab)}})) // move to password input
}

// TTSendSpecial submits a KeyMsg containing the special key (CTRL+C, ESC, etc) to the test model.
// Ensures the KeyMsg is well-formatted, as ill-formatted KeyMsgs are silently dropped (as they are not read as KeyMsgs) or cause panics.
//
// For use with TeaTests.
func TTSendSpecial(tm *teatest.TestModel, kt tea.KeyType) {
	tm.Send(tea.KeyMsg(tea.Key{Type: kt, Runes: []rune{rune(kt)}}))
}

//#endregion TeaTest

// ExpectedActual returns a string declaring what was expected and what we got instead.
// ! Prefixes the string with a newline.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%+v'\n\tGot:'%+v'", expected, actual)
}
