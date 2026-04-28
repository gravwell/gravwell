/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package uniques

import (
	"slices"
	"strings"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/group"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/cobra"
)

type ExpectedWalkResult struct {
	commandName     string
	remainingTokens []string
	builtin         string
	helpMode        bool
	err             bool
}

func TestWalk(t *testing.T) {
	// build a tree to walk
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

	builtins := []string{"builtin1", "builtin2", "help", "jump", "ls"}

	tests := []struct {
		name    string
		pwdPath string // if given, walks to this location and sets it as dir before passing in tokens
		input   string // string to tokenize and feed to walk
		want    ExpectedWalkResult
	}{
		// edge cases
		{"empty input", "", "",
			ExpectedWalkResult{commandName: "root", remainingTokens: nil, builtin: "", helpMode: false, err: false}},

		// navigation
		{"first level nav", "", "Anav",
			ExpectedWalkResult{commandName: "Anav", remainingTokens: nil, builtin: "", helpMode: false, err: false}},
		{"first level nav alias", "", "Anav_alias", ExpectedWalkResult{"Anav", nil, "", false, false}},
		{"upward from root", "", "..", ExpectedWalkResult{"root", nil, "", false, false}},
		{"rootward from root", "", "~", ExpectedWalkResult{"root", nil, "", false, false}},
		{"rootward from root", "", "/", ExpectedWalkResult{"root", nil, "", false, false}},
		{"rootward", "Cnav", "/", ExpectedWalkResult{"root", nil, "", false, false}},
		{"unknown first token", "", "bad", ExpectedWalkResult{"root", nil, "", false, true}},
		{"second level action", "", "Bnav BCaction", ExpectedWalkResult{"BCaction", nil, "", false, false}},
		{"start at CCnav", "Cnav CCnav", "CCAaction", ExpectedWalkResult{"CCAaction", nil, "", false, false}},
		{"circuitous route", "Cnav CCnav", ".. .. Bnav ~ Cnav CBaction",
			ExpectedWalkResult{"CBaction", nil, "", false, false}},
		{"circuitous route with excess whitespace", "    Cnav CCnav", "..    .. Bnav ~   Cnav CBaction  ",
			ExpectedWalkResult{"CBaction", nil, "", false, false}},

		// builtins
		{"simple builtin", "", "builtin1",
			ExpectedWalkResult{"root", nil, "builtin1", false, false}},
		{"builtin with excess tokens", "", "builtin1 some extra tokens",
			ExpectedWalkResult{"root", []string{"some", "extra", "tokens"}, "builtin1", false, false}},
		{"interspersed builtin", "", "Bnav builtin1",
			ExpectedWalkResult{"Bnav", nil, "builtin1", false, false}},
		{"interspersed builtin", "", "Bnav builtin1 excess",
			ExpectedWalkResult{"Bnav", []string{"excess"}, "builtin1", false, false}},

		// bare help
		{"bare help", "", "help",
			ExpectedWalkResult{"root", nil, "", true, false}},
		{"bare help, extra token", "", "help Anav",
			ExpectedWalkResult{"Anav", []string{}, "", true, false}},
		{"bare help on builtin", "", "help jump",
			ExpectedWalkResult{"root", []string{}, "jump", true, false}},
		{"help help", "", "help help",
			ExpectedWalkResult{"root", []string{}, "help", true, false}},
		{"help help excess tokens", "", "help help excess tokens",
			ExpectedWalkResult{"root", []string{"excess", "tokens"}, "help", true, false}},
		{"help help on non-root nav", "Anav", "help help",
			ExpectedWalkResult{"Anav", []string{}, "help", true, false}},
		{"interspersed help", "", "Cnav help CCnav",
			ExpectedWalkResult{"Cnav", []string{"CCnav"}, "help", false, true}},
		{"interspersed help", "", "Cnav help CCnav CCAaction",
			ExpectedWalkResult{"Cnav", []string{"CCnav", "CCAaction"}, "help", false, true}},
		{"interspersed help", "", "Cnav CCnav help CCAaction",
			ExpectedWalkResult{commandName: "CCnav", remainingTokens: []string{"CCAaction"}, builtin: "help", helpMode: false, err: true}},
		{"interspersed help help", "", "help Cnav CCnav help CCAaction",
			ExpectedWalkResult{commandName: "CCnav", remainingTokens: []string{"CCAaction"}, builtin: "help", helpMode: false, err: true}},

		// help flag
		{"help flag, shortform on root", "", "-h",
			ExpectedWalkResult{commandName: "root", remainingTokens: nil, builtin: "", helpMode: true, err: false}},
		{"help flag, longform on root", "", "--help",
			ExpectedWalkResult{commandName: "root", remainingTokens: nil, builtin: "", helpMode: true, err: false}},
		{"help flag on non-root pwd", "Anav", "--help",
			ExpectedWalkResult{commandName: "Anav", remainingTokens: nil, builtin: "", helpMode: true, err: false}},
		{"help flag on remote nav", "", "Bnav --help",
			ExpectedWalkResult{commandName: "Bnav", remainingTokens: nil, builtin: "", helpMode: true, err: false}},
		{"help flag on remote action", "", "Bnav BAaction --help",
			ExpectedWalkResult{commandName: "BAaction", remainingTokens: nil, builtin: "", helpMode: true, err: false}},
		{"help flag on builtin", "", "jump --help",
			ExpectedWalkResult{commandName: "root", remainingTokens: nil, builtin: "jump", helpMode: true, err: false},
		},

		// other flags
		{"help flag, shortform on root", "", "-h --flag=1",
			ExpectedWalkResult{commandName: "root", remainingTokens: []string{"--flag=1"}, builtin: "", helpMode: true, err: false}},
		{"stop nav on first flag", "", "Cnav --flag=1 CCnav",
			ExpectedWalkResult{commandName: "Cnav", remainingTokens: []string{"--flag=1", "CCnav"}, builtin: "", helpMode: true, err: false}},
		{"include flags in action", "", "Daction --flag=1 CCnav",
			ExpectedWalkResult{commandName: "Daction", remainingTokens: []string{"--flag=1", "CCnav"}, builtin: "", helpMode: true, err: false}},
		{"stop on builtin prior to flags", "", "ls -f",
			ExpectedWalkResult{commandName: "root", remainingTokens: []string{"-f"}, builtin: "ls", helpMode: true, err: false}},
		{"stop on flag prior to builtin", "", "-p 1 ls",
			ExpectedWalkResult{commandName: "root", remainingTokens: []string{"-p", "1", "ls"}, builtin: "", helpMode: true, err: false}},
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

			actual, err := Walk(startingDir, tt.input, builtins)
			testWalkResult(t, actual, err, tt.want)
		})
	}

	// check edge cases
	t.Run("nil pwd", func(t *testing.T) {
		actual, err := Walk(nil, "", builtins)
		testWalkResult(t, actual, err, ExpectedWalkResult{"", nil, "", false, true})
	})
	t.Run("nil pwd with excess tokens", func(t *testing.T) {
		actual, err := Walk(nil, "excess tokens", builtins)
		testWalkResult(t, actual, err, ExpectedWalkResult{"", nil, "", false, true})
	})

}

// helper for TestWalk
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

// helper for TestWalk
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

// helper for TestWalk
func testWalkResult(t *testing.T, actual WalkResult, actualErr error, want ExpectedWalkResult) {
	// check errors first
	if (want.err && actualErr == nil) || (!want.err && actualErr != nil) {
		t.Errorf("mismatch error state.\nwant err? %v | actual err: %v", want.err, actualErr)
	}
	if actual.EndCmd != nil && (actual.EndCmd.Name() != want.commandName) {
		t.Error(ExpectedActual(want.commandName, actual.EndCmd.Name()))
	}
	if slices.Compare(actual.RemainingTokens, want.remainingTokens) != 0 {
		t.Error("bad remaining tokens" + ExpectedActual(want.remainingTokens, actual.RemainingTokens))
	}
	if actual.Builtin != want.builtin {
		t.Error("bad built-in." + ExpectedActual(want.builtin, actual.Builtin))
	}
}
