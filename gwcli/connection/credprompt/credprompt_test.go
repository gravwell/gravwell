package credprompt

import (
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

		testsupport.TTSendWord(t, tm, []rune(inUser))
		testsupport.TTSendEnter(tm)
		testsupport.TTSendWord(t, tm, []rune(inPass))
		testsupport.TTSendEnter(tm) // submit

		// receive the final output
		f := <-result
		cm, ok := f.(credModel)
		if !ok {
			t.Fatal("failed to assert final model to a credModel")
		}
		// check the results
		user, pass := cm.UserTI.Value(), cm.PassTI.Value()
		if user != inUser && pass != inPass {
			t.Fatalf("Unexpected values in TIs: '%v' & '%v'", user, pass)
		}
	})
	t.Run("garbage messages after submission", func(t *testing.T) {
		inUser, inPass := "Blitzo", "TheOIsSilent"
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

		testsupport.TTSendWord(t, tm, []rune(inUser))
		testsupport.TTSendEnter(tm)
		testsupport.TTSendWord(t, tm, []rune(inPass))
		testsupport.TTSendEnter(tm) // submit

		// this should not be captured by the prompt
		testsupport.TTSendWord(t, tm, []rune("should not be caught"))

		// receive the final output
		f := <-result
		cm, ok := f.(credModel)
		if !ok {
			t.Fatal("failed to assert final model to a credModel")
		}
		// check the results
		user, pass := cm.UserTI.Value(), cm.PassTI.Value()
		if user != inUser && pass != inPass {
			t.Fatalf("Unexpected values in TIs: '%v' & '%v'", user, pass)
		}
	})

	t.Run("global kill key", func(t *testing.T) {})
	t.Run("child kill key", func(t *testing.T) {})

}
