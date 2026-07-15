//go:build !ci
// +build !ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package myinfo

import (
	"bytes"
	"encoding/csv"
	"errors"
	"path"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/connection"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
)

const (
	server   string = "localhost:80"
	username string = "admin"
	password string = "changeme"
)

func TestNewUserMyInfoAction(t *testing.T) {
	dir := t.TempDir()

	// spin up logger and connect to the backend
	if err := clilog.Init(path.Join(dir, "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	} else if err := connection.Initialize(server, false, true, path.Join(dir, "dev.log")); err != nil {
		t.Fatal(err)
	} else if err := connection.Login(username, password, "", true); err != nil {
		t.Fatal(err)
	}

	pair := NewUserMyInfoAction()
	// test non-interactive
	/*niOut :=*/
	niNormal, niCSV := nonInteractive(t, pair.Action)
	// test interactive
	mdlNormal, mdlCSV := model(t, pair.Model)

	// clean and compare outputs
	niNormal = strings.TrimSpace(niNormal)
	niCSV = strings.TrimSpace(niCSV)
	mdlNormal = strings.TrimSpace(mdlNormal)
	mdlCSV = strings.TrimSpace(mdlCSV)

	if niNormal != mdlNormal {
		t.Errorf("mismatch between invocations (normal output).\ncommand: '%v'\nmodel: '%v'", niNormal, mdlNormal)
	}
	if niCSV != mdlCSV {
		t.Errorf("mismatch between invocations (CSV output).\ncommand: '%v'\nmodel: '%v'", niCSV, mdlCSV)
	}
}

// runs the command externally, returning the normal and csv results
func nonInteractive(t *testing.T, action *cobra.Command) (normalOut, csvOut string) {
	uniques.AttachPersistentFlags(action)
	if err := action.Execute(); err != nil {
		t.Fatal(err)
	}
	// capture output
	outBuf := &bytes.Buffer{}
	action.SetOut(outBuf)
	errBuf := &bytes.Buffer{}
	action.SetErr(errBuf)
	action.SetArgs(nil)
	if err := action.Execute(); err != nil {
		t.Fatal(err)
	}
	normalOut = outBuf.String()
	outBuf.Reset()
	t.Log(normalOut)

	// fetch the SV version
	action.SetArgs([]string{"--csv"})
	if err := action.Execute(); err != nil {
		t.Fatal(err)
	}
	csvOut = strings.TrimSpace(outBuf.String())

	if err := validateCSV(csvOut); err != nil {
		t.Error(err)
	}
	return normalOut, csvOut
}

func model(t *testing.T, mdl action.Model) (normalOut, csvOut string) {
	{
		fs := flags()
		if inv, cmd, err := mdl.SetArgs(&fs, nil, 80, 50); err != nil {
			t.Fatal(err)
		} else if inv != "" {
			t.Fatal("failed to set args due invalid arguments: ", inv)
		} else if cmd != nil {
			t.Fatal("basic actions are not expected to return a command from SetArgs")
		}
	}
	cmd := mdl.Update(tea.WindowSizeMsg{Width: 0, Height: 0}) // dummy message
	if cmd == nil {
		t.Fatal("basic action did not return a command")
	}
	// rip the string out of the command
	// myinfo does not return an extra command, so this should NOT be a sequence
	normalOut = ExtractPrintLineMessageString(t, cmd, false, 0)
	// continue Mother's cycle
	if v := mdl.View(); v != "" {
		t.Error("basic actions should not return a view.", ExpectedActual("", v))
	}
	if !mdl.Done() {
		t.Error("basic actions should be done after a single cycle")
	}
	if err := mdl.Reset(); err != nil {
		t.Error(err)
	}

	// run it back, but for CSV
	{
		fs := flags()
		if inv, cmd, err := mdl.SetArgs(&fs, []string{"--csv"}, 80, 50); err != nil {
			t.Fatal(err)
		} else if inv != "" {
			t.Fatal("failed to set args due invalid arguments: ", inv)
		} else if cmd != nil {
			t.Fatal("basic actions are not expected to return a command from SetArgs")
		}
	}
	cmd = mdl.Update(tea.WindowSizeMsg{Width: 0, Height: 0}) // dummy message
	if cmd == nil {
		t.Fatal("basic action did not return a command")
	}
	// rip the string out of the command
	// myinfo does not return an extra command, so this should NOT be a sequence
	csvOut = ExtractPrintLineMessageString(t, cmd, false, 0)
	// continue Mother's cycle
	if v := mdl.View(); v != "" {
		t.Error("basic actions should not return a view.", ExpectedActual("", v))
	}
	if !mdl.Done() {
		t.Error("basic actions should be done after a single cycle")
	}
	if err := mdl.Reset(); err != nil {
		t.Error(err)
	}

	if err := validateCSV(csvOut); err != nil {
		t.Error(err)
	}

	return normalOut, csvOut
}

func validateCSV(csvStr string) error {
	// perform some basic validation
	if exploded := strings.Split(csvStr, "\n"); len(exploded) != 2 {
		return errors.New("bad CSV line count." + ExpectedActual(2, len(exploded)))
	}
	csvR := csv.NewReader(strings.NewReader(csvStr))
	if _, err := csvR.ReadAll(); err != nil {
		return errors.New("failed to read CSV: " + err.Error())
	}
	return nil
}
