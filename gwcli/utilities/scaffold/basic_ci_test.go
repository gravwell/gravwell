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
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
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
		} else if out != I(expected) {
			t.Fatal(ExpectedActual(expected, out))
		}
	})
}

func TestNewBasicAction(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		ba := NewBasicAction("test", "short test", "long test", []string{"some", "aliases"},
			func(c *cobra.Command) (string, tea.Cmd) {
				testbool, err := c.Flags().GetBool("testbool")
				if err != nil {
					panic(err)
				}
				s := fmt.Sprintf("testbool: %v", testbool)
				return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
			}, func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			})
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
		if strOut := strings.TrimSpace(sbOut.String()); strOut != "testbool: false" {
			t.Fatal(ExpectedActual("testbool: false", strOut))
		}
	})
	t.Run("set flag", func(t *testing.T) {
		ba := NewBasicAction("test", "short test", "long test", []string{"some", "aliases"},
			func(c *cobra.Command) (string, tea.Cmd) {
				testbool, err := c.Flags().GetBool("testbool")
				if err != nil {
					panic(err)
				}
				s := fmt.Sprintf("testbool: %v", testbool)
				return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
			}, func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			})
		var (
			sbOut strings.Builder
			sbErr strings.Builder
		)
		ba.Action.SetOut(&sbOut)
		ba.Action.SetErr(&sbErr)

		ba.Action.SetArgs([]string{"--testbool"})
		if err := ba.Action.Execute(); err != nil {
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
}

func TestModel(t *testing.T) {
	t.Run("normal run, twice", func(t *testing.T) {
		pair := NewBasicAction("test", "short test", "long test", []string{"some", "aliases"},
			func(c *cobra.Command) (string, tea.Cmd) {
				testbool, err := c.Flags().GetBool("testbool")
				if err != nil {
					panic(err)
				}
				s := fmt.Sprintf("testbool: %v", testbool)
				return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
			}, func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			})
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
		fauxMother(t, ba, []string{})
		fauxMother(t, ba, []string{})

		// check outputs
		if strErr := strings.TrimSpace(sbErr.String()); strErr != "" {
			t.Fatal(strErr)
		}
		if strOut := strings.TrimSpace(sbOut.String()); strOut != "" {
			t.Fatal(strOut)
		}
	})
	t.Run("run with options, twice", func(t *testing.T) {
		pair := NewBasicAction("test", "short test", "long test", []string{"some", "aliases"},
			func(c *cobra.Command) (string, tea.Cmd) {
				testbool, err := c.Flags().GetBool("testbool")
				if err != nil {
					panic(err)
				}
				s := fmt.Sprintf("testbool: %v", testbool)
				return s, tea.Println(s) // basics typically should not return printlns, but we can use it for testing
			}, func() pflag.FlagSet {
				fs := pflag.FlagSet{}
				fs.Bool("testbool", false, "a boolean for testing")
				return fs
			}, WithPositionalArguments(cobra.ExactArgs(2)))
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
		fauxMother(t, ba, []string{"1", "2"})
		if inv, cmd, err := ba.SetArgs(nil, []string{}); err != nil {
			t.Fatal(err)
		} else if inv == "" {
			t.Fatal("no arguments were given, 2 expected; ") // TODO SetArgs with no args should fail due to positional args
		} else if cmd != nil {
			t.Fatal("SetArgs should not return commands")
		}
		if ba.Done() {
			t.Fatal("Should not be done before a cycle has been run")
		}
		if cmd := ba.Update(tea.WindowSizeMsg{Width: 80, Height: 50}); cmd == nil {
			t.Fatal("Update should always return at least 1 cmd")
		} else {
			// TODO crack open the tea.Sequence and check for at least a println
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

		// check outputs
		if strErr := strings.TrimSpace(sbErr.String()); strErr != "" {
			t.Fatal(strErr)
		}
		if strOut := strings.TrimSpace(sbOut.String()); strOut != "" {
			t.Fatal(strOut)
		}
	})
}

func fauxMother(t *testing.T, ba *basicAction, args []string) {
	if inv, cmd, err := ba.SetArgs(nil, args); err != nil {
		t.Fatal(err)
	} else if inv != "" {
		t.Fatal(inv)
	} else if cmd != nil {
		t.Fatal("SetArgs should not return commands")
	}
	if ba.Done() {
		t.Fatal("Should not be done before a cycle has been run")
	}
	if cmd := ba.Update(tea.WindowSizeMsg{Width: 80, Height: 50}); cmd == nil {
		t.Fatal("Update should always return at least 1 cmd")
	} else {
		// TODO crack open the tea.Sequence and check for at least a println
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
