/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package testsupport provides utility functions useful across disparate testing packages
//
// TT* functions are for use with tests that rely on TeaTest.
// Friendly reminder: calling tm.Type() with "\n"/"\t"/etc does not, at the time of writing, actually trigger the corresponding key message.
package testsupport

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

//#region TeaTest

// A MessageRcvr is anything that can accept tea.Msgs via a .Send() method.
type MessageRcvr interface {
	Send(tea.Msg)
}

// TTSendSpecial submits a KeyMsg containing the special key (CTRL+C, ESC, etc) to the test model.
// Ensures the KeyMsg is well-formatted, as ill-formatted KeyMsgs are silently dropped (as they are not read as KeyMsgs) or cause panics.
//
// For use with TeaTests.
func TTSendSpecial(r MessageRcvr, kt tea.KeyType) {
	r.Send(tea.KeyMsg(tea.Key{Type: kt, Runes: []rune{rune(kt)}}))
}

// Type adds teatest.TestModel.Type() to a normal tea.Program.
func Type(prog *tea.Program, text string) {
	for _, r := range text {
		prog.Send(tea.KeyMsg(
			tea.Key{Type: tea.KeyRunes, Runes: []rune{rune(r)}}))
	}
}

//#endregion TeaTest

// ExpectedActual returns a string declaring what was expected and what we got instead.
// ! Prefixes the string with a newline.
func ExpectedActual(expected, actual any) string {
	return fmt.Sprintf("\n\tExpected:'%+v'\n\tGot:'%+v'", expected, actual)
}

// NonZeroExit calls Fatal if code is <> 0.
func NonZeroExit(t *testing.T, code int, stderr string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("non-zero exit code %v.\nstderr: '%v'", code, stderr)
	}
}

// StartSingletons spins up all the required singletons that actions/tests/commands typically expect to be in place.
// Running tests without the singletons spinning is likely to cause nil panics
// (for example, due to trying to access connection.Client before it has been .Initialize()'d).
//
// Starts the clilog and the connection, logs-in the connection.
//
// Fatal on failure.
/*func StartSingletons(t *testing.T, server, username, password, apiToken string, scriptMode bool) {
if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
	t.Fatal(err)
} else if err := connection.Initialize(server, false, true, path.Join(t.TempDir(), "dev.log")); err != nil {
	t.Fatal(err)
} else if err := connection.Login(username, password, apiToken, scriptMode); err != nil {
	t.Fatal(err)
}
}*/
