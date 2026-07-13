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

}
