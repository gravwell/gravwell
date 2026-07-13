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
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pallinder/go-randomdata"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection/credprompt"
	"github.com/stretchr/testify/assert"
)

// Tests the public, exposed Collect api and that I/O can be redirected.
func TestCollect(t *testing.T) {
	var (
		usernamePrefill = "half"      // what text is prefilled in the username field via parameter
		usernameEntered = "-username" // what text will be entered from in
		password        = "mypass"
		logpath         = filepath.Join(t.TempDir(), randomdata.Alphanumeric(10)+".txt")
	)

	// clilog needs to be spinning
	clilog.InitializeFromArgs([]string{"--" + clilog.FlagLogPath.Name + "=" + logpath})

	// predefine IO
	var (
		in  = bytes.NewBufferString(usernameEntered + "\r" + password + "\r") // REMINDER: BubbleTea reads carriage returns as "enter"
		out = strings.Builder{}
	)
	gotUser, gotPass, gotErr := credprompt.Collect(usernamePrefill, in, &out) // prefill
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	assert.Equal(t, usernamePrefill+usernameEntered, gotUser)
	assert.Equal(t, password, gotPass)

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
}
