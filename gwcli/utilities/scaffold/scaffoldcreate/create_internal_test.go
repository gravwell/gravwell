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
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
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
		"A": {Required: true, Type: Text, Title: "A", Order: 10},
		"B": {Required: true, Type: File, Title: "B", Order: 0},
	}
	ca := NewCreateAction("test", cfg, func(cfg Config, values map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
		return 0, "", nil
	}, func() pflag.FlagSet {
		return pflag.FlagSet{}
	})
	cm, ok := ca.Model.(*createModel)
	if !ok {
		t.Fatal("failed to type assert to *createModel")
	}

	if len(cm.inputs.ordered) != 2 {
		t.Fatal(ExpectedActual(1, len(cm.inputs.ordered)))
	} else if cm.inputs.ordered[0].Key != "A" {
		t.Error(ExpectedActual("A", cm.inputs.ordered[0].Key))
	} else if cm.inputs.ordered[1].Key != "B" {
		t.Error(ExpectedActual("B", cm.inputs.ordered[1].Key))
	}
	cm.focusNext()
	// should be the second field
	if cm.inputs.selected != 1 {
		t.Fatal("expected second field to be selected")
	}
	// see if B auto completes to a file at its path
	{
		dir, _ := path.Split(dummyfilePath)
		for _, r := range dir {
			cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		// check for value and correct next suggestion
		pti := cm.inputs.PTIs["B"]
		if pti.Value() != dir {
			t.Error("incorrect value on field \"B\"")
		}
		if curSgt := pti.CurrentSuggestion(); curSgt != dummyfilePath {
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

func Test_Ordering(t *testing.T) {
	cfg := map[string]Field{
		"3": {Required: true, Type: Text, Title: "3", Order: -10},
		"4": {Required: true, Type: Text, Title: "4", Order: -50},
		"1": {Required: true, Type: Text, Title: "1", Order: 50},
		"2": {Required: false, Type: Text, Title: "2", Order: 0},
	}
	cm := newCreateModel(cfg, "test",
		func(cfg Config, values map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		}, func() pflag.FlagSet {
			return pflag.FlagSet{}
		})

	for i, ti := range cm.inputs.ordered {
		kint, err := strconv.Atoi(ti.Key)
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
			"A": Field{Required: true, Type: Text, Title: "A", Order: 0},
			"B": Field{Required: false, Type: File, Title: "B", Order: 10},
			"C": Field{Required: true, Type: Text, Title: "C", Order: -10},
		})
		// set values into inputs
		for i, o := range cm.inputs.ordered {
			switch o.Type {
			case File:
				cm.inputs.PTIs[o.Key].SetValue(fmt.Sprintf("%d", i))
			case Text:
				cm.inputs.TIs[o.Key].SetValue(fmt.Sprintf("%d", i))
			}
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
			} else if cm.inputs.ordered[num].Key != key || cm.getInputValue(key, fld.Type) != v {
				t.Error("mismatching values after extraction.",
					ExpectedActual(cm.getInputValue(key, fld.Type), v))
			}
		}
	})
	t.Run("missing required", func(t *testing.T) {
		cm := setup(t, Config{
			"A": Field{Required: true, Type: Text, Title: "A", Order: 0},
			"B": Field{Required: false, Type: Text, Title: "B", Order: 10},
			"C": Field{Required: true, Type: Text, Title: "C", Order: -10},
		})
		// extract values from TIs
		_, mr := cm.extractValuesFromTIs()
		if len(mr) != 2 {
			t.Error("incorrect missing required count.", ExpectedActual(2, len(mr)))
		}

		// set one of the requireds and try again
		cm.inputs.TIs[cm.inputs.ordered[1].Key].SetValue("test value") // A
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
	// use a consistent color scheme
	stylesheet.Cur = stylesheet.Plain()

	var createdCalled bool

	cm := newCreateModel(
		Config{
			"A": Field{Required: true, Type: Text, Title: "A", FlagName: "a", Order: 100},
			"B": Field{Required: false, Type: Text, Title: "B", FlagName: "b", Order: 50},
		}, "test",
		func(cfg Config, values map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
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
	//t.Helper()
	if inv, _, err := cm.SetArgs(nil, []string{"--bln"}, 80, 50); err != nil {
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
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.inputs.ordered)), cm.inputs.selected))
	}
	cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// check for errors
	if cm.inputs.err == "" { // A is required and was not set
		t.Fatal("expected inputErr to be set due to missing requireds.")
	}
	// set A
	cm.Update(tea.KeyMsg{Type: tea.KeyDown})
	cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	cm.Update(tea.KeyMsg{Type: tea.KeyUp})
	if !cm.SubmitSelected() {
		t.Fatal("expected the cursor to be on the submit button.", ExpectedActual(uint(len(cm.inputs.ordered)), cm.inputs.selected))
	}
	cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	cm := newCreateModel(
		cfg, "test",
		func(cfg Config, values map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error) {
			return 0, "", nil
		},
		func() pflag.FlagSet { return pflag.FlagSet{} })
	return cm
}
