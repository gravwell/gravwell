//go:build ci

/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package mother_test

import (
	"os"
	"path"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// regenerate these golden files with:
// go test -test.fullpath=true -timeout 30s -run ^Test_SuggestionCompletion* github.com/gravwell/gravwell/v4/gwcli/mother -update
func Test_SuggestionCompletion_TeaTest(t *testing.T) {
	// initialize singletons
	logpath := path.Join(t.TempDir(), "log.txt")
	t.Log("logging to", logpath)
	clilog.InitializeFromArgs([]string{"--log=" + logpath, "--loglevel=debug"})
	t.Cleanup(func() {
		if t.Failed() {
			if b, err := os.ReadFile(logpath); err != nil {
				t.Log(err)
			} else {
				t.Log("Log Output:\n", string(b))
			}

		}
	})
	// use a plain stylesheet, but not NoColor as NoColor disables suggestions
	stylesheet.Cur = stylesheet.Plain()
	//stylesheet.NoColor = true
	// build up some example commands
	nav1Action1 := scaffold.NewBasicAction("actionone", "action1 short", "action1 long",
		func(fs *pflag.FlagSet) (string, tea.Cmd) { return "", nil }, scaffold.BasicOptions{})
	nav1 := treeutils.GenerateNav("topNav1", "nav1 short", "nav1 long", nil, []action.Pair{nav1Action1})
	nav2 := treeutils.GenerateNav("topNav2", "nav2 short", "nav2 long", nil, nil)
	action1 := scaffold.NewBasicAction("topAct", "action1 short", "action1 long",
		func(fs *pflag.FlagSet) (string, tea.Cmd) { return "", nil }, scaffold.BasicOptions{})
	root := treeutils.GenerateNav("root", "root short", "root long",
		[]*cobra.Command{nav1, nav2}, []action.Pair{action1})

	mthr := mother.New(root, root, nil, nil)
	tm := teatest.NewTestModel(t, mthr, teatest.WithInitialTermSize(100, 80))
	t.Cleanup(func() {
		testsupport.TTSendSpecial(tm, tea.KeyCtrlC)
	})

	t.Run("completion on empty input completes to help", func(t *testing.T) {
		testsupport.TTSendSpecial(tm, testsupport.SendHotkey(hotkeys.Complete).Type)

		out := testsupport.TTMatchGolden(t, tm, false, 0)
		// should contain help exactly twice; once for the prompt, once for the suggestion bars
		if count := strings.Count(string(out), "help"); count != 2 {
			t.Errorf("incorrect \"help\" count: %v", testsupport.ExpectedActual(2, count))
		}
	})
	t.Run("clear prompt on ctrl+u", func(t *testing.T) {
		testsupport.TTSendSpecial(tm, tea.KeyCtrlU)
		testsupport.TTMatchGolden(t, tm, false, 0)
	})

	// NOTE(rlandau): golden file matching is finicky.
	// The should work, but be careful while editing it or adding new golden file tests;
	// golden files tend to contain multiple outputs from Mother rather than an exact, point-in-time snapshot.
	t.Run("navs are prioritized over actions", func(t *testing.T) {
		// navs should be sorted alphanumerically, but always suggested before actions
		tm.Type("top")
		time.Sleep(100 * time.Millisecond)
		testsupport.TTSendSpecial(tm, testsupport.SendHotkey(hotkeys.Complete).Type) // autocomplete topnav1

		out := testsupport.TTMatchGolden(t, tm, false, 0)
		// should contain help exactly twice; once for the prompt, once for the suggestion bars
		if count := strings.Count(string(out), "topnav1"); count != 2 {
			t.Error("incorrect suggestion count", testsupport.ExpectedActual(2, count), "\noutput:", string(out))
		}
	})
}

// Tests that all tokens are properly rebuilt on mother's prompt after a New().
func TestAllTokensPropagate(t *testing.T) {
	clilog.InitializeFromArgs(nil)

	// root
	// - nav1
	// |- action1 (flags: f1=int f2=bool)
	// - nav2

	action1 := treeutils.GenerateAction("action1", "action one", "action yī", func(c *cobra.Command, s []string) error { return nil })
	action1.Flags().Int("f1", 0, "")
	action1.Flags().Bool("f2", false, "")
	nav1 := treeutils.GenerateNav("nav1", "nav one", "nav yī", nil, []action.Pair{action.NewPair(action1, nil)})
	nav2 := treeutils.GenerateNav("nav2", "nav two", "nav ѐr", nil, nil)
	root := treeutils.GenerateNav("root", "root", "root", []*cobra.Command{nav1, nav2}, nil)
	uniques.AttachPersistentFlags(root)

	tests := []struct {
		name       string
		cur        *cobra.Command
		args       []string
		wantPrompt string
	}{
		{"no args",
			action1, nil, "root nav1>action1"},
		{"one args",
			action1, []string{"arg1"}, "root nav1>action1 arg1"},
		{"two args",
			action1, []string{"arg1", "arg2"}, "root nav1>action1 arg1 arg2"},
		{"two args and two flags",
			action1, []string{"--f1=3", "--f2", "arg1", "arg2"}, "root nav1>action1 --f1=3 --f2 arg1 arg2"},
		{"int flag only",
			action1, []string{"--f1=3"}, "root nav1>action1 --f1=3"},
		{"bool flag only",
			action1, []string{"--f2"}, "root nav1>action1 --f2"},

		// NOTE(rlandau): we don't test unknown flags as cobra will catch and error these for us

		// NOTE(rlandau): we don't test cur = nav because navs never take additional args

	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mother.New(root, tt.cur, tt.args, nil)
			// ! Update should NOT be called, as it will cause Mother to enter handoff mode and we won't be able to view her prompt.
			v := m.View()
			// only care about the prompt itself
			prompt, _, found := strings.Cut(v, "\n")
			require.True(t, found)
			require.Equal(t, tt.wantPrompt, strings.TrimSpace(prompt))
		})
	}

}
