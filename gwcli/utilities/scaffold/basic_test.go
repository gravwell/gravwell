package scaffold_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldBasicErrorHandling(t *testing.T) {
	newBA := func() action.Pair {
		return scaffold.NewBasicAction("test", "test", "test",
			func(fs *pflag.FlagSet) (_ []scaffold.Result, addtlCmds tea.Cmd) {
				fail, err := fs.GetBool("fail")
				require.NoError(t, err)
				if fail {
					return []scaffold.Result{{Output: "failed"}}, nil
				}
				return []scaffold.Result{{Output: "succeeded", Success: true}}, nil
			},
			scaffold.BasicOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := pflag.FlagSet{}
					fs.Bool("fail", false, "")
					return &fs
				},
				ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
					if fs.NArg() != 1 {
						return "you must specify exactly 1 argument", nil
					} else if fs.Arg(0) == "I want to error in ValidateArgs" {
						return "", errors.New("you've successfully errored in ValidateArgs")
					}
					return "", nil
				},
			},
		)
	}

	t.Run("non-interactive", func(t *testing.T) {
		t.Run("fail from ValidateArgs: invalid", func(t *testing.T) {
			var sbErr strings.Builder
			ba := newBA()
			ba.Action.SetErr(&sbErr)
			ba.Action.SetArgs([]string{}) // missing a bare argument
			assert.ErrorContains(t, ba.Action.Execute(), "exactly 1 argument")
			assert.Contains(t, sbErr.String(), "exactly 1 argument")
		})
		t.Run("fail from ValidateArgs: error", func(t *testing.T) {
			var sbErr strings.Builder
			ba := newBA()
			ba.Action.SetErr(&sbErr)
			ba.Action.SetArgs([]string{"I want to error in ValidateArgs"}) // special bare argument
			assert.ErrorContains(t, ba.Action.Execute(), "you've successfully errored in ValidateArgs")
			assert.Contains(t, sbErr.String(), "you've successfully errored in ValidateArgs")
		})
		t.Run("fail from act", func(t *testing.T) {
			var sbErr strings.Builder
			ba := newBA()
			ba.Action.SetErr(&sbErr)
			ba.Action.SetArgs([]string{"--fail", "arg"})
			assert.ErrorContains(t, ba.Action.Execute(), "failed")
			assert.Contains(t, sbErr.String(), "failed")
		})
		t.Run("success", func(t *testing.T) {
			var sbOut, sbErr strings.Builder
			ba := newBA()
			ba.Action.SetOut(&sbOut)
			ba.Action.SetErr(&sbErr)
			ba.Action.SetArgs([]string{"arg"})
			assert.NoError(t, ba.Action.Execute())
			assert.Equal(t, "succeeded", strings.TrimSpace(sbOut.String()))
			assert.Empty(t, sbErr.String())
		})
	})
	t.Run("interactive", func(t *testing.T) {
		// unlike the non-interactive tests,
		// running these back to back on the same model is beneficial
		ba := newBA()
		t.Run("fail", func(t *testing.T) {
			inv, _, err := ba.Model.SetArgs(nil, []string{"--fail", "bare"}, 80, 60)
			assert.NoError(t, err)
			assert.Empty(t, inv)
			output := ba.Model.Update(nil) // message is irrelevant
			require.Equal(t, "failed", testsupport.ExtractPrintLineMessageString(t, output, true, 0))
			ba.Model.View() // irrelevant
			assert.True(t, ba.Model.Done())
		})
		ba.Model.Reset()
		t.Run("success", func(t *testing.T) {
			inv, _, err := ba.Model.SetArgs(nil, []string{"bare"}, 80, 60)
			assert.NoError(t, err)
			assert.Empty(t, inv)
			output := ba.Model.Update(nil) // message is irrelevant
			require.Equal(t, "succeeded", testsupport.ExtractPrintLineMessageString(t, output, true, 0))
			ba.Model.View() // irrelevant
			assert.True(t, ba.Model.Done())
		})
	})
}

// we expect the scaffold to panic if critical information (use/act) are nil
func TestScaffoldBasicArgumentValidation(t *testing.T) {
	tests := []struct {
		use string
		act scaffold.ActFunc
	}{
		{"",
			func(fs *pflag.FlagSet) (_ []scaffold.Result, addtlCmds tea.Cmd) { return nil, nil },
		},
		{"mytestuse", nil},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q/%p", tt.use, tt.act), func(t *testing.T) {

		})
		var recovered bool
		defer func() {
			// this is the final deferred function; if we have not recovered by this point, we goofed
			if !recovered {
				t.Error("test did not recover from expected panic (either it panicked and failed to recover or it did not panic)")
			}
		}()
		defer func() {
			recover()
			recovered = true
		}()
		// call function expected to panic
		scaffold.NewBasicAction(tt.use, "", "", tt.act, scaffold.BasicOptions{})
	}
}

func TestScaffoldBasicOptions(t *testing.T) {
	example, aliases := "example", []string{"alias1", "alias2"}
	// helper function to generate a new action pair (with returned aliases and example set in the command) with three flags:
	//
	// --testbool
	//
	// --negative-five=<> (required to equal -5)
	//
	// --five=<> (required to equal 5)
	newBAWithFlags := func() action.Pair {
		return scaffold.NewBasicAction("test", "short test", "long test",
			func(fs *pflag.FlagSet) ([]scaffold.Result, tea.Cmd) {
				testbool, err := fs.GetBool("testbool")
				if err != nil {
					return []scaffold.Result{{Output: err.Error()}}, nil
				}
				// basics typically should not return printlns, but we can use it for testing
				s := fmt.Sprintf("testbool: %v", testbool)
				return []scaffold.Result{{Output: s, Success: true}}, tea.Println(s)
			}, scaffold.BasicOptions{
				Example: example,
				Aliases: aliases,
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("testbool", false, "a boolean for testing")
					fs.Int("negative-five", 0, "must be set to -5")
					fs.Uint("five", 0, "must be set to 5")
					return fs
				},
				// define a boolean that can be set by the actFunc and a couple ints to test ValidateArgs

				ValidateArgs: func(fs *pflag.FlagSet) (invalid string, err error) {
					if nfive, err := fs.GetInt("negative-five"); err != nil {
						return "", err
					} else if nfive != -5 {
						return "--negative-five must equal -5", nil
					}
					if five, err := fs.GetUint("five"); err != nil {
						return "", err
					} else if five != 5 {
						return "--five must equal 5", nil
					}
					return "", nil
				},
			})
	}

	t.Run("non-interactive", func(t *testing.T) {
		// baseline test. No options given, scaffoldbasic should operate with no exceptions
		t.Run("baseline(no options)", func(t *testing.T) {
			expectedOutput := "Hello World"
			ba := scaffold.NewBasicAction("test", "short test", "long test",
				func(fs *pflag.FlagSet) ([]scaffold.Result, tea.Cmd) {
					// basics typically should not return printlns as cmd, but we can use it for testing
					return []scaffold.Result{{Output: expectedOutput, Success: true}}, tea.Println(expectedOutput)
				}, scaffold.BasicOptions{})
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)

			ba.Action.SetOut(&sbOut)
			ba.Action.SetErr(&sbErr)
			require.NoError(t, ba.Action.Execute())
			assert.Empty(t, strings.TrimSpace(sbErr.String()))
			assert.Equal(t, expectedOutput, strings.TrimSpace(sbOut.String()))
		})
		// validate that all options set in the helper function are actually set and,
		// when all flags are set, flags pass through as expected
		t.Run("all flags passed", func(t *testing.T) {
			pair := newBAWithFlags()

			// validate create-time data is set properly
			assert.Equal(t, example, pair.Action.Example)
			assert.Equal(t, aliases, pair.Action.Aliases)

			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)
			pair.Action.SetArgs([]string{"--testbool", "--negative-five=-5", "--five", "5"})
			require.NoError(t, pair.Action.Execute())
			assert.Empty(t, strings.TrimSpace(sbErr.String()))
			assert.Equal(t, "testbool: true", strings.TrimSpace(sbOut.String()))
		})
		t.Run("--negative-five unset", func(t *testing.T) {
			pair := newBAWithFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)
			pair.Action.SetArgs([]string{"--testbool", "--five", "5"})
			assert.Error(t, pair.Action.Execute())
			assert.NotEmpty(t, strings.TrimSpace(sbErr.String()), "the error should have printed to stderr, too")
			assert.Empty(t, strings.TrimSpace(sbOut.String()), "stdout shouldn't have any data")
		})
		t.Run("--five not set", func(t *testing.T) {
			pair := newBAWithFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)
			pair.Action.SetArgs([]string{"--testbool", "--negative-five=-5"})
			assert.Error(t, pair.Action.Execute())
			assert.NotEmpty(t, strings.TrimSpace(sbErr.String()), "the error should have printed to stderr, too")
			assert.Empty(t, strings.TrimSpace(sbOut.String()), "stdout shouldn't have any data")
		})
		t.Run("--five given a negative", func(t *testing.T) {
			pair := newBAWithFlags()

			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)
			pair.Action.SetArgs([]string{"--testbool", "--five=-5"})
			assert.Error(t, pair.Action.Execute())
			assert.NotEmpty(t, strings.TrimSpace(sbErr.String()), "the error should have printed to stderr, too")
			assert.Empty(t, strings.TrimSpace(sbOut.String()), "stdout shouldn't have any data")
		})
	})
	t.Run("interactive", func(t *testing.T) {
		// run the mother cycle back to back several times to ensure data is properly unset
		t.Run("back-to-back", func(t *testing.T) {
			pair := newBAWithFlags()
			t.Log("cycle 1: all arguments given")
			msg := motherCycle(t, pair.Model, []string{"--testbool", "--negative-five=-5", "--five", "5"}, false)
			assert.Equal(t, "testbool: true", msg)
			t.Log("cycle 2: no arguments given") // missing required arguments
			msg = motherCycle(t, pair.Model, nil, true)
			assert.Empty(t, msg)
			t.Log("cycle 3: all arguments given except --testbool")
			msg = motherCycle(t, pair.Model, []string{"--negative-five=-5", "--five", "5"}, false)
			assert.Equal(t, "testbool: false", msg)
			t.Log("cycle 4: all arguments given")
			msg = motherCycle(t, pair.Model, []string{"--testbool", "--negative-five=-5", "--five", "5"}, false)
			assert.Equal(t, "testbool: true", msg)
			t.Log("cycle 4: missing required arg --negative-five")
			msg = motherCycle(t, pair.Model, []string{"--five", "5"}, true)
			assert.Empty(t, msg)
		})
	})

}

// expects the model to be created via newBAWithFlags.
// If wantSetArgsInvalid, returns before invoking the full cycle
//
// Returns the string pulled from the cmd returned by model.Update
// Returns the empty string if cmd was nil or we didn't make it that far (ex: because of wantSetArgsInvalid)
func motherCycle(t *testing.T, model action.Model, args []string, wantSetArgsInvalid bool) (updateCMDMsg string) {
	defer assert.NoError(t, model.Reset()) // we always want to reset

	{ // install arguments
		inv, cmd, err := model.SetArgs(nil, args, 80, 50)
		require.NoError(t, err)
		require.Equal(t, wantSetArgsInvalid, inv != "", inv)
		if wantSetArgsInvalid {
			return
		}
		// expected okay
		assert.Nil(t, cmd, "SetArgs should not return commands")
	}
	assert.False(t, model.Done(), "should not be done before a cycle has been run")
	updateCMD := model.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	if updateCMD != nil {
		updateCMDMsg = testsupport.ExtractPrintLineMessageString(t, updateCMD, true, 0)
	}
	// crack open the message to check for a println
	assert.Empty(t, model.View(), "basic actions should never return view data")
	assert.True(t, model.Done())
	return
}
