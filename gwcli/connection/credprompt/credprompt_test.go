package credprompt

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

type output struct {
	user         string
	pass         string
	killed       bool
	userSelected bool
}

// NOTE: this testing package relies on teatest, which is an experimental package at the time of authorship (~June 2025).

// TestCredPrompt_TeaTest runs interactivity tests against the cred prompt model
func TestCredPrompt_TeaTest(t *testing.T) {
	tests := []struct {
		name             string
		input            func(tm *teatest.TestModel, expected output) // used to send messages to the model, to mimic user input
		expected         output                                       // what the final model's values should look like
		timeoutDur       time.Duration                                // stop waiting on the final model after this much time
		expectingTimeout bool                                         // are we expecting this test to timeout while awaiting the final model
	}{
		{"normal u/p", func(tm *teatest.TestModel, expected output) {
			tm.Type(expected.user)
			testsupport.TTSendSpecial(tm, tea.KeyEnter)
			tm.Type(expected.pass)
			testsupport.TTSendSpecial(tm, tea.KeyEnter)
		}, output{"Blitzo", "TheOIsSilent", false, false}, 2 * time.Second, false},
		{"garbage after submitting", func(tm *teatest.TestModel, expected output) {
			tm.Type(expected.user)
			testsupport.TTSendSpecial(tm, tea.KeyEnter)
			tm.Type(expected.pass)
			testsupport.TTSendSpecial(tm, tea.KeyEnter)

			// this should not be captured by the prompt
			tm.Type("should not be caught")
			tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC, Runes: []rune{rune(tea.KeyCtrlC)}}))
		}, output{"Moxxie", "Milly", false, false}, 2 * time.Second, false},
		{"global kill key", func(tm *teatest.TestModel, expected output) {
			tm.Type(expected.user)
			testsupport.TTSendSpecial(tm, tea.KeyTab)
			tm.Type(expected.pass)

			// kill with a sigint
			testsupport.TTSendSpecial(tm, tea.KeyCtrlC)
			// this should not be captured by the prompt
			tm.Type("should not be caught")
		}, output{"Stolas", "Blitzy", true, false}, 2 * time.Second, false},
		{"child kill key", func(tm *teatest.TestModel, expected output) {
			tm.Type(expected.user)

			// kill with a sigint
			testsupport.TTSendSpecial(tm, tea.KeyEsc)

			// this should not be captured by the prompt
			tm.Type("should not be caught")
		}, output{"Loona", "", true, true}, 2 * time.Second, false},
		{"wrap", func(tm *teatest.TestModel, expected output) {
			tm.Type(expected.user)
			testsupport.TTSendSpecial(tm, tea.KeyDown)
			tm.Type(expected.pass)
			testsupport.TTSendSpecial(tm, tea.KeyDown)
			testsupport.TTSendSpecial(tm, tea.KeyShiftTab)
			testsupport.TTSendSpecial(tm, tea.KeyEnter)
		}, output{"Fizzarolli", "Oops", false, false}, 1 * time.Second, false},
		{"timeout", func(tm *teatest.TestModel, expected output) {
			testsupport.TTSendSpecial(tm, tea.KeyEnter)
			tm.Type("some password that should not get returned")
			testsupport.TTSendSpecial(tm, tea.KeyUp)
		}, output{}, 3 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, ch := spawnModel(t)

			// execute commands against the test model
			tt.input(tm, tt.expected)

			// check results
			out, timedout := awaitFinal(t, ch, tt.timeoutDur)
			compareFinal(t, out, tt.expected, timedout, tt.expectingTimeout)
		})
	}

}

// Test_collect tests .Collect() via the internal subroutine that .Collect() calls under the hood.
// Does not actually rely on teatest; instead, passes a tea program with a mocked input and interacts via external .Send()ing.
func Test_collect(t *testing.T) {

	tests := []struct {
		name         string
		input        func(prog *tea.Program)
		expectedUser string
		expectedPass string
		expectedErr  error
	}{
		{"normal u/p", func(prog *tea.Program) {
			prog.Send(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'u'}}))
			testsupport.TTSendSpecial(prog, tea.KeyEnter)
			testsupport.TTSendSpecial(prog, tea.KeyEnter)

		}, "u", "", nil},
		{"killed", func(prog *tea.Program) {
			testsupport.TTSendSpecial(prog, tea.KeyCtrlC)
		}, "", "", uniques.ErrMustAuth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := make(chan struct {
				user string
				pass string
				err  error
			})

			// spawn a model
			m := New("")
			read, _, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			// make the model read out of an open pipe
			prog := tea.NewProgram(m, tea.WithInput(read))

			// spin off the actual TUI via Collect()
			go func() {
				u, p, err := collect("", prog)

				result <- struct {
					user string
					pass string
					err  error
				}{u, p, err}
			}()

			// send in mock-user input
			tt.input(prog)

			// await results
			r := <-result
			if r.err != tt.expectedErr {
				t.Error("Unexpected error:", testsupport.ExpectedActual(tt.expectedErr, r.err))
			} else if r.user != tt.expectedUser {
				t.Error("Unexpected user:", testsupport.ExpectedActual(tt.expectedErr, r.err))
			} else if r.pass != tt.expectedPass {
				t.Error("Unexpected password:", testsupport.ExpectedActual(tt.expectedErr, r.err))
			}
		})
	}

}

// spawnModel spins off a credprompt returns a channel that can be read from after the model exists to get its final state.
func spawnModel(t *testing.T) (*teatest.TestModel, chan tea.Model) {
	t.Helper()
	// create a channel for us to receive the final model on
	result := make(chan tea.Model)

	// spawn a model
	m := New("")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(300, 100))
	go func() {
		final := tm.FinalModel(t, teatest.WithFinalTimeout(10*time.Second))
		result <- final
		close(result)
	}()

	return tm, result
}

// awaitFinal pulls data from the final struct and returns it for easy evaluation.
//
// Returns the final values contained in the credmodel or that we timed-out waiting for it.
func awaitFinal(t *testing.T, ch <-chan tea.Model, timeout time.Duration) (output, bool) {
	t.Helper()

	select {
	case final := <-ch:
		cm, ok := final.(credModel)
		if !ok {
			t.Fatal("failed to assert final model to a credModel")
		}
		// check the results
		return output{cm.UserTI.Value(), cm.PassTI.Value(), cm.killed, cm.userSelected}, false

	case <-time.After(timeout):
		return output{}, true
	}
}

// compareFinal tests the actual output of awaitFinal against the expected output of awaitFinal, erroring on deltas.
func compareFinal(t *testing.T, actual, expected output, timedOut, expectedTimedOut bool) {
	t.Helper()

	if timedOut != expectedTimedOut {
		t.Fatal("timed out waiting for credModel to finish")
	} else if actual != expected {
		t.Error("incorrect final state:", testsupport.ExpectedActual(expected, actual))
	}
}

// NOTE: This test does not work because bubbletea is unable to open a tty on the mocked stdin port.
// The logic is sound, but bubbletea is not compatible with it, hence why the other tests rely on teatest.
// I am leaving it as relic code to showcase that fact.
/*func TestManualCredPrompt(t *testing.T) {
	//#region capture stdin so we can send data into it

	// create a pipe to use instead
	_, writeMockSTDIN, err := os.Pipe()
	if err != nil {
		t.Fatal("failed to create stdin pipes:", err)
	}
	origSTDIN := os.Stdin
	os.Stdin = writeMockSTDIN
	t.Cleanup(func() { os.Stdin = origSTDIN })

	//#endregion

	// capture stdout so we can get outputs
	// TODO

	// create a pipe to pull username, password, and error
	results := make(chan struct {
		username string
		password string
		err      error
	})

	t.Run("basic", func(t *testing.T) {
		// spin out a goro to wait on Collect
		go func() {
			u, p, err := Collect("")
			results <- struct {
				username string
				password string
				err      error
			}{u, p, err}
			close(results)
		}()

		// give collect a few moments to spin up
		time.Sleep(time.Second)

		// send username into Collect
		if _, err := writeMockSTDIN.Write([]byte("somename")); err != nil {
			t.Fatal()
		}
		// switch to password
		if _, err := writeMockSTDIN.Write([]byte("\n")); err != nil {
			t.Fatal()
		}
		// send username into Collect
		if _, err := writeMockSTDIN.Write([]byte("somepass")); err != nil {
			t.Fatal()
		}
		// push
		if _, err := writeMockSTDIN.Write([]byte("\n")); err != nil {
			t.Fatal()
		}

		// await the outcome
		r := <-results
		if r.err != nil {
			t.Fatal(err)
		}
		t.Logf("%+v", r)
		t.Fatal()
	})

}*/
