/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother

import (
	"errors"
	"path"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/cobra"
)

/*
root
├─ Anav
├─ Bnav
│  ├─ BAaction
│  ├─ BBaction
│  ├─ BCaction
├─ Cnav
│  ├─ CAaction
│  ├─ CBaction
│  ├─ CCnav
│  │  ├─ CCAaction
├─ Daction
*/
func Test_walk(t *testing.T) {
	// clilog needs to be spinning
	if err := clilog.Init(path.Join(t.TempDir(), t.Name()+".log"), "DEBUG"); err != nil {
		t.Fatal("failed to spin up logger: ", err)
	}

	root := newNav("root", "short", "long", nil, []*cobra.Command{
		newNav("Anav", "short", "long", []string{"Anav_alias"}, nil),
		newNav("Bnav", "short", "long", nil, []*cobra.Command{
			newAction("BAaction", "short", "long", nil),
			newAction("BBaction", "short", "long", nil),
			newAction("BCaction", "short", "long", []string{"BCaction_alias1", "BCaction_alias2", "BCaction_alias3"}),
		}),
		newNav("Cnav", "short", "long", nil, []*cobra.Command{
			newAction("CAaction", "short", "long", nil),
			newAction("CBaction", "short", "long", nil),
			newNav("CCnav", "short", "long", []string{"CCnav_alias"}, []*cobra.Command{
				newAction("CCAaction", "short", "long", nil),
			}),
		}),
		newAction("Daction", "short", "long", nil),
	})

	// prepare builtins
	initBuiltins()

	type result struct {
		commandName     string
		status          walkStatus
		builtinFunc     func(*Mother, []string) tea.Cmd
		remainingString string
	}

	tests := []struct {
		name    string
		pwdPath string // if given, walks to this location and sets it as dir before passing in tokens
		input   string // string to tokenize and feed to walk
		want    result
	}{
		{"first level nav", "", "Anav", result{"Anav", foundNav, nil, ""}},
		{"first level nav alias", "", "Anav_alias", result{"Anav", foundNav, nil, ""}},
		{"second level action", "", "Bnav BCaction", result{"BCaction", foundAction, nil, ""}},
		{"start at CCnav", "Cnav CCnav", "CCAaction", result{"CCAaction", foundAction, nil, ""}},
		{"circuitous route", "Cnav CCnav", ".. .. Bnav ~ Cnav CBaction", result{"CBaction", foundAction, nil, ""}},
		// TODO builtins
		// TODO -h flag
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startingDir := root
			// walk to pwd, if given
			if tt.pwdPath != "" {
				if c, tkns, err := root.Find(strings.Split(strings.TrimSpace(tt.pwdPath), " ")); err != nil {
					t.Fatal(err)
				} else if len(tkns) > 0 {
					t.Error("found extra tokens: ", tkns)
				} else {
					startingDir = c
				}
			}

			actual := walk(startingDir, strings.Split(strings.TrimSpace(tt.input), " "))
			if err := testWalkResult(actual, tt.want); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func newNav(use, short, long string, aliases []string, children []*cobra.Command) *cobra.Command {
	root := &cobra.Command{
		Use:     use,
		Short:   strings.ToLower(short),
		Long:    long,
		Aliases: aliases,
		GroupID: group.NavID,
		Run:     func(cmd *cobra.Command, args []string) {},
	}
	group.AddNavGroup(root)
	group.AddActionGroup(root)

	root.AddCommand(children...)

	return root
}

func newAction(use, short, long string, aliases []string) *cobra.Command {
	root := &cobra.Command{
		Use:     use,
		Short:   strings.ToLower(short),
		Long:    long,
		Aliases: aliases,
		GroupID: group.ActionID,
		Run:     func(cmd *cobra.Command, args []string) {},
	}

	return root
}

func testWalkResult(wr walkResult, want struct {
	commandName     string
	status          walkStatus
	builtinFunc     func(*Mother, []string) tea.Cmd
	remainingString string
}) error {
	if wr.endCommand != nil && (wr.endCommand.Name() != want.commandName) {
		return errors.New(ExpectedActual(want.commandName, wr.endCommand.Name()))
	} else if wr.status != want.status {
		return errors.New("bad status." + ExpectedActual(want.status, wr.status))
	} else if !reflect.DeepEqual(wr.builtinFunc, want.builtinFunc) {
		return errors.New("bad built-in func." + ExpectedActual(wr.builtinFunc, want.builtinFunc))
	} else if wr.remainingString != want.remainingString {
		return errors.New(ExpectedActual(want.remainingString, wr.remainingString))
	}

	return nil
}
