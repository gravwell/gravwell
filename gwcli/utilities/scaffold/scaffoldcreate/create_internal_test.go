//go:build ci

/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/spf13/pflag"
)

func TestMain(m *testing.M) {
	logPath := path.Join(os.TempDir(), "gwcli_create_internal_test", "dev.log")
	if err := os.MkdirAll(path.Dir(logPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory for clilog: %v", err)
		os.Exit(1)
	}

	if err := clilog.Init(logPath, "debug"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize clilog: %v", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func Test_createModel_basics(t *testing.T) {
	// create a couple dummy files in a temp directory to navigate to
	dummyfilePath := path.Join(t.TempDir(), "dummyfile1")
	if f, err := os.Create(dummyfilePath); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}

	cfg := map[string]Field{
		"A": {Required: true, Title: "A", Order: 10, Provider: &TextProvider{}},
		"B": {Required: true, Title: "B", Order: 0, Provider: &PathProvider{}},
	}
	ca := NewCreateAction("test", cfg, func(cfg Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
		return 0, "", nil
	}, Options{})
	cm, ok := ca.Model.(*createModel)
	if !ok {
		t.Fatal("failed to type assert to *createModel")
	}

	if len(cm.inputs.ordered) != 2 {
		t.Fatal(ExpectedActual(1, len(cm.inputs.ordered)))
	} else if cm.inputs.ordered[0] != "A" {
		t.Error(ExpectedActual("A", cm.inputs.ordered[0]))
	} else if cm.inputs.ordered[1] != "B" {
		t.Error(ExpectedActual("B", cm.inputs.ordered[1]))
	} else if cm.inputs.selected != 0 {
		t.Error("incorrect starting selected field.", ExpectedActual(0, cm.inputs.selected))
	}
	cm.focusNext()
	// should be the second field
	if cm.inputs.selected != 1 {
		t.Fatal("expected second field to be selected")
	} else if provider := cm.fields["B"].Provider.(*PathProvider); !provider.pti.Focused() {
		t.Fatal("focusing next did not, in fact, focus the next field")
	}
	// see if B auto completes to a file at its path
	{
		dir, _ := path.Split(dummyfilePath)
		for _, r := range dir {
			cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		// check for value and correct next suggestion
		provider := cm.fields["B"].Provider.(*PathProvider)
		if provider.pti.Value() != dir {
			t.Error("incorrect value on field \"B\"")
		}
		if curSgt := provider.pti.CurrentSuggestion(); curSgt != dummyfilePath {
			t.Error("incorrect current suggestion", ExpectedActual(dummyfilePath, curSgt))
		}
	}

	cm.focusNext()
	// should be the submit button
	if cm.inputs.selected != uint(len(cm.inputs.ordered)) {
		t.Fatal("expected submit button to be selected")
	}
	cm.focusNext()
	// should be the first field
	if cm.inputs.selected != 0 {
		t.Fatal("expected first field to be selected")
	}
	cm.focusPrevious()
	// should be the submit button
	if cm.inputs.selected != uint(len(cm.inputs.ordered)) {
		t.Fatal("expected submit button to be selected")
	}
}

// Tests that fields are correctly reordered by their Order
func Test_Ordering(t *testing.T) {
	cfg := map[string]Field{
		"3": {Required: true, Title: "3", Order: -10, Provider: &TextProvider{}},
		"4": {Required: true, Title: "4", Order: -50, Provider: &TextProvider{}},
		"1": {Required: true, Title: "1", Order: 50, Provider: &TextProvider{}},
		"2": {Required: false, Title: "2", Order: 0, Provider: &TextProvider{}},
	}

	cm := newCreateModelInitialize(cfg,
		func(cfg Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		}, Options{})

	for i, key := range cm.inputs.ordered {
		kint, err := strconv.Atoi(key)
		if err != nil {
			t.Fatal(err)
		} else if i+1 != kint {
			t.Error("bad ordering.", ExpectedActual(kint, i+1))
		}
	}
}

// Tests that we can successfully set values into each field.
func Test_ValueSetting(t *testing.T) {
	t.Run("all set", func(t *testing.T) {
		pair := NewCreateAction("test", Config{
			"A": Field{Required: true, Title: "A", Order: 0, Provider: &TextProvider{}},
			"B": Field{Required: false, Title: "B", Order: 10, Provider: &PathProvider{}},
			"C": Field{Required: true, Title: "C", Order: -10, Provider: &TextProvider{}},
		}, func(fields Config, fs *pflag.FlagSet) (id any, invalid string, err error) { return 0, "", nil }, Options{})

		cm, ok := pair.Model.(*createModel)
		if !ok {
			t.Fatal("failed to type assert to *createModel")
		}

		// set values into inputs
		for i, key := range cm.inputs.ordered {
			cm.fields[key].Provider.Set(fmt.Sprintf("%d", i))
		}

		// check that every field has a value set
		for key, fld := range cm.fields {
			v := fld.Provider.Get()
			if v == "" {
				t.Errorf("field %v does not have a value set", key)
			}
			num, err := strconv.Atoi(v)
			if err != nil {
				t.Errorf("failed to parse %s as an int (key: %s)", v, key)
			} else if cm.inputs.ordered[num] != key {
				t.Error(ExpectedActual(key, cm.inputs.ordered[num]))
			}
		}
	})
}

// E2E testing for a dummy create action.
// Runs the dummy action twice to ensure Reset and SetArgs work back to back.
//
// Does not utilize teatest as createModel is not a full tea.Model.
// Thus, input and output are handled manually.
func Test_Full(t *testing.T) {
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.Plain()

	var createdCalled bool

	cm := newCreateModelInitialize(
		Config{
			"A": Field{Required: true, Title: "A", Flag: FlagConfig{Name: "a"}, Order: 100, Provider: &TextProvider{}},
			"B": Field{Required: false, Title: "B", Flag: FlagConfig{Name: "b"}, Order: 50, Provider: &TextProvider{}},
		},
		func(cfg Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var bln bool
			if !fs.Parsed() {
				t.Errorf("flagset should be parsed")
			} else if bln, err = fs.GetBool("bln"); err != nil {
				t.Error(err)
			} else if !bln {
				t.Error("expected bln to be set by SetArgs()")
			}
			if v := cfg["A"].Provider.Get(); v != "a" {
				t.Error("incorrect value set into field A.", ExpectedActual("a", v))
			}

			createdCalled = true

			return "value", "", nil
		}, Options{
			CommonOptions: scaffold.CommonOptions{
				AddtlFlags: func() *pflag.FlagSet {
					fs := &pflag.FlagSet{}
					fs.Bool("bln", false, "some flag text")
					return fs
				},
			},
		})

	fauxMother(t, cm, &createdCalled)
	createdCalled = false
	fauxMother(t, cm, &createdCalled)
}

// helper function for Test_Full to allow it to be run back-by-back.
// Sets value 'a' into the first field and always passes the --bln flag.
func fauxMother(t *testing.T, cm *createModel, createdCalled *bool) {
	//t.Helper()
	if inv, _, err := cm.SetArgs(nil, []string{"--bln"}, 80, 50); err != nil {
		t.Fatal("failed to Set Args:", err)
	} else if inv != "" {
		t.Fatal("failed to validate valid args:", inv)
	}

	cm.Update(testsupport.SendHotkey(hotkeys.CursorDown))

	// split the output to check for the fields
	var out []string
	for line := range strings.SplitSeq(cm.View(), "\n") {
		// trim out empty lines
		line = strings.TrimSpace(line)
		out = append(out, line)
	}
	t.Log(out)
	if len(out) < 3 {
		t.Error("too few lines in output. Should be 3+.", ExpectedActual(3, len(out)))
	} else {
		t.Log(out[0])
		fieldName, _, found := strings.Cut(out[0], ":")
		if !found {
			t.Error("failed to parse line; did not find ':'")
		} else if strings.TrimSpace(fieldName) != "A" {
			t.Error("unexpected field name at index 0", ExpectedActual("A", strings.TrimSpace(fieldName)))
		}
	}

	// navigate to the submit button by underflowing
	cm.Update(testsupport.SendHotkey(hotkeys.CursorUp))
	cm.Update(testsupport.SendHotkey(hotkeys.CursorUp))
	if !cm.SubmitSelected() {
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.inputs.ordered)), cm.inputs.selected))
	}
	cm.Update(testsupport.SendHotkey(hotkeys.Invoke))
	// check for errors
	if cm.inputs.err == "" { // A is required and was not set
		t.Fatal("expected inputErr to be set due to missing requireds.")
	}
	// set A
	cm.Update(testsupport.SendHotkey(hotkeys.CursorDown))
	cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	cm.Update(testsupport.SendHotkey(hotkeys.CursorUp))
	if !cm.SubmitSelected() {
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.inputs.ordered)), cm.inputs.selected))
	}
	cm.Update(testsupport.SendHotkey(hotkeys.Invoke))
	if cm.inputs.err != "" {
		t.Fatalf("unexpected input error: %v", cm.inputs.err)
	} else if cm.createErr != "" {
		t.Fatalf("unexpected create error: %v", cm.createErr)
	} else if !(*createdCalled) {
		t.Error("create function was not called after hitting submit button")
	} else if !cm.Done() {
		t.Fatal("expected to be done after calling submit")
	}
	if err := cm.Reset(); err != nil {
		t.Errorf("failed to reset: %v", err)
	}
}

func setup(t *testing.T, cfg Config) *createModel {
	t.Helper()
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.Plain()
	cm := newCreateModelInitialize(cfg, func(cfg Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
		return 0, "", nil
	}, Options{})
	return cm
}

// wrapper around newCreateModel to initialize fields, as would normally be done by newCreateModel's caller.
func newCreateModelInitialize(
	fields Config,
	createFunc func(cfg Config, fs *pflag.FlagSet) (id any, invalid string, err error),
	options Options,
) *createModel {
	for _, f := range fields {
		f.Provider.Initialize(f.DefaultValue, f.Required)
	}

	return newCreateModel(fields, "test", createFunc, options)
}
