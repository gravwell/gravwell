//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mfaprompt_test

import (
	"bytes"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/client/types"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection/mfaprompt"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect(t *testing.T) {
	t.Run("TOTP", func(t *testing.T) {
		clilog.Destroy()
		var (
			codeEntered = strconv.FormatUint(rand.Uint64N(80000), 10) // what text will be entered from in
			logpath     = filepath.Join(t.TempDir(), randomdata.Alphanumeric(10)+".txt")
		)
		// clilog needs to be spinning
		clilog.InitializeFromArgs([]string{"--" + clilog.FlagLogPath.Name + "=" + logpath})

		// predefine IO
		var (
			in  = bytes.NewBufferString(codeEntered + "\r") // REMINDER: BubbleTea reads carriage returns as "enter"
			out = strings.Builder{}
		)
		gotCode, gotTyp, gotErr := mfaprompt.Collect(in, &out) // prefill
		require.Nil(t, gotErr)
		assert.Equal(t, codeEntered, gotCode)
		assert.Equal(t, types.AUTH_TYPE_TOTP, gotTyp)

		if t.Failed() {
			// let's see what is in the output buffer
			t.Log("final output:\n", out.String())

			b, err := os.ReadFile(logpath)
			if err != nil {
				t.Log("failed to dump log file:", err)
			} else {
				t.Log(string(b))
			}
		}
	})
	t.Run("Recovery", func(t *testing.T) {
		clilog.Destroy()
		var (
			codeEntered = strconv.FormatUint(rand.Uint64N(80000), 10) // what text will be entered from in
			logpath     = filepath.Join(t.TempDir(), randomdata.Alphanumeric(10)+".txt")
		)
		// clilog needs to be spinning
		clilog.InitializeFromArgs([]string{"--" + clilog.FlagLogPath.Name + "=" + logpath})

		// predefine IO
		var (
			// keydown + recovery code + enter
			in  = bytes.NewBufferString("\x1b[B" + codeEntered + "\r") // REMINDER: BubbleTea reads carriage returns as "enter"
			out = strings.Builder{}
		)
		gotCode, gotTyp, gotErr := mfaprompt.Collect(in, &out) // prefill
		require.Nil(t, gotErr)
		assert.Equal(t, codeEntered, gotCode)
		assert.Equal(t, types.AUTH_TYPE_RECOVERY, gotTyp)

		if t.Failed() {
			// let's see what is in the output buffer
			t.Log("final output:\n", out.String())

			b, err := os.ReadFile(logpath)
			if err != nil {
				t.Log("failed to dump log file:", err)
			} else {
				t.Log(string(b))
			}
		}
	})

	t.Run("redundant tests", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "log.txt")
		require.Nil(t, clilog.Init(logPath, "DEBUG"))

		tests := []struct {
			name             string
			input            func(t *testing.T, in io.Writer)
			expectedCode     string // TOTP or recovery
			expectedAuthType types.AuthType
			expectedErr      bool
		}{
			{"TOTP", func(t *testing.T, in io.Writer) {
				in.Write([]byte("u"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
			}, "u", types.AUTH_TYPE_TOTP, false},
			{"killed", func(t *testing.T, in io.Writer) {
				in.Write([]byte{byte(testsupport.SIGINT)})
			}, "", types.AUTH_TYPE_NONE, true},
			/*{"code validator", func(prog *tea.Program) {
				testsupport.Type(prog, "1a2b3c4d5e6f7g") // -> 123456
				testsupport.TTSendSpecial(prog, tea.KeyEnter)
			}, "123456", types.AUTH_TYPE_TOTP, nil},*/
			{"recovery", func(t *testing.T, in io.Writer) {
				in.Write(testsupport.HotkeyBytes(t, hotkeys.CursorDown))
				in.Write([]byte("some1 long2 recovery3 key!"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
			}, "some1 long2 recovery3 key!", types.AUTH_TYPE_RECOVERY, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := make(chan struct {
					code string
					at   types.AuthType
					err  error
				})

				// redirect IO
				read, write, err := os.Pipe()
				require.Nil(t, err)
				sb := strings.Builder{}

				// spin off the actual TUI via Collect()
				go func() {
					c, at, err := mfaprompt.Collect(read, &sb)

					result <- struct {
						code string
						at   types.AuthType
						err  error
					}{c, at, err}
				}()

				// send in mock-user input
				tt.input(t, write)

				// await results
				r := <-result
				assert.Equal(t, tt.expectedErr, (r.err != nil), "unexpected error. %s. Output: %v", r.err, sb.String())
				assert.Equal(t, tt.expectedAuthType, r.at, "Output: %v", sb.String())
				assert.Equal(t, tt.expectedCode, r.code, "Output: %v", sb.String())
			})
		}
	})
}
