package scaffoldcreate_test

import (
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
	"github.com/spf13/pflag"
)

func TestGetSet(t *testing.T) {
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
}

func TestTextProvider(t *testing.T) {
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
		pair.Model.Update(tea.KeyMsg{Type: hotkeys.CursorDown}) // submit
		pair.Model.Update(tea.KeyMsg{Type: hotkeys.Invoke})

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
			pair.Model.Update(tea.KeyMsg{Type: hotkeys.Complete})
			if val := provider.Get(); val != "f1.txt" {
				t.Fatal("autocomplete failed", testsupport.ExpectedActual("f1.txt", val))
			}
			// clear
			provider.Set("")
			// test manual autocompletion to a file in a subdirectory
			testsupport.TypeModel(pair.Model, "xdir/x1")
			pair.Model.Update(tea.KeyMsg{Type: hotkeys.Complete})
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
	items := []list.DefaultItem{
		testItem{"ttl1", "desc1"},
		testItem{"ttl2", "desc2"},
		testItem{"ttl3", "desc3"},
	}

	var f scaffoldcreate.Field
	{
		baseProvider := scaffoldcreate.NewMSLProvider(items,
			multiselectlist.Options{Preselected: map[uint]bool{2: true}})
		baseProvider.RequireAtLeast = 2
		f = scaffoldcreate.NewField("msl", true, baseProvider)
	}

	f.Provider.Initialize(f.DefaultValue, f.Required)

	// initial get should only return the preselected item
	selected := f.Provider.Get()
	if selected != "ttl3" {
		t.Fatal()
	}
	// with only a single item selected, the field should be unsatisfied
	if invalid := f.Provider.Satisfied(); invalid == "" {
		t.Fatal("Provider should be unsatisfied due to only having a single item selected")
	}
	t.Run("do not enter takeover mode if unselected", func(t *testing.T) {
		// an update that *would* invoke takeover mode should be ignored if not selected
		_, takeover := f.Provider.Update(false, tea.KeyMsg{Type: hotkeys.Select})
		if takeover {
			t.Error("takeover mode entered while not selected")
		}
		kind, _, _ := f.Provider.View(false, 80)
		if kind == scaffoldcreate.Takeover {
			t.Error("view believed it was in takeover mode")
		}
	})
	t.Run("enter takeover mode when selected", func(t *testing.T) {
		_, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Select})
		if !takeover {
			t.Fatal("failed to enter takeover mode while selected")
		}
		kind, _, _ := f.Provider.View(false, 80)
		if kind != scaffoldcreate.Takeover {
			t.Error("view did not believe it was in takeover mode")
		}
		t.Run("select all the first 2 items; deselect the last item", func(t *testing.T) {
			// select the first item
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Select}); !takeover {
				t.Fatal("update after first item exited takeover mode")
			}
			if inv := f.Provider.Satisfied(); inv != "" {
				t.Errorf("provider is not satisfied despite 2 items being selected. Reason: %s", inv)
			}
			// select the second item
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.CursorDown}); !takeover {
				t.Fatal("update during second item exited takeover mode")
			}
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Select}); !takeover {
				t.Fatal("update after second item exited takeover mode")
			}
			if inv := f.Provider.Satisfied(); inv != "" {
				t.Errorf("provider is not satisfied despite 3 items being selected. Reason: %s", inv)
			}
			// deselect the third item
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.CursorDown}); !takeover {
				t.Fatal("update during third item exited takeover mode")
			}
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Select}); !takeover {
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
		})
		t.Run("invoke to exit; ensure we can reenter", func(t *testing.T) {
			// exit the list
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Invoke}); takeover {
				t.Fatal("invoke hotkey did not trigger an exit")
			}
			// ensure view agrees we have left takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind == scaffoldcreate.Takeover {
				t.Error("view believes it is still in takeover mode")
			}
			// reenter the list
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Select}); !takeover {
				t.Fatal("failed to reenter takeover mode")
			}
			// ensure view agrees we are in takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind != scaffoldcreate.Takeover {
				t.Error("view did not believe it was in takeover mode")
			}
			// move around a bit
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.CursorUp}); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.CursorUp}); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.CursorDown}); !takeover {
				t.Fatal("incorrect time to leave takeover mode")
			}
			// ensure view agrees we are in takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind != scaffoldcreate.Takeover {
				t.Error("view did not believe it was in takeover mode")
			}
			// exit the list
			if _, takeover := f.Provider.Update(true, tea.KeyMsg{Type: hotkeys.Invoke}); takeover {
				t.Fatal("invoke hotkey did not trigger an exit")
			}
			// ensure view agrees we have left takeover mode
			if kind, _, _ := f.Provider.View(false, 80); kind == scaffoldcreate.Takeover {
				t.Error("view believes it is still in takeover mode")
			}
		})
	})

}

type testItem struct {
	title       string
	description string
}

func (t testItem) Title() string {
	return t.title
}
func (t testItem) Description() string {
	return t.description
}
func (t testItem) FilterValue() string {
	return t.title
}
