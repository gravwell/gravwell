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

// TTCtrlC submits a CTRL-C KeyMsg to the test model, proc'ing a sigint.
//
// For use with TeaTests.
func TTCtrlC(tm *teatest.TestModel) {
	tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC, Runes: []rune{rune(tea.KeyCtrlC)}}))
}

//#endregion TeaTest

// ExpectedActual returns a string declaring what was expected and what we got instead.
// ! Prefixes the string with a newline.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%+v'\n\tGot:'%+v'", expected, actual)
}
