//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package credprompt_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection/credprompt"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests the public, exposed Collect api and that I/O can be redirected.
func TestCollect(t *testing.T) {
	tests := []struct {
		name         string
		initialUser  string
		input        func(t *testing.T, in io.Writer)
		expectedUser string
		expectedPass string
		expectedErr  bool
	}{
		{"normal u/p",
			"",
			func(t *testing.T, in io.Writer) {
				in.Write([]byte("user"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.CursorUp)) // wrap
				in.Write([]byte("pass"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
			}, "user", "pass", false},
		{"killed immediately",
			"",
			func(t *testing.T, in io.Writer) { in.Write([]byte{byte(testsupport.SIGINT)}) }, "", "", true},
		{"killed at password entry",
			"init",
			func(t *testing.T, in io.Writer) {
				in.Write(testsupport.HotkeyBytes(t, hotkeys.CursorDown))
				in.Write([]byte{byte(testsupport.SIGINT)})
			}, "", "", true},
		{"complete username from initial input",
			"half",
			func(t *testing.T, in io.Writer) {
				in.Write([]byte("-username"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke)) // should go down to password entry, not submit
				in.Write([]byte("mypass"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.CursorDown)) // wrap around
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
			},
			"half-username", "mypass", false,
		},
		{"throw away initial input",
			"input that should be lost",
			func(t *testing.T, in io.Writer) {
				in.Write([]byte(strings.Repeat(string(testsupport.Delete), len("input that should be lost"))))
				in.Write([]byte("new"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.CursorDown))
				in.Write([]byte("mypass"))
				in.Write(testsupport.HotkeyBytes(t, hotkeys.Invoke))
			},
			"new", "mypass", false,
		},
	}

	for _, tt := range tests {
		clilog.Destroy()
		logpath := filepath.Join(t.TempDir(), randomdata.Alphanumeric(10)+".txt")
		clilog.InitializeFromArgs([]string{"--" + clilog.FlagLogPath.Name + "=" + logpath})

		t.Run(tt.name, func(t *testing.T) {
			result := make(chan struct {
				user string
				pass string
				err  error
			})

			// redirect IO
			read, write, err := os.Pipe()
			require.Nil(t, err)
			sb := strings.Builder{}

			// spin off the actual TUI via Collect()
			go func() {
				u, p, err := credprompt.Collect(tt.initialUser, read, &sb)

				result <- struct {
					user string
					pass string
					err  error
				}{u, p, err}
			}()

			// send in mock-user input
			tt.input(t, write)

			// await results
			r := <-result
			assert.Equal(t, tt.expectedErr, (r.err != nil), "unexpected error. %s. Output: %v", r.err, sb.String())
			assert.Equal(t, tt.expectedUser, r.user, "Output: %v", sb.String())
			assert.Equal(t, tt.expectedPass, r.pass, "Output: %v", sb.String())
		})
	}

}
