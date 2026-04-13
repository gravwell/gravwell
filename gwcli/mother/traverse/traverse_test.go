/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package traverse_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/group"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/mother/traverse"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestDeriveSuggestions(t *testing.T) {
	dummyActionFunc := func(_ *pflag.FlagSet) (string, tea.Cmd) { return "", nil } // actually functionality is irrelevant
	/*
		generate a command tree to test against:
		root/
		├── nav_a/ (aliases: "nav_a_alias","AAlias")
		│   └── action_a_1
		├── action1
		└── nav_b/
		    └── nav_ba/
		        ├── action_ba_1
		        └── action_ba_2 (aliases: "aBA2")
	*/
	nav_a := treeutils.GenerateNav("nav_a", "nav_a short", "nav_a long", []string{"nav_a_alias", "AAlias"},
		nil, // subnavs
		[]action.Pair{scaffold.NewBasicAction("action_a_1", "action_a_1 short", "action_a_1 long", dummyActionFunc, scaffold.BasicOptions{})}, // sub-actions
	)
	action1 := scaffold.NewBasicAction("action1", "action1 short", "action1 long", dummyActionFunc, scaffold.BasicOptions{})
	nav_ba := treeutils.GenerateNav("nav_ba", "nav_ba short", "nav_ba long", nil,
		nil, // subnavs
		[]action.Pair{
			scaffold.NewBasicAction("action_ba_1", "action_ba_1 short", "action_ba_1 long", dummyActionFunc, scaffold.BasicOptions{}),
			scaffold.NewBasicAction("action_ba_2", "action_ba_2 short", "action_ba_2 long", dummyActionFunc, scaffold.BasicOptions{CommonOptions: scaffold.CommonOptions{Aliases: []string{"aBA2"}}}),
		}, // sub-actions
	)
	nav_b := treeutils.GenerateNav("nav_b", "nav_b short", "nav_b long", nil,
		[]*cobra.Command{nav_ba}, // subnavs
		nil,                      // sub-actions
	)
	root := treeutils.GenerateNav("root", "root short", "root long", nil,
		[]*cobra.Command{nav_a, nav_b},
		[]action.Pair{action1})

	tests := []struct {
		name                  string // NOTE: curInput is prefixed to test name on run
		curInput              string
		startingWD            *cobra.Command
		builtins              []string
		expectedNavs          []traverse.Suggestion
		expectedActions       []traverse.Suggestion
		expectedBISuggestions []traverse.Suggestion
	}{
		{"nil working directory",
			"nav", nil, []string{"a", "b", "c"},
			nil,
			nil,
			nil,
		},
		{"empty input should suggest all immediate navs, actions and all builtins.",
			"", root, []string{"bi1", "bi2", "help"},
			[]traverse.Suggestion{
				{FullName: "nav_a"},
				{FullName: "nav_b"},
			},
			[]traverse.Suggestion{
				{FullName: "action1"},
			},
			[]traverse.Suggestion{
				{FullName: "bi1"},
				{FullName: "bi2"},
				{FullName: "help"},
				{FullName: traverse.RootToken},
				{FullName: traverse.RootTokenSecondary},
				{FullName: traverse.UpToken},
			},
		},
		{"whitespace-only input should suggest all immediate navs, actions and all builtins.",
			"       	  ", nav_ba, []string{"bi1", "bi2", "help"},
			nil,
			[]traverse.Suggestion{
				{FullName: "action_ba_1"},
				{FullName: "action_ba_2"},
			},
			[]traverse.Suggestion{
				{FullName: "bi1"},
				{FullName: "bi2"},
				{FullName: "help"},
				{FullName: traverse.RootToken},
				{FullName: traverse.RootTokenSecondary},
				{FullName: traverse.UpToken},
			},
		},
		{"against root should match both subnavs and a BI, but not the action",
			"nav", root, []string{"bi1", "bi2", "help", "n", "N", "navigator", "Navigator"},
			[]traverse.Suggestion{
				{FullName: "nav_a", MatchedCharacters: "nav"},
				{FullName: "nav_b", MatchedCharacters: "nav"},
			},
			nil,
			[]traverse.Suggestion{{FullName: "navigator", MatchedCharacters: "nav"}},
		},
		{"against nav_b should match only nav_ba and a BI",
			"nav", nav_b, []string{"bi1", "bi2", "help", "n", "N", "navigator", "Navigator"},
			[]traverse.Suggestion{
				{FullName: "nav_ba", MatchedCharacters: "nav"},
			},
			nil,
			[]traverse.Suggestion{{FullName: "navigator", MatchedCharacters: "nav"}},
		},
		{"against root should traverse to nav_b and match only nav_ba and a BI",
			"nav_b nav", root, []string{"bi1", "bi2", "help", "n", "N", "navigator", "Navigator"},
			[]traverse.Suggestion{
				{FullName: "nav_ba", MatchedCharacters: "nav"},
			},
			nil,
			[]traverse.Suggestion{{FullName: "navigator", MatchedCharacters: "nav"}},
		},
		{"(trailing space) should traverse and then suggest all at new pwd",
			"nav_a ", root, []string{"a", "b"},
			nil,
			[]traverse.Suggestion{
				{FullName: "action_a_1"},
			},
			[]traverse.Suggestion{
				{FullName: "a"},
				{FullName: "b"},
				{FullName: traverse.RootToken},
				{FullName: traverse.RootTokenSecondary},
				{FullName: traverse.UpToken},
			},
		},
		{"alias match, but no trailing space so no traversal and thus no suggestions",
			"AAlias", root, []string{"a", "b"},
			nil,
			nil,
			nil,
		},
		{"(trailing space) traverse nav_a via alias",
			"AAlias ", root, []string{"a", "b"},
			nil,
			[]traverse.Suggestion{
				{FullName: "action_a_1"},
			},
			[]traverse.Suggestion{
				{FullName: "a"},
				{FullName: "b"},
				{FullName: traverse.RootToken},
				{FullName: traverse.RootTokenSecondary},
				{FullName: traverse.UpToken},
			},
		},
		{"no matching suggests, no traversal",
			"z", root, []string{"a", "b"},
			nil,
			nil,
			nil,
		},
		{"traversal, then no matching suggests",
			"nav_b z", root, []string{"a", "b"},
			nil,
			nil,
			nil,
		},
		{"bad traversal, should return nothing",
			"nav_DNE z", root, []string{"a", "b"},
			nil,
			nil,
			nil,
		},
		{"special traversal character exact match",
			"..", nav_a, []string{"a", "b"},
			nil,
			nil,
			[]traverse.Suggestion{
				{FullName: traverse.UpToken, MatchedCharacters: ".."},
			},
		},
		{"special traversal character partial match",
			".", nav_a, []string{"a", "b"},
			nil,
			nil,
			[]traverse.Suggestion{
				{FullName: traverse.UpToken, MatchedCharacters: "."},
			},
		},
		{"traverse with special characters to partial match back at root",
			"nav_a / nav_b .. actio", root, []string{"bi1", "bi2", "help"},
			nil,
			[]traverse.Suggestion{
				{FullName: "action1", MatchedCharacters: "actio"},
			},
			nil,
		},
		{"halt traversal on first action match (action1)",
			"nav_a / nav_b .. action1 ~ nav_a", root, []string{"bi1", "bi2", "help"},
			nil,
			nil,
			nil,
		},
		{"(trailing space) halt traversal on first action match (action1)",
			"nav_a / nav_b .. action1 ~ nav_a ", root, []string{"bi1", "bi2", "help"},
			nil,
			nil,
			nil,
		},
		{"halt traversal on first builtin match (bi2)",
			"nav_a / nav_b .. bi2 ~ nav_a", root, []string{"bi1", "bi2", "help"},
			nil,
			nil,
			nil,
		},
		{"(trailing space) halt traversal on first builtin match (bi2)",
			"nav_a / nav_b .. bi2 ~ nav_a", root, []string{"bi1", "bi2", "help"},
			nil,
			nil,
			nil,
		},
		{"traversal ignores \"help\" and continues suggesting at pwd",
			"help na", root, []string{"navigator", "Navigator", "noodle"},
			[]traverse.Suggestion{
				{FullName: "nav_a", MatchedCharacters: "na"},
				{FullName: "nav_b", MatchedCharacters: "na"},
			},
			nil,
			[]traverse.Suggestion{
				{FullName: "navigator", MatchedCharacters: "na"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("in:\"%s\"|%v", tt.curInput, tt.name), func(t *testing.T) {
			actualNavs, actualActions, actualBI := traverse.DeriveSuggestions(tt.curInput, tt.startingWD, tt.builtins)

			// sort each expected slice to ensure consistency
			slices.SortStableFunc(tt.expectedNavs, traverse.SuggestionsCompare)
			slices.SortStableFunc(tt.expectedActions, traverse.SuggestionsCompare)
			slices.SortStableFunc(tt.expectedBISuggestions, traverse.SuggestionsCompare)

			// compare nav suggestions
			if !slices.EqualFunc(actualNavs, tt.expectedNavs, func(a, b traverse.Suggestion) bool {
				return a.Equals(b)
			}) {
				t.Error("incorrect nav suggestions", testsupport.ExpectedActual(tt.expectedNavs, actualNavs))
			}
			// compare action suggestions
			if !slices.EqualFunc(actualActions, tt.expectedActions, func(a, b traverse.Suggestion) bool {
				return a.Equals(b)
			}) {
				t.Error("incorrect action suggestions", testsupport.ExpectedActual(tt.expectedActions, actualActions))
			}
			// compare BI suggestions
			if !slices.Equal(actualBI, tt.expectedBISuggestions) {
				t.Error("incorrect BI suggestions", testsupport.ExpectedActual(tt.expectedBISuggestions, actualBI))
			}
		})
	}
}

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

			actual, err := traverse.Walk(startingDir, tt.input, builtins)
			testWalkResult(t, actual, err, tt.want)
		})
	}

	// check edge cases
	t.Run("nil pwd", func(t *testing.T) {
		actual, err := traverse.Walk(nil, "", builtins)
		testWalkResult(t, actual, err, ExpectedWalkResult{"", nil, "", false, true})
	})
	t.Run("nil pwd with excess tokens", func(t *testing.T) {
		actual, err := traverse.Walk(nil, "excess tokens", builtins)
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
func testWalkResult(t *testing.T, actual traverse.WalkResult, actualErr error, want ExpectedWalkResult) {
	// check errors first
	if (want.err && actualErr == nil) || (!want.err && actualErr != nil) {
		t.Errorf("mismatch error state.\nwant err? %v | actual err: %v", want.err, actualErr)
	}
	if actual.EndCmd != nil && (actual.EndCmd.Name() != want.commandName) {
		t.Error(testsupport.ExpectedActual(want.commandName, actual.EndCmd.Name()))
	}
	if slices.Compare(actual.RemainingTokens, want.remainingTokens) != 0 {
		t.Error("bad remaining tokens" + testsupport.ExpectedActual(want.remainingTokens, actual.RemainingTokens))
	}
	if actual.Builtin != want.builtin {
		t.Error("bad built-in." + testsupport.ExpectedActual(want.builtin, actual.Builtin))
	}
}
