package credprompt

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

// NOTE: this testing package relies on teatest, which is an experimental package at the time of authorship (~June 2025).
//
// NOTE 2: as this relies on teatest, you will need a "golden" file, which can be generated via go test -v ./... -update.
// A golden file provides the output/View of the program for automated testing purposes.
// See [this](https://charm.sh/blog/teatest/) blog post for more information.

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

// TestCredPrompt_TeaTest runs interactivity tests against the cred prompt model
func TestCredPrompt_TeaTest(t *testing.T) {
	t.Run("standard submission", func(t *testing.T) {
		inUser, inPass := "Blitzo", "TheOIsSilent"
		tm, ch := spawnModel(t)

		tm.Type(inUser)
		testsupport.TTSendEnter(tm)
		tm.Type(inPass)
		testsupport.TTSendEnter(tm) // submit

		// check results
		u, p, _, _ := parseFinal(t, <-ch)
		if u != inUser && p != inPass {
			t.Fatalf("Unexpected values in TIs: '%v' & '%v'", u, p)
		}
	})
	t.Run("garbage messages after submission", func(t *testing.T) {
		inUser, inPass := "Blitzo", "TheOIsSilent"

		tm, ch := spawnModel(t)

		tm.Type(inUser)
		testsupport.TTSendEnter(tm)
		tm.Type(inPass)
		testsupport.TTSendEnter(tm) // submit

		// this should not be captured by the prompt
		tm.Type("should not be caught")
		tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC, Runes: []rune{rune(tea.KeyCtrlC)}}))

		// check results
		u, p, _, _ := parseFinal(t, <-ch)
		if u != inUser && p != inPass {
			t.Fatalf("Unexpected values in TIs: '%v' & '%v'", u, p)
		}
	})

	t.Run("global kill key", func(t *testing.T) {
		inUser, inPass := "Blitzo", "TheOIsSilent"

		tm, ch := spawnModel(t)

		tm.Type(inUser)
		tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyTab, Runes: []rune{rune(tea.KeyTab)}})) // move to password input
		tm.Type(inPass)

		// kill with a sigint
		tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC, Runes: []rune{rune(tea.KeyCtrlC)}}))

		// this should not be captured by the prompt
		tm.Type("should not be caught")

		// check results
		if u, p, killed, userSelected := parseFinal(t, <-ch); userSelected {
			t.Error("userTI is selected despite an enter being sent")
		} else if !killed {
			t.Error("CTRL+C was sent to the prompt, but it did not mark itself as having been killed")
		} else if u != inUser || p != inPass {
			t.Fatalf("Unexpected values in TIs: '%v'!='%v' or '%v'!='%v'", u, inUser, p, inPass)
		}
	})
	t.Run("child kill key", func(t *testing.T) {})

}

func Test_collect(t *testing.T) {
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
	prog := tea.NewProgram(m, tea.WithInput(read))
	//tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(300, 100))
	//p := tm.GetProgram()

	/*go func() {
		final := tm.FinalModel(t, teatest.WithFinalTimeout(10*time.Second))
		result <- final
		close(result)
	}()*/

	go func() {
		u, p, err := collect("", prog)

		result <- struct {
			user string
			pass string
			err  error
		}{u, p, err}
	}()

	// send data into the program
	/*if _, err := in.Write([]byte("user")); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}*/

	prog.Send(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'u'}}))
	prog.Send(tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Runes: []rune{rune(tea.KeyEnter)}}))
	prog.Send(tea.KeyMsg(tea.Key{Type: tea.KeyEnter, Runes: []rune{rune(tea.KeyEnter)}}))

	//tm.Type("user")
	//testsupport.TTSendEnter(tm)
	//testsupport.TTSendEnter(tm)
	//tm.Send(tea.KeyMsg(tea.Key{Type: tea.KeyCtrlC, Runes: []rune{rune(tea.KeyCtrlC)}}))

	r := <-result
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.user != "u" {
		t.Fatalf("incorrect username: %v", r)
	}

	// TODO
	/*_, _, killed, _ := parseFinal(t, <-result)
	if !killed {
		t.Fatal("not killed")
	}*/
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

// parseFinal pulls data from the final struct and returns it for easy evaluation.
func parseFinal(t *testing.T, final tea.Model) (u, p string, killed, userSelected bool) {
	t.Helper()
	cm, ok := final.(credModel)
	if !ok {
		t.Fatal("failed to assert final model to a credModel")
	}
	// check the results
	return cm.UserTI.Value(), cm.PassTI.Value(), cm.killed, cm.userSelected
}
