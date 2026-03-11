/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffold

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestFromString(t *testing.T) {
	tfs(t, "uuid", "1e16d1e9-4545-495d-9995-5d58ef4dcb68", uuid.MustParse("1e16d1e9-4545-495d-9995-5d58ef4dcb68"), false)
	tfs(t, "uint", "18446744073709551615", uint(math.MaxUint), false)
	tfs(t, "uint8", "255", uint8(math.MaxUint8), false)
	tfs(t, "uint16", "65535", uint16(math.MaxUint16), false)
	tfs(t, "uint32", "60", uint32(60), false)
	tfs(t, "uint64", "60", uint64(60), false)
	tfs(t, "int", "9223372036854775807", math.MaxInt, false)
	tfs(t, "-int", "-9223372036854775808", int(math.MinInt), false)
	tfs(t, "int8", "127", int8(math.MaxInt8), false)
	tfs(t, "int16", "3", int16(3), false)
	tfs(t, "int32", "60", int32(60), false)
	tfs(t, "int64", "60", int64(60), false)

	tfs(t, "uuid invalid", "1e16d1e9-4545-495d-9995", uuid.UUID{}, true)
	tfs(t, "int16 out of range", "65535", int16(math.MaxInt16), true)
	tfs(t, "bad character", "60s", 0, true)
	tfs(t, "empty", "", 0, true)
}

// helper for TestFromString to execute FromString and check the outcome.
func tfs[I Id_t](t *testing.T, name string, strVal string, expected I, wantErr bool) {
	t.Run(name, func(t *testing.T) {
		out, err := FromString[I](strVal)
		if (err != nil) != wantErr {
			t.Fatal(err)
		} else if out != expected {
			t.Fatal(ExpectedActual(expected, out))
		}
	})
}

func TestNonInteractive(t *testing.T) {
	t.Run("sanity check arguments", func(t *testing.T) {
		// helper function
		fn := func(use, short, long string, act ActFunc) {
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
			NewBasicAction(use, short, long, act, BasicOptions{})
		}

		t.Run("use", func(t *testing.T) {
			fn("", "short", "long", func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				return "", nil
			})
		})
		t.Run("short", func(t *testing.T) {
			fn("use", "", "long", func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				return "", nil
			})
		})
		t.Run("long", func(t *testing.T) {
			fn("", "short", "", func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				return "", nil
			})
		})
		t.Run("act", func(t *testing.T) {
			fn("use", "short", "", nil)
		})

	})
	t.Run("no options", func(t *testing.T) {
		expectedOutput := "Hello World"
		ba := NewBasicAction("test", "short test", "long test",
			func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				return expectedOutput, tea.Println(expectedOutput) // basics typically should not return printlns, but we can use it for testing
			}, BasicOptions{})
		var (
			sbOut strings.Builder
			sbErr strings.Builder
		)

		ba.Action.SetOut(&sbOut)
		ba.Action.SetErr(&sbErr)

		if err := ba.Action.Execute(); err != nil {
			t.Fatal(err)
		}
		// check outputs
		if strErr := strings.TrimSpace(sbErr.String()); strErr != "" {
			t.Fatal(strErr)
		}
		if strOut := strings.TrimSpace(sbOut.String()); strOut != expectedOutput {
			t.Fatal(ExpectedActual(expectedOutput, strOut))
		}
	})
	t.Run("options set", func(t *testing.T) {
		t.Run("all as expected", func(t *testing.T) {
			pair, _, _ := newPairWithRequiredFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)

			pair.Action.SetArgs([]string{"--testbool", "--negative-five=-5", "--five", "5"})
			if err := pair.Action.Execute(); err != nil {
				t.Fatal(err)
			}
			// check outputs
			if strErr := strings.TrimSpace(sbErr.String()); strErr != "" {
				t.Fatal(strErr)
			}
			if strOut := strings.TrimSpace(sbOut.String()); strOut != "testbool: true" {
				t.Fatal(ExpectedActual("testbool: true", strOut))
			}
		})
		t.Run("--negative-five unset", func(t *testing.T) {
			pair, _, _ := newPairWithRequiredFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)

			pair.Action.SetArgs([]string{"--testbool", "--five", "5"})
			if err := pair.Action.Execute(); err != nil {
				t.Fatal(err)
			}
			// check outputs
			if strErr := strings.TrimSpace(sbErr.String()); strErr == "" {
				t.Fatal("expected failure due to missing required parameter --negative-five=-5")
			}
			if strOut := strings.TrimSpace(sbOut.String()); strOut != "" {
				t.Fatal("expected stdout to failure due to validate error")
			}
		})
		t.Run("--five unset", func(t *testing.T) {
			pair, _, _ := newPairWithRequiredFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)

			pair.Action.SetArgs([]string{"--testbool", "--negative-five=-5"})
			if err := pair.Action.Execute(); err != nil {
				t.Fatal(err)
			}
			// check outputs
			if strErr := strings.TrimSpace(sbErr.String()); strErr == "" {
				t.Fatal("expected failure due to missing required parameter --negative-five=-5")
			}
			if strOut := strings.TrimSpace(sbOut.String()); strOut != "" {
				t.Fatal("expected stdout to failure due to validate error")
			}
		})
		t.Run("--five given a negative", func(t *testing.T) {
			pair, _, _ := newPairWithRequiredFlags()
			var (
				sbOut strings.Builder
				sbErr strings.Builder
			)
			pair.Action.SetOut(&sbOut)
			pair.Action.SetErr(&sbErr)

			pair.Action.SetArgs([]string{"--testbool", "--five=-5"})
			if err := pair.Action.Execute(); err == nil {
				t.Fatal("expected an error after giving a negative number to uint")
			}
		})

	})
}

func TestModel(t *testing.T) {
	t.Run("normal run, twice", func(t *testing.T) {
		pair := NewBasicAction("test", "short test", "long test",
			func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				testbool, err := fs.GetBool("testbool")
				if err != nil {
					panic(err)
				}
				s := fmt.Sprintf("testbool: %v", testbool)
				return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
			}, BasicOptions{AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			}, CmdMods: func(c *cobra.Command) { c.Example = "an example of " + c.Use + " command" }})

		// initial check options
		if pair.Action.Example != "an example of test command" {
			t.Fatal(ExpectedActual("an example of test command", pair.Action.Example))
		}

		var (
			sbOut strings.Builder
			sbErr strings.Builder
		)
		pair.Action.SetOut(&sbOut)
		pair.Action.SetErr(&sbErr)

		ba, ok := pair.Model.(*basicAction)
		if !ok {
			t.Fatal("failed to type assert model to *basicAction")
		}

		// run it twice
		fauxMother(t, ba, []string{}, false, "testbool: false")
		fauxMother(t, ba, []string{"--testbool"}, false, "testbool: true")

		// check outputs
		if strErr := strings.TrimSpace(sbErr.String()); strErr != "" {
			t.Fatal(strErr)
		}
		if strOut := strings.TrimSpace(sbOut.String()); strOut != "" {
			t.Fatal(strOut)
		}
	})
	t.Run("run with options, twice", func(t *testing.T) {
		pair := NewBasicAction("test", "short test", "long test",
			func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
				testbool, err := fs.GetBool("testbool")
				if err != nil {
					panic(err)
				}
				return fmt.Sprintf("testbool: %v", testbool), nil
			}, BasicOptions{AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			},
				CmdMods: func(c *cobra.Command) {
					c.Args = cobra.ExactArgs(2)
				}})
		var (
			sbOut strings.Builder
			sbErr strings.Builder
		)
		pair.Action.SetOut(&sbOut)
		pair.Action.SetErr(&sbErr)

		ba, ok := pair.Model.(*basicAction)
		if !ok {
			t.Fatal("failed to type assert model to *basicAction")
		}

		fauxMother(t, ba, []string{"1", "2"}, false, "testbool: false")
		// check that outputs are empty
		if e := strings.TrimSpace(sbErr.String()); e != "" {
			t.Fatal(ExpectedActual("", e))
		}
		if o := strings.TrimSpace(sbOut.String()); o != "" {
			t.Fatal(ExpectedActual("", o))
		}

		sbOut.Reset()
		sbErr.Reset()

		fauxMother(t, ba, []string{}, true, "")
		t.Log("stdout (second run): ", sbOut.String())
		t.Log("stderr (second run): ", sbErr.String())
		// check that outputs are empty
		if e := strings.TrimSpace(sbErr.String()); e != "" {
			t.Fatal(ExpectedActual("", e))
		}
		if o := strings.TrimSpace(sbOut.String()); o != "" {
			t.Fatal(ExpectedActual("", o))
		}
	})
	t.Run("required flags", func(t *testing.T) {
		pair, _, _ := newPairWithRequiredFlags()
		var (
			sbOut strings.Builder
			sbErr strings.Builder
		)
		pair.Action.SetOut(&sbOut)
		pair.Action.SetErr(&sbErr)

		ba, ok := pair.Model.(*basicAction)
		if !ok {
			t.Fatal("failed to type assert model to *basicAction")
		}
		// supplying a bare argument, but not one of the required flags
		fauxMother(t, ba, []string{"1"}, true, "")
		// supplying a bare argument and one of the required flags (but not both)
		fauxMother(t, ba, []string{"--negative-five", "-5", "1"}, true, "")
		// supplying a bare argument and both required flags
		fauxMother(t, ba, []string{"--negative-five", "-5", "--five=5", "1"}, false, "testbool: false")
	})
}

// fauxMother mimics the call tree of Mother (.SetArgs -> .Update -> .View() -> .Done() -> .Reset()) against ba.
//
// setArgsInvalid checks that .SetArgs() returned invalid. If setArgsInvalid is set and matched, fauxMother will return early.
// expectedUpdatePrintMsg tests the printLineMessage returned by .Update() (in sequence form or bare form).
func fauxMother(t *testing.T, ba *basicAction, args []string, setArgsInvalid bool, expectedUpdatePrintMsg string) {
	t.Helper()
	{
		inv, cmd, err := ba.SetArgs(nil, args, 80, 50)
		if err != nil {
			t.Fatal(err)
		}

		if inv != "" && setArgsInvalid { // expected invalid
			return
		} else if inv == "" && setArgsInvalid { // unexpected okay
			t.Fatal("expected .SetArgs to return invalid")
		} else if inv != "" && !setArgsInvalid { // unexpected invalid
			t.Fatal(inv)
		}
		// expected okay

		if cmd != nil {
			t.Fatal("SetArgs should not return commands")
		}
	}
	if ba.Done() {
		t.Fatal("Should not be done before a cycle has been run")
	}
	if cmd := ba.Update(tea.WindowSizeMsg{Width: 80, Height: 50}); cmd == nil {
		t.Fatal("Update should always return at least 1 cmd")
	} else { // crack open the message to check for a println (possibly nested in a tea.Sequence)
		plm := extractPrintLineMessageString(t, cmd)
		if expectedUpdatePrintMsg != plm {
			t.Fatal(ExpectedActual(expectedUpdatePrintMsg, plm))
		}
	}
	if view := ba.View(); view != "" {
		t.Fatal("View should never return data")
	}
	if !ba.Done() {
		t.Fatal("Should be done after a single cycle")
	}
	if err := ba.Reset(); err != nil {
		t.Fatal(err)
	}
}

func extractPrintLineMessageString(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	voItm1 := reflect.ValueOf(cmd())
	t.Logf("Update msg kind: %v", voItm1.Kind())
	// this will be a slice if it is a sequence or a struct if a single msg
	var voPLM reflect.Value
	if voItm1.Kind() == reflect.Slice {
		if voItm1.Len() < 1 {
			t.Fatal(ExpectedActual(2, voItm1.Len()))
		} else if voItm1.Len() > 2 {
			t.Log(ExpectedActual(2, voItm1.Len()))
		}
		// replace voMsg with the first item
		voItm1 = voItm1.Index(0)
		// voItm1 should now be a Cmd that returns a printLineMessage
		if voItm1.Kind() != reflect.Func {
			t.Fatal(ExpectedActual(reflect.Func, voItm1.Kind()))
		}

		if res := voItm1.Call(nil); len(res) != 1 {
			t.Fatal("bad output  count", ExpectedActual(1, len(res)))
		} else {
			voPLM = res[0]
		}
	} else { // not a sequence, just a raw printLineMessage (or an interface of a  Msg)
		voPLM = voItm1
	}

	// if the Message is still in interface form, we need to dereference it
	if voPLM.Kind() == reflect.Interface {
		voPLM = voPLM.Elem()
	}
	if voPLM.Kind() != reflect.Struct {
		t.Fatal(ExpectedActual(reflect.Struct, voPLM.Kind()))
	}

	voMessageBody := voPLM.FieldByName("messageBody")
	if voMessageBody.Kind() != reflect.String {
		t.Fatal(ExpectedActual(reflect.String, voMessageBody.Kind()))
	}
	return voMessageBody.String()
}

// helper function to generate a new action pair (with returned aliases and example set in the command) with three flags:
//
// --testbool
//
// --negative-five=<> (required to equal -5)
//
// --five=<> (required to equal 5)
func newPairWithRequiredFlags() (pair action.Pair, aliases []string, example string) {
	aliases, example = []string{"alias1", "alias2"}, "example"
	return NewBasicAction("test", "short test", "long test",
		func(cmd *cobra.Command, fs *pflag.FlagSet) (string, tea.Cmd) {
			// validate that the command has the expected values
			if slices.Compare(cmd.Aliases, aliases) != 0 {
				panic(ExpectedActual(aliases, cmd.Aliases))
			} else if cmd.Example != example {
				panic(ExpectedActual(example, cmd.Example))
			}
			testbool, err := fs.GetBool("testbool")
			if err != nil {
				panic(err)
			}
			s := fmt.Sprintf("testbool: %v", testbool)
			return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
		}, BasicOptions{
			// define a boolean that can be set by the actFunc and a couple ints to test ValidateArgs
			AddtlFlagFunc: func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				fs.Int("negative-five", 0, "must be set to -5")
				fs.Uint("five", 0, "must be set to 5")
				return fs
			},
			Aliases: aliases,
			CmdMods: func(c *cobra.Command) {
				c.Example = example
			},
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
		}), aliases, example
}
