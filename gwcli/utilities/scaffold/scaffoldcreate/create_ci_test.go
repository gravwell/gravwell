//go:build !race
// +build !race

/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/spf13/pflag"
)

func Test_createModel_basics(t *testing.T) {
	cfg := map[string]Field{
		"A": NewField(true, "A", 0),
		"B": NewField(true, "B", 0),
	}
	ca := NewCreateAction("test", cfg, func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
		return 0, "", nil
	}, func() pflag.FlagSet {
		return pflag.FlagSet{}
	})
	cm, ok := ca.Model.(*createModel)
	if !ok {
		t.Fatal("failed to type assert to *createModel")
	}

	if len(cm.orderedTIs) != 2 {
		t.Fatal(ExpectedActual(1, len(cm.orderedTIs)))
	} else if cm.orderedTIs[0].key != "A" {
		t.Fatal(ExpectedActual("A", cm.orderedTIs[0].key))
	} else if cm.orderedTIs[1].key != "B" {
		t.Fatal(ExpectedActual("B", cm.orderedTIs[1].key))
	}
	cm.focusNext()
	// should be the second field
	if cm.selected != 1 {
		t.Fatal("expected second field to be selected")
	}
	cm.focusNext()
	// should be the submit button
	if cm.selected != uint(len(cm.orderedTIs)) {
		t.Fatal("expected submit button to be selected")
	}
	cm.focusNext()
	// should be the first field
	if cm.selected != 0 {
		t.Fatal("expected first field to be selected")
	}
	cm.focusPrevious()
	// should be the submit button
	if cm.selected != uint(len(cm.orderedTIs)) {
		t.Fatal("expected submit button to be selected")
	}
}

func Test_Ordering(t *testing.T) {
	cfg := map[string]Field{
		"3": NewField(true, "3", -10),
		"4": NewField(true, "4", -50),
		"1": NewField(true, "1", 50),
		"2": NewField(false, "2", 0),
	}
	cm := newCreateModel(cfg, "test",
		func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		}, func() pflag.FlagSet {
			return pflag.FlagSet{}
		})

	for i, ti := range cm.orderedTIs {
		kint, err := strconv.Atoi(ti.key)
		if err != nil {
			t.Fatal(err)
		} else if i+1 != kint {
			t.Error("bad ordering.", ExpectedActual(kint, i+1))
		}
	}
}

func Test_ExtractValues(t *testing.T) {
	t.Run("all set", func(t *testing.T) {
		cm := setup(t, Config{
			"A": NewField(true, "A", 0),
			"B": NewField(false, "B", 10),
			"C": NewField(true, "C", -10),
		})
		// set values into all TIs
		for i := range cm.orderedTIs {
			cm.orderedTIs[i].ti.SetValue(fmt.Sprintf("%d", i))
		}

		// extract values from TIs
		values, mr := cm.extractValuesFromTIs()
		if len(mr) != 0 {
			t.Errorf("missing required (%v) setting all TIs", mr)
		}
		for key, fld := range cm.fields {
			v, ok := values[key]
			if !ok {
				t.Errorf("failed to find value for key %v (field: %v)", key, fld)
			}
			num, err := strconv.Atoi(v)
			if err != nil {
				t.Errorf("failed to parse %v as an int", v)
			} else if cm.orderedTIs[num].key != key || cm.orderedTIs[num].ti.Value() != v {
				t.Error("mismatching values after extraction.",
					ExpectedActual(cm.orderedTIs[num].key, key),
					ExpectedActual(cm.orderedTIs[num].ti.Value(), v))
			}
		}
	})
	t.Run("missing required", func(t *testing.T) {
		cm := setup(t, Config{
			"A": NewField(true, "A", 0),
			"B": NewField(false, "B", 10),
			"C": NewField(true, "C", -10),
		})
		// extract values from TIs
		_, mr := cm.extractValuesFromTIs()
		if len(mr) != 2 {
			t.Error("incorrect missing required count.", ExpectedActual(2, len(mr)))
		}

		// set one of the requireds and try again
		cm.orderedTIs[1].ti.SetValue("test value") // A
		_, mr = cm.extractValuesFromTIs()
		if len(mr) != 1 {
			t.Error("incorrect missing required count.", ExpectedActual(1, len(mr)))
		}
	})
}

// E2E testing for a dummy create action.
// Runs the dummy action twice to ensure Reset and SetArgs work back to back.
//
// Does not utilize teatest as createModel is not a full tea.Model.
// Thus, input and output are handled manually.
func Test_Full(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.NoColor()

	var createdCalled bool

	cm := newCreateModel(
		Config{
			"A": NewField(true, "A", 100),
			"B": NewField(false, "B", 50),
		}, "test",
		func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
			var bln bool
			if !fs.Parsed() {
				t.Errorf("flagset should be parsed")
			} else if bln, err = fs.GetBool("bln"); err != nil {
				t.Error(err)
			} else if !bln {
				t.Error("expected bln to be set by SetArgs()")
			}
			_, ok := values["A"]
			if !ok {
				t.Error("failed to get the value assigned to A")
			}

			createdCalled = true

			return "value", "", nil
		},
		func() pflag.FlagSet {
			fs := pflag.FlagSet{}
			fs.Bool("bln", false, "some flag text")
			return fs
		})

	fauxMother(t, cm, &createdCalled)
	createdCalled = false
	fauxMother(t, cm, &createdCalled)
}

// helper function for Test_Full to allow it to be run back-by-back.
func fauxMother(t *testing.T, cm *createModel, createdCalled *bool) {
	t.Helper()
	if inv, _, err := cm.SetArgs(nil, []string{"--bln"}); err != nil {
		t.Fatal("failed to Set Args:", err)
	} else if inv != "" {
		t.Fatal("failed to validate valid args:", inv)
	}
	cm.Update(tea.KeyMsg{
		Type: tea.KeyDown,
		//Runes []rune
		//Alt   bool
		//Paste bool
	})

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
	cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !cm.SubmitSelected() {
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.orderedTIs)), cm.selected))
	}
	cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// check for errors
	if cm.inputErr == "" { // A is required and was not set
		t.Fatal("expected inputErr to be set due to missing requireds.")
	}
	// set A
	cm.Update(tea.KeyMsg{Type: tea.KeyDown})
	cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !cm.SubmitSelected() {
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.orderedTIs)), cm.selected))
	}
	cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cm.inputErr != "" {
		t.Fatalf("unexpected input error: %v", cm.inputErr)
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
	stylesheet.Cur = stylesheet.NoColor()
	cm := newCreateModel(
		cfg, "test",
		func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		},
		func() pflag.FlagSet { return pflag.FlagSet{} })
	return cm
}
