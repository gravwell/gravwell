package mfaprompt

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
)

func Test_collect(t *testing.T) {
	tests := []struct {
		name             string
		input            func(prog *tea.Program)
		expectedCode     string // TOTP or recovery
		expectedAuthType types.AuthType
		expectedErr      error
	}{
		{"TOTP", func(prog *tea.Program) {
			prog.Send(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'u'}}))
			testsupport.TTSendSpecial(prog, tea.KeyEnter)
		}, "u", types.AUTH_TYPE_TOTP, nil},
		{"killed", func(prog *tea.Program) {
			testsupport.TTSendSpecial(prog, tea.KeyCtrlC)
		}, "", types.AUTH_TYPE_NONE, uniques.ErrMustAuth},
		/*{"code validator", func(prog *tea.Program) {
			testsupport.Type(prog, "1a2b3c4d5e6f7g") // -> 123456
			testsupport.TTSendSpecial(prog, tea.KeyEnter)
		}, "123456", types.AUTH_TYPE_TOTP, nil},*/
		{"recovery", func(prog *tea.Program) {
			testsupport.TTSendSpecial(prog, tea.KeyTab)
			testsupport.Type(prog, "some1 long2 recovery3 key!") // -> 123456
			testsupport.TTSendSpecial(prog, tea.KeyEnter)
		}, "some1 long2 recovery3 key!", types.AUTH_TYPE_RECOVERY, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := make(chan struct {
				code string
				at   types.AuthType
				err  error
			})

			// spawn a model
			m := New()
			read, _, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			// make the model read out of an open pipe
			prog := tea.NewProgram(m, tea.WithInput(read))

			// spin off the actual TUI via Collect()
			go func() {
				c, at, err := collect(prog)

				result <- struct {
					code string
					at   types.AuthType
					err  error
				}{c, at, err}
			}()

			// send in mock-user input
			tt.input(prog)

			// await results
			r := <-result
			if r.err != tt.expectedErr {
				t.Error("Unexpected error:", testsupport.ExpectedActual(tt.expectedErr, r.err))
			} else if r.at != tt.expectedAuthType {
				t.Error("Unexpected auth type:", testsupport.ExpectedActual(tt.expectedAuthType, r.at))
			} else if r.code != tt.expectedCode {
				t.Error("Unexpected code:", testsupport.ExpectedActual(tt.expectedCode, r.code))
			}
		})
	}
}
