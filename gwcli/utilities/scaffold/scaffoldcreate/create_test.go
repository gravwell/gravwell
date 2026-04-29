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
			"path": scaffoldcreate.FieldPath("test"),
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
	if !testsupport.SlicesUnorderedEqual(act.Action.Aliases, aliases) {
		t.Error("incorrect aliases", testsupport.ExpectedActual(aliases, act.Action.Aliases))
	}

	tests := []struct {
		testName string
		args     []string
		setArgs  struct {
			wantInvalid string
			wantCmd     bool // true iff a cmd should be returned, false if cmd should == nil
			wantErr     bool // early exists if an err is returned
		}
		updates []tea.Msg // remember to hit enter if you want things populated

		wantName     string
		wantPath     string
		wantCust     int
		wantTestbool bool
	}{
		{"set all fields and addtl flags from args",
			[]string{"--name=nm", "--path=/tmp", "--custom", fmt.Sprint(1), "--testbool"},
			struct {
				wantInvalid string
				wantCmd     bool
				wantErr     bool
			}{"", false, false},
			[]tea.Msg{testsupport.SendHotkey(hotkeys.CursorUp), testsupport.SendHotkey(hotkeys.Invoke)},
			"nm", "/tmp", 1, true,
		},
	}

	for _, tt := range tests {
		// reset sets before the next test
		setName, setPath, setCust, setTestbool = "", "", 0, false
		t.Run(tt.testName, func(t *testing.T) {
			rootFS := pflag.FlagSet{}
			{ // set args
				invalid, cmd, err := act.Model.SetArgs(&rootFS, tt.args, 50, 30)
				if invalid != tt.setArgs.wantInvalid {
					t.Error("setArgs: incorrect invalid", testsupport.ExpectedActual(tt.setArgs.wantInvalid, invalid))
				}
				if tt.setArgs.wantCmd && (cmd == nil) {
					t.Error("setArgs: expected cmd but cmd is nil")
				} else if !tt.setArgs.wantCmd && (cmd != nil) {
					t.Error("setArgs: expected nil cmd but cmd is not nil")
				}
				if tt.setArgs.wantErr && (err == nil) {
					t.Error("setArgs: expected error but err is nil")
				} else if !tt.setArgs.wantErr && (err != nil) {
					t.Error("setArgs: expected nil error but err is not nil")
				}
				if err != nil {
					return
				}
			}
			{ // update
				for _, upd := range tt.updates {
					act.Model.Update(upd)
				}
			}
			{ // view
				act.Model.View()
			}
			{ // reset
				act.Model.Reset()
			}
			// check results
			if setName != tt.wantName {
				t.Error("incorrect name value", testsupport.ExpectedActual(tt.wantName, setName))
			}
			if setPath != tt.wantPath {
				t.Error("incorrect path value", testsupport.ExpectedActual(tt.wantPath, setPath))
			}
			if setCust != tt.wantCust {
				t.Error("incorrect cust value", testsupport.ExpectedActual(tt.wantCust, setCust))
			}
			if setTestbool != tt.wantTestbool {
				t.Error("incorrect testBool value", testsupport.ExpectedActual(tt.wantTestbool, setTestbool))
			}
		})
	}
}

// Tests that boolean providers operate as we expect.
func TestBoolean(t *testing.T) {
	var b1Value, b2Value bool

	var (
		b1 = scaffoldcreate.Field{
			Title:    "b1",
			Required: false, // TODO omit from missingRequired to make ineffectual
			Order:    100,
			Provider: &scaffoldcreate.BoolProvider{},
		}
		b2 = scaffoldcreate.Field{
			Title:    "b2",
			Required: false,
			Order:    100,
			Provider: &scaffoldcreate.BoolProvider{Initial: true},
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
