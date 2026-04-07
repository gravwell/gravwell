package scaffoldcreate_test

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold/scaffoldcreate"
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
