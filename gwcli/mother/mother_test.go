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
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) { return "", nil }, scaffold.BasicOptions{})
	nav1 := treeutils.GenerateNav("topNav1", "nav1 short", "nav1 long", nil, nil, []action.Pair{nav1Action1})
	nav2 := treeutils.GenerateNav("topNav2", "nav2 short", "nav2 long", nil, nil, nil)
	action1 := scaffold.NewBasicAction("topAct", "action1 short", "action1 long",
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) { return "", nil }, scaffold.BasicOptions{})
	root := treeutils.GenerateNav("root", "root short", "root long", nil,
		[]*cobra.Command{nav1, nav2}, []action.Pair{action1})

	mthr := mother.New(root, root, nil, nil)
	tm := teatest.NewTestModel(t, mthr, teatest.WithInitialTermSize(100, 80))
	t.Cleanup(func() {
		testsupport.TTSendSpecial(tm, tea.KeyCtrlC)
	})

	t.Run("completion on empty input completes to help", func(t *testing.T) {
		testsupport.TTSendSpecial(tm, tea.KeyTab)

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
		testsupport.TTSendSpecial(tm, tea.KeyTab) // autocomplete topnav1

		out := testsupport.TTMatchGolden(t, tm, false, 0)
		// should contain help exactly twice; once for the prompt, once for the suggestion bars
		if count := strings.Count(string(out), "topnav1"); count != 2 {
			t.Error("incorrect suggestion count", testsupport.ExpectedActual(2, count), "\noutput:", string(out))
		}
	})
}
