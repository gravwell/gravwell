/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate_test

import (
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/spf13/pflag"
)

func TestTextProvider(t *testing.T) {
	t.Run("simple get set", func(t *testing.T) {
		t.Parallel()
		var customInitCalled bool
		p := scaffoldcreate.NewField("title0", false, &scaffoldcreate.TextProvider{
			CustomInit: func() textinput.Model {
				ti := stylesheet.NewTI("", true)
				ti.Width = 50
				customInitCalled = true
				return ti
			},
		})
		p.Provider.Initialize("def", false)
		if p.Provider.Get() != "def" {
			t.Fatal("failed to get default value out of provider")
		} else if !customInitCalled {
			t.Fatal("custom init was not called")
		}
		p.Provider.Set("new val")
		if p.Provider.Get() != "new val" {
			t.Fatal("failed to get set value out of provider")
		}
	})
	t.Run("full mother cycle + hook set args to alter the TI", func(t *testing.T) {
		t.Parallel()
		// tests that TextProvider both successfully applies a CustomSetArgs and operates as expected over the course of a full Mother cycle
		var invocationCount = 0
		provider := &scaffoldcreate.TextProvider{CustomSetArgs: func(m textinput.Model) textinput.Model {
			// count invocations and set the TI's char limit to invocation count so we have something to check that TIs are being updated
			invocationCount += 1
			m.CharLimit = invocationCount
			return m
		}}
		f := scaffoldcreate.NewField("invoke", false, provider)
		f.Flag.Shorthand = 't'
		var create string
		pair := scaffoldcreate.NewCreateAction("test",
			scaffoldcreate.Config{"t": f},
			func(fields scaffoldcreate.Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
				create = fields["t"].Provider.Get()
				return 0, "", nil
			},
			scaffoldcreate.Options{})

		// manually execute SetArg
		f.Provider.SetArgs(0, 0)
		if invocationCount != 1 {
			t.Fatal("invocation count did not increment with SetArgs")
		}

		// manually execute Mother cycle
		testsupport.CheckSetArgs(t, pair.Model, &pflag.FlagSet{}, nil, 0, 0, "", nil, nil)
		pair.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // enter some characters into the field
		pair.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		pair.Model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		{
			v := pair.Model.View()
			// slice up the view and check for our inputs, limited to 2 by CharLimit
			_, after, found := strings.Cut(v, "invoke:")
			if !found {
				t.Fatal("failed to find field \"invoke\" in view:\n", testsupport.Uncloak(v))
			}
			before, _, _ := strings.Cut(after, "\n")
			if before = strings.TrimSpace(before); before != "ab" {
				t.Fatal("bad value after \"invoke\" field title:", testsupport.ExpectedActual("ab", before))
			}
		}
		pair.Model.Update(testsupport.SendHotkey(hotkeys.CursorDown))
		pair.Model.Update(testsupport.SendHotkey(hotkeys.Invoke)) // submit

		if create != "ab" { // create should have been set as part of submission
			t.Fatal("bad value from createFunc", testsupport.ExpectedActual("ab", create))
		}
		pair.Model.Reset()
		if x := provider.Get(); x != "" {
			t.Fatal("provider value not destroyed by Reset. Lingering value: ", x)
		}
		testsupport.CheckSetArgs(t, pair.Model, &pflag.FlagSet{}, []string{"-t=YungVenuz"}, 0, 0, "", nil, nil)
		if x := provider.Get(); x != "Yun" { // should be limited by our CharLimit
			t.Fatal("bad value after second SetArgs", testsupport.ExpectedActual("Yun", x))
		}

	})
}

func TestPathProvider(t *testing.T) {
	t.Run("accept completion", func(t *testing.T) {
		// set PWD and create some files to test path completion against
		tDir := t.TempDir()
		t.Chdir(tDir)
		{
			f, err := os.Create("f1.txt")
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		{
			f, err := os.Create("f2.txt")
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		{
			// directory with 3 files inside of it:
			// x1
			// x2
			// x3
			if err := os.Mkdir("xdir", 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(path.Join("xdir", "x1.txt"))
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
			f, err = os.Create(path.Join("xdir", "x2.txt"))
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
			f, err = os.Create(path.Join("xdir", "x3.txt"))
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
		}

		provider := &scaffoldcreate.PathProvider{}
		f := scaffoldcreate.NewField("path", true, provider)
		pair := scaffoldcreate.NewCreateAction("test",
			scaffoldcreate.Config{"path": f},
			func(fields scaffoldcreate.Config, fs *pflag.FlagSet) (id any, invalid string, err error) {
				return 0, "", nil
			}, scaffoldcreate.Options{})
		testsupport.CheckSetArgs(t, pair.Model, &pflag.FlagSet{}, nil, 80, 60, "", nil, nil)

		// before we enter anything, check for suggestions
		pair.Model.Update(nil)
		{
			v := pair.Model.View()
			lines := strings.Split(v, "\n")
			if len(lines) < 3 {
				t.Fatalf("unexpected line count while parsing view '%s'", testsupport.Uncloak(v))
			}
			// first line should be our field
			if _, _, found := strings.Cut(lines[0], "path:"); !found {
				t.Fatal("first line does not contain expected title. Line: ", lines[0])
			}
			// second line should contain our completions
			lines[1] = strings.TrimSpace(lines[1])
			wantSuggestions := []string{"f1.txt", "f2.txt", "xdir"}
			if suggestions := strings.Split(lines[1], " "); !slices.Equal(suggestions, wantSuggestions) {
				t.Fatal(testsupport.ExpectedActual(wantSuggestions, suggestions))
			}
		}

		t.Run("completion of partial values", func(t *testing.T) {
			testsupport.TypeModel(pair.Model, "f1")
			pair.Model.Update(testsupport.SendHotkey(hotkeys.Complete))
			if val := provider.Get(); val != "f1.txt" {
				t.Fatal("autocomplete failed", testsupport.ExpectedActual("f1.txt", val))
			}
			// clear
			provider.Set("")
			// test manual autocompletion to a file in a subdirectory
			testsupport.TypeModel(pair.Model, "xdir/x1")
			pair.Model.Update(testsupport.SendHotkey(hotkeys.Complete))
			if val := provider.Get(); val != "xdir/x1.txt" {
				t.Fatal("autocomplete failed", testsupport.ExpectedActual("xdir/x1.txt", val))
			}
		})

	})
}

func TestMSLProvider(t *testing.T) {
	// Spin up an MSL as a Field and test that it can:
	// 1. enter takeover mode
	// 2. select and deselect items
	// 3. exit takeover mode
	// 4. and fetch selected items
	t.Parallel()
	items := []multiselectlist.SelectableItem[string]{
		&multiselectlist.DefaultSelectableItem[string]{Title_: "1", Description_: "desc1", ID_: "one"},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "2", Description_: "desc2", ID_: "two"},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "3", Description_: "desc3", Selected_: true, ID_: "three"},
	}

	t.Run("hide description", func(t *testing.T) {
		baseProvider := scaffoldcreate.NewMSLProvider(items,
			scaffoldcreate.MSLOptions{ListOptions: multiselectlist.Options{HideDescription: true}},
		)
		f := scaffoldcreate.NewField("msl", true, baseProvider)
		f.Provider.Initialize("", false)
		f.Provider.SetArgs(40, 20)
		// enter takeover mode
		_, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select))
		if !takeover {
			t.Fatal("failed to enter takeover mode")
		}
		kind, view, _ := f.Provider.View(true, 40)
		if kind != scaffoldcreate.Takeover {
			t.Error("incorrect view kind", testsupport.ExpectedActual(scaffoldcreate.Takeover, kind))
		}
		want := testsupport.LinesTrimSpace(`   List                                                               
                                                                      
  3 items                                                             
                                                                      
│ [ ] 1                                                               
                                                                      
  [ ] 2                                                               
                                                                      
  [✓] 3                                                               
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
                                                                      
  ↑ cursor up • ↓ cursor down • \ filter • shift+← clear filter • ↹ accept • ctrl+\ cancel filter • esc quit • ? more
  space select • ↲ continue`)
		if view = testsupport.LinesTrimSpace(view); view != want {
			t.Fatal("incorrect no descriptions view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(view)))
		}

	})

	var f scaffoldcreate.Field
	{
		baseProvider := scaffoldcreate.NewMSLProvider(items,
			scaffoldcreate.MSLOptions{ListOptions: multiselectlist.Options{}},
		)
		baseProvider.RequireAtLeast = 2
		f = scaffoldcreate.NewField("msl", true, baseProvider)
	}

	f.Provider.Initialize(f.DefaultValue, f.Required)

	t.Run("set nonexistent value", func(t *testing.T) {
		// set dne
		if invalid := f.Provider.Set("does not exist"); invalid == "" {
			t.Fatal("successfully set nonexistent value")
		}
	})

	// initial get should only return the preselected item
	selected := f.Provider.Get()
	if selected != "three" {
		t.Fatal("incorrect selected", testsupport.ExpectedActual("three", selected))
	}
	// with only a single item selected, the field should be unsatisfied
	if invalid := f.Provider.Satisfied(); invalid == "" {
		t.Fatal("Provider should be unsatisfied due to only having a single item selected")
	}
	t.Run("do not enter takeover mode if unselected", func(t *testing.T) {
		// an update that *would* invoke takeover mode should be ignored if not selected
		_, takeover := f.Provider.Update(false, testsupport.SendHotkey(hotkeys.Select))
		if takeover {
			t.Error("takeover mode entered while not selected")
		}
		kind, _, _ := f.Provider.View(false, 80)
		if kind == scaffoldcreate.Takeover {
			t.Error("view believed it was in takeover mode")
		}
	})
	t.Run("enter takeover mode when selected", func(t *testing.T) {
		_, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select))
		if !takeover {
			t.Fatal("failed to enter takeover mode while selected")
		}
		kind, _, _ := f.Provider.View(true, 80)
		if kind != scaffoldcreate.Takeover {
			t.Error("view did not believe it was in takeover mode")
		}
		t.Run("select all the first 2 items; deselect the last item", func(t *testing.T) {
			// select the first item
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select)); !takeover {
				t.Fatal("update after first item exited takeover mode")
			}
			if inv := f.Provider.Satisfied(); inv != "" {
				t.Errorf("provider is not satisfied despite 2 items being selected. Reason: %s", inv)
			}
			// select the second item
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.CursorDown)); !takeover {
				t.Fatal("update during second item exited takeover mode")
			}
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select)); !takeover {
				t.Fatal("update after second item exited takeover mode")
			}
			if inv := f.Provider.Satisfied(); inv != "" {
				t.Errorf("provider is not satisfied despite 3 items being selected. Reason: %s", inv)
			}
			// deselect the third item
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.CursorDown)); !takeover {
				t.Fatal("update during third item exited takeover mode")
			}
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select)); !takeover {
				t.Fatal("update after third item exited takeover mode")
			}
			if inv := f.Provider.Satisfied(); inv != "" {
				t.Errorf("provider is not satisfied despite 2 items being selected. Reason: %s", inv)
			}
			// ensure view agrees we are in takeover mode
			kind, _, _ := f.Provider.View(false, 80)
			if kind != scaffoldcreate.Takeover {
				t.Error("view did not believe it was in takeover mode")
			}
			if x := f.Provider.Get(); x != "one,two" {
				t.Error("incorrect selected items.", testsupport.ExpectedActual("one,two", x))
			}
		})
		t.Run("invoke to exit; ensure we can reenter", func(t *testing.T) {
			// exit the list
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Invoke)); takeover {
				t.Fatal("invoke hotkey did not trigger an exit")
			}
			// ensure view agrees we have left takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind == scaffoldcreate.Takeover {
				t.Error("view believes it is still in takeover mode")
			}
			// reenter the list
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select)); !takeover {
				t.Fatal("failed to reenter takeover mode")
			}
			// ensure view agrees we are in takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind != scaffoldcreate.Takeover {
				t.Error("view did not believe it was in takeover mode")
			}
			// move around a bit
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.CursorUp)); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.CursorUp)); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.CursorDown)); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			// ensure view agrees we are in takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind != scaffoldcreate.Takeover {
				t.Error("view did not believe it was in takeover mode")
			}
			// exit the list
			if _, takeover := f.Provider.Update(true, testsupport.SendHotkey(hotkeys.Invoke)); takeover {
				t.Fatal("invoke hotkey did not trigger an exit")
			}
			// ensure view agrees we have left takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind == scaffoldcreate.Takeover {
				t.Error("view believes it is still in takeover mode")
			}
		})
	})
}

// tests thats MSLs operate as normal when no list is given at create time.
// A common scenario: list is composed of options that are queried from the server and thus not available until SetArgs.
func TestMSLProviderLateBinding(t *testing.T) {
	t.Parallel()
	var f scaffoldcreate.Field
	items := []multiselectlist.SelectableItem[string]{
		&multiselectlist.DefaultSelectableItem[string]{Title_: "1", Description_: "desc1", ID_: "one"},
	}
	{
		baseProvider := scaffoldcreate.NewMSLProvider(nil, scaffoldcreate.MSLOptions{
			SetArgsInsertItems: func(currentItems []multiselectlist.SelectableItem[string]) (_ []multiselectlist.SelectableItem[string]) {
				return items // reset to current state of items array
			},
		})
		baseProvider.RequireAtMost = 2
		f = scaffoldcreate.NewField("msl", true, baseProvider)
	}

	f.Provider.Initialize("", false)
	if selected := f.Provider.Get(); selected != "" { // just making sure this doesn't panic
		t.Errorf("selected contains data: %v", selected)
	}
	f.Provider.SetArgs(80, 60)
	if invalid := f.Provider.Set("one"); invalid != "" {
		t.Error("failed to mark \"one\" as selected: ", invalid)
	} else if selected := f.Provider.Get(); selected != "one" {
		t.Error("incorrect selected items after selecting item one")
	}

	// reroll, check that the list was updated properly
	items = []multiselectlist.SelectableItem[string]{
		&multiselectlist.DefaultSelectableItem[string]{Title_: "1", Description_: "desc1", ID_: "one"},
		&multiselectlist.DefaultSelectableItem[string]{Title_: "2", Description_: "desc2", ID_: "two"},
	}
	f.Provider.Reset()
	f.Provider.SetArgs(80, 60)
	if selected := f.Provider.Get(); selected != "" { // one should have been reset to unselected
		t.Error("incorrect selected state after setting args again", testsupport.ExpectedActual("", selected))
	}
	// try to set both
	if invalid := f.Provider.Set("one"); invalid != "" {
		t.Error("failed to mark \"one\" as selected: ", invalid)
	}
	if invalid := f.Provider.Set("two"); invalid != "" {
		t.Error("failed to mark \"two\" as selected: ", invalid)
	}
	if selected := f.Provider.Get(); selected != "one,two" { // one should have been reset to unselected
		t.Error("incorrect selected state after setting args again", testsupport.ExpectedActual("one,two", selected))
	}

}

func TestBooleanProvider(t *testing.T) {
	t.Run("get/set, satisfied", func(t *testing.T) {
		t.Parallel()
		p := scaffoldcreate.NewField("reaper", false, &scaffoldcreate.BooleanProvider{})
		p.Provider.Initialize("unused", false)

		if invalid := p.Provider.Satisfied(); invalid != "" {
			t.Error("bool providers should never be unsatisfied")
		}

		// get default
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if b {
			t.Error("checkbox should be false")
		}

		if invalid := p.Provider.Set("true"); invalid != "" {
			t.Fatalf("failed to set: %s", invalid)
		}
		// get the set value
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if !b {
			t.Error("checkbox should be true")
		}

		if invalid := p.Provider.Satisfied(); invalid != "" {
			t.Error("bool providers should never be unsatisfied")
		}
	})
	t.Run("full mother cycle with initial value", func(t *testing.T) {
		t.Parallel()
		initial := true
		p := scaffoldcreate.NewField("chelicerate", false, &scaffoldcreate.BooleanProvider{Initial: initial})
		p.Provider.Initialize("unused", false)

		// get default
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if !b {
			t.Error("checkbox should be true")
		}
		// run a cycle
		p.Provider.SetArgs(0, 0)
		p.Provider.Update(false, nil)
		p.Provider.View(false, 0)
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if b != initial {
			t.Fatal("provider is no longer initial value", testsupport.ExpectedActual(initial, b))
		}
		p.Provider.Reset()
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if b != initial {
			t.Fatal("provider is no longer initial value", testsupport.ExpectedActual(initial, b))
		}
		// run a cycle, but change the value this time
		p.Provider.SetArgs(0, 0)
		p.Provider.Update(true, testsupport.SendHotkey(hotkeys.Select))
		p.Provider.View(false, 0)
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if b == initial {
			t.Fatal("provider is still initial value after toggling", testsupport.ExpectedActual(initial, b))
		}
		p.Provider.Reset()
		if b, err := strconv.ParseBool(p.Provider.Get()); err != nil {
			t.Fatal(err)
		} else if b != initial {
			t.Fatal("provider is not initial value after reset", testsupport.ExpectedActual(initial, b))
		}
	})
	t.Run("view", func(t *testing.T) {
		t.Parallel()
		p := scaffoldcreate.NewField("ghost", false, &scaffoldcreate.BooleanProvider{})
		p.Provider.Initialize("unused", false)
		p.Provider.SetArgs(0, 0)
		p.Provider.Update(false, nil)

		kind, view, sl := p.Provider.View(false, 0)
		want := stylesheet.Checkbox(false) // reminder: titles are added later, by scaffoldcreate
		if kind != scaffoldcreate.TitleValue {
			t.Error("bad kind", testsupport.ExpectedActual(scaffoldcreate.TitleValue, kind))
		}
		if sl != "" {
			t.Errorf("why does second line have a value? '%s'", sl)
		}
		if want != view {
			t.Error("incorrect view (whitespace stripped)", testsupport.ExpectedActual(want, view))
		}

	})
}
