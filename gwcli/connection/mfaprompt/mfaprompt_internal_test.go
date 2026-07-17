//go:build ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mfaprompt_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection/mfaprompt"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/stretchr/testify/require"
)

func Test_Collect(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "log.txt")
	require.Nil(t, clilog.Init(logPath, "DEBUG"))

	tests := []struct {
		name             string
		input            func(in io.Writer)
		expectedCode     string // TOTP or recovery
		expectedAuthType types.AuthType
		expectedErr      bool
	}{
		{"TOTP", func(in io.Writer) {
			in.Write([]byte("u\r"))
		}, "u", types.AUTH_TYPE_TOTP, false},
		{"killed", func(in io.Writer) {
			in.Write([]byte("\003"))
		}, "", types.AUTH_TYPE_NONE, true},
		/*{"code validator", func(prog *tea.Program) {
			testsupport.Type(prog, "1a2b3c4d5e6f7g") // -> 123456
			testsupport.TTSendSpecial(prog, tea.KeyEnter)
		}, "123456", types.AUTH_TYPE_TOTP, nil},*/
		{"recovery", func(in io.Writer) {
			in.
				testsupport.TTSendSpecial(prog, testsupport.SendHotkey(hotkeys.CursorDown).Type)
			testsupport.Type(prog, "some1 long2 recovery3 key!") // -> 123456
			testsupport.TTSendSpecial(prog, testsupport.SendHotkey(hotkeys.Invoke).Type)
		}, "some1 long2 recovery3 key!", types.AUTH_TYPE_RECOVERY, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := make(chan struct {
				code string
				at   types.AuthType
				err  error
			})

			// spawn a model
			read, write, err := os.Pipe()
			require.Nil(t, err)

			// spin off the actual TUI via Collect()
			go func() {
				c, at, err := mfaprompt.Collect(read, nil)

				result <- struct {
					code string
					at   types.AuthType
					err  error
				}{c, at, err}
			}()

			// send in mock-user input
			tt.input(write)

			// await results
			r := <-result
			if (r.err != nil) != tt.expectedErr {
				t.Error("Unexpected error:", testsupport.ExpectedActual(tt.expectedErr, r.err))
			} else if r.at != tt.expectedAuthType {
				t.Error("Unexpected auth type:", testsupport.ExpectedActual(tt.expectedAuthType, r.at))
			} else if r.code != tt.expectedCode {
				t.Error("Unexpected code:", testsupport.ExpectedActual(tt.expectedCode, r.code))
			}
		})
	}
}
