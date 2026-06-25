//go:build ci

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate_test

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestCleanPathSuggestions(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		availSgts []string
		input     string
		want      []string
	}{
		{"input is directory",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "dir1/",
			[]string{"file1", "file2", "abc"}},
		{"no input",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "",
			[]string{"file1", "file2", "abc"}},
		{"input has no matches",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "unmatching",
			[]string{}},
		{"input is partial file match",
			[]string{"dir1/file1", "dir1/file2", "dir1/abc"}, "dir1/",
			[]string{"file1", "file2", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaffoldcreate.TrimSuggestsToFile(tt.availSgts, tt.input)
			if !slices.Equal(tt.want, got) {
				t.Error(testsupport.ExpectedActual(tt.want, got))
			}
		})
	}
}

// TestOptions creates a new create action with all options invoked.
// It also run basic tests to check that fields were applied and set and the create func is called.
func TestOptions(t *testing.T) {
	var (
		setName     string
		setPath     string
		setCust     int
		setTestbool bool
	)

	aliases := []string{"alt1", "alt2"}
	act := scaffoldcreate.NewCreateAction("test",
		map[string]scaffoldcreate.Field{
			"name": scaffoldcreate.FieldName("test"),
			"path": scaffoldcreate.FieldPath("test", true),
			"cust": { // converted into an int
				Required: false,
				Title:    "customs",
				Flag: scaffoldcreate.FlagConfig{
					Name:      "custom",
					Usage:     "customs usage",
					Shorthand: 'c',
				},
				Order:    1,
				Provider: &scaffoldcreate.TextProvider{},
			},
		},
		func(cfg map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
			setName = cfg["name"].Provider.Get()
			setPath = cfg["path"].Provider.Get()
			i, _ := strconv.ParseInt(cfg["cust"].Provider.Get(), 10, 64)
			setCust = int(i)
			setTestbool, err = fs.GetBool("testbool")
			return 1, "", err
		},
		scaffoldcreate.Options{
			CommonOptions: scaffold.CommonOptions{
				Use:     "alt",
				Short:   "my short description",
				Long:    "my long description",
				Aliases: aliases,
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.BoolP("testbool", "k", false, "")
					return fs
				},
			},
		},
	)
	if act.Action.Use != "alt" {
		t.Error("use option was not applied", testsupport.ExpectedActual("alt", act.Action.Use))
	}
	if act.Action.Short != "my short description" {
		t.Error("short option was not applied", testsupport.ExpectedActual("my short description", act.Action.Short))
	}
	if act.Action.Long != "my long description" {
		t.Error("long option was not applied", testsupport.ExpectedActual("my long description", act.Action.Long))
	}
	if !testsupport.SlicesUnorderedEqual(act.Action.Aliases, aliases) {
		t.Error("incorrect aliases", testsupport.ExpectedActual(aliases, act.Action.Aliases))
	}

	setName, setPath, setCust, setTestbool = "", "", 0, false // reset stuff set in createFunc
	t.Run("standard run", func(t *testing.T) {
		wantName := "nm"
		wantPath := "/tmp"
		wantCust := 1
		wantTestbool := true
		rootFS := pflag.FlagSet{}
		// set args
		testsupport.CheckSetArgs(t,
			act.Model.SetArgs,
			&rootFS,
			[]string{"--name=nm", "--path=/tmp", "--custom", fmt.Sprint(1), "--testbool"}, 50, 30,
			false, nil, false)
		for _, upd := range []tea.Msg{testsupport.SendHotkey(hotkeys.CursorUp), testsupport.SendHotkey(hotkeys.Invoke)} {
			act.Model.Update(upd)
		}
		act.Model.View()

		act.Model.Reset()

		// check results
		assert.Equal(t, wantName, setName)
		assert.Equal(t, wantPath, setPath)
		assert.Equal(t, wantCust, setCust)
		assert.Equal(t, wantTestbool, setTestbool)
	})
	setName, setPath, setCust, setTestbool = "", "", 0, false // reset stuff set in createFunc
	t.Run("rerun with no sets or new sets to ensure everything gets reset and clobbered properly", func(t *testing.T) {
		wantName := "nm2"
		wantPath := "/tmp/2"
		wantCust := 0
		wantTestbool := false
		rootFS := pflag.FlagSet{}
		// set args
		testsupport.CheckSetArgs(t,
			act.Model.SetArgs,
			&rootFS,
			[]string{"--name=nm2", "--path=/tmp/2"}, 50, 30,
			false, nil, false)
		for _, upd := range []tea.Msg{testsupport.SendHotkey(hotkeys.CursorUp), testsupport.SendHotkey(hotkeys.Invoke)} {
			act.Model.Update(upd)
		}
		act.Model.View()

		act.Model.Reset()

		// check results
		assert.Equal(t, wantName, setName)
		assert.Equal(t, wantPath, setPath)
		assert.Equal(t, wantCust, setCust)
		assert.Equal(t, wantTestbool, setTestbool)
	})
	t.Run("validate args", func(t *testing.T) {
		// we want to test 3 things:
		// 1) validate args is called (also that it can pass and fail normally)
		// 2) field-generated flags are accessible from validate
		// 3) additional flags are accessible from validate
		pair := scaffoldcreate.NewCreateAction("test",
			map[string]scaffoldcreate.Field{
				"one": {
					Title:    "field",
					Flag:     scaffoldcreate.FlagConfig{Name: "ff", Usage: "test field flag"},
					Provider: &scaffoldcreate.NumberProvider{},
				},
			},
			func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
				return 1, "", nil
			},
			scaffoldcreate.Options{
				CommonOptions: scaffold.CommonOptions{
					AddtlFlags: func() *pflag.FlagSet {
						fs := &pflag.FlagSet{}
						fs.Bool("nonfield", false, "addtl flag")
						return fs
					},
				},
			})
		// TODO run non-interactive
		// TODO run a Mother cycle
		// TODO run twice to check resetting
	})
}

// Tests that boolean providers operate as we expect.
func TestBoolean(t *testing.T) {
	var b1Value, b2Value bool

	var (
		b1 = scaffoldcreate.Field{
			Title:    "b1",
			Required: false,
			Order:    100,
			Provider: &scaffoldcreate.BoolProvider{},
		}
		b2 = scaffoldcreate.Field{
			Title:        "b2",
			Required:     false,
			DefaultValue: "true",
			Order:        100,
			Provider:     &scaffoldcreate.BoolProvider{},
		}
	)

	tests := []struct {
		name string
		args []string
		// the main cycle of inputs (via update) and view checks (via view);
		// this is where the core testing occurs
		mainCycle func(update func(msg tea.Msg) tea.Cmd, view func() string)
	}{
		{"no bool changes prior to submit",
			nil,
			func(update func(msg tea.Msg) tea.Cmd, view func() string) {
				// navigate down to, and press, submit
				update(testsupport.SendHotkey(hotkeys.CursorUp))
				wantV := testsupport.LinesTrimSpace(` b1:[ ]
         b2:[✓]
         ╭──────╮
        >│submit│
         ╰──────╯`)
				if v := testsupport.LinesTrimSpace(view()); v != wantV {
					t.Error("incorrect view after wrap to submit button", testsupport.ExpectedActual(wantV, v))
				}
				update(testsupport.SendHotkey(hotkeys.Invoke))
				// check that create was called and our fields have the values we expect
				if get, _ := strconv.ParseBool(b1.Provider.Get()); b1Value != false && b1Value != get {
					t.Errorf("incorrect field b1 values:\nExpected: false | set value: %v | provider get value: %v", b1Value, get)
				}
				if get, _ := strconv.ParseBool(b2.Provider.Get()); b2Value != true && b2Value != get {
					t.Errorf("incorrect field b2 values:\nExpected: false | set value: %v | provider get value: %v", b2Value, get)
				}
			},
		},
		{"invert each bool",
			nil,
			func(update func(msg tea.Msg) tea.Cmd, view func() string) {
				update(testsupport.SendHotkey(hotkeys.Select))
				update(testsupport.SendHotkey(hotkeys.CursorDown))
				update(testsupport.SendHotkey(hotkeys.Select))
				update(testsupport.SendHotkey(hotkeys.CursorDown))
				update(testsupport.SendHotkey(hotkeys.Invoke))
				wantV := testsupport.LinesTrimSpace(` b1:[✓]
         b2:[ ]
         ╭──────╮
        >│submit│
         ╰──────╯`)
				if v := testsupport.LinesTrimSpace(view()); v != wantV {
					t.Error("incorrect view after wrap to submit button", testsupport.ExpectedActual(wantV, v))
				}
				update(testsupport.SendHotkey(hotkeys.Invoke))
				// check that create was called and our fields have the values we expect
				if get, _ := strconv.ParseBool(b1.Provider.Get()); b1Value != true && b1Value != get {
					t.Errorf("incorrect field b1 values:\nExpected: false | set value: %v | provider get value: %v", b1Value, get)
				}
				if get, _ := strconv.ParseBool(b2.Provider.Get()); b2Value != false && b2Value != get {
					t.Errorf("incorrect field b2 values:\nExpected: false | set value: %v | provider get value: %v", b2Value, get)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := scaffoldcreate.NewCreateAction("bool_action", map[string]scaffoldcreate.Field{
				"b1": b1,
				"b2": b2,
			},
				func(fields map[string]scaffoldcreate.Field, fs *pflag.FlagSet) (id any, invalid string, err error) {
					b1Value, err = strconv.ParseBool(fields["b1"].Provider.Get())
					if err != nil {
						return 0, "", err
					}
					b2Value, err = strconv.ParseBool(fields["b2"].Provider.Get())
					return 0, "", err
				}, scaffoldcreate.Options{})
			if inv, _, err := pair.Model.SetArgs(&pflag.FlagSet{}, tt.args, 80, 60); err != nil || inv != "" {
				t.Fatalf("set args failed:\nerr: '%v' | invalid: '%v'", err, inv)
			}
			tt.mainCycle(pair.Model.Update, pair.Model.View)

		})
	}

}
