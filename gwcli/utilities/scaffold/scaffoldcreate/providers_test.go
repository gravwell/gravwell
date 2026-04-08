package scaffoldcreate_test

import (
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
		invalid, onStart, err := pair.Model.SetArgs(&pflag.FlagSet{}, nil, 0, 0)
		if invalid != "" || onStart != nil || err != nil {
			t.Fatal("bad SetArgs results."+
				"\nInvalid:", testsupport.ExpectedActual("", invalid),
				"\nonStart:", testsupport.ExpectedActual(nil, onStart),
				"\nerr:", testsupport.ExpectedActual(nil, err),
			)
		}
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
		pair.Model.Update(tea.KeyMsg{Type: hotkeys.Interact})

		if create != "ab" { // create should have been set as part of submission
			t.Fatal("bad value from createFunc", testsupport.ExpectedActual("ab", create))
		}
		pair.Model.Reset()
		if x := provider.Get(); x != "" {
			t.Fatal("provider value not destroyed by Reset. Lingering value: ", x)
		}
		if invalid, onStart, err := pair.Model.SetArgs(&pflag.FlagSet{}, []string{"-t=YungVenuz"}, 0, 0); invalid != "" ||
			onStart != nil ||
			err != nil {
			t.Fatal("bad SetArgs results."+
				"\nInvalid:", testsupport.ExpectedActual("", invalid),
				"\nonStart:", testsupport.ExpectedActual(nil, onStart),
				"\nerr:", testsupport.ExpectedActual(nil, err),
			)
		}
		if x := provider.Get(); x != "Yun" { // should be limited by our CharLimit
			t.Fatal("bad value after second SetArgs", testsupport.ExpectedActual("Yun", x))
		}

	})
}

func TestMSLProvider(t *testing.T) {
	items := []list.DefaultItem{
		testItem{"ttl1", "desc1"},
		testItem{"ttl2", "desc2"},
	}

	var f scaffoldcreate.Field
	{
		baseProvider := scaffoldcreate.NewMSLProvider(items,
			multiselectlist.Options{Preselected: map[uint]bool{0: true}})
		baseProvider.RequireAtLeast = 2
		f = scaffoldcreate.NewField("msl", true, baseProvider)
	}

	f.Provider.Initialize(f.DefaultValue, f.Required)

	selected := f.Provider.Get()
	if selected != "ttl1" {
		t.Fatal()
	}
	// with only a single item selected, the field should be unsatisfied
	if invalid := f.Provider.Satisfied(); invalid == "" {
		t.Fatal("Provider should be unsatisfied due to only having a single item selected")
	}
	// try to set takeover mode

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
