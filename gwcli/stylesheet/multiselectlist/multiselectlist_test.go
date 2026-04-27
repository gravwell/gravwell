//go:build ci

package multiselectlist_test

import (
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/hotkeys"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet/multiselectlist"
)

type testItem struct {
	title       string
	description string
}

func (ti testItem) Title() string {
	return ti.title
}

func (ti testItem) Description() string {
	return ti.description
}

func (ti testItem) FilterValue() string {
	return ti.title
}

func TestPreSelection(t *testing.T) {

	var items = []list.DefaultItem{
		testItem{title: "0", description: "desc0"},
		testItem{title: "1", description: "desc1"},
		testItem{title: "2", description: "desc2"},
		testItem{title: "3", description: "desc3"},
	}

	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{
				Preselected: map[uint]bool{
					1: true,
					2: false,
					3: true,
				},
			})
		want := []list.DefaultItem{
			testItem{title: "1", description: "desc1"},
			testItem{title: "3", description: "desc3"},
		}

		selected := msl.GetSelectedItems()
		if !slices.Equal(selected, want) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
	t.Run("no pre-selections", func(t *testing.T) {
		msl := multiselectlist.New(items, 80, 50, multiselectlist.Options{})
		want := []list.DefaultItem{}

		selected := msl.GetSelectedItems()
		if !slices.Equal(selected, want) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

func TestSelectCurrentItem(t *testing.T) {
	var items = []list.DefaultItem{
		testItem{title: "0", description: "desc0"},
		testItem{title: "1", description: "desc1"},
		testItem{title: "2", description: "desc2"},
		testItem{title: "3", description: "desc3"},
	}

	t.Run("pre-select items 1 and 3", func(t *testing.T) {

		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{Preselected: map[uint]bool{
				1: true,
				2: false,
				3: true,
			},
			})
		if msl.Cursor() != 0 {
			t.Error("cursor is not index 0 at start! Cursor: ", msl.Cursor())
		}
		msl.ToggleCurrentItem()

		want := []list.DefaultItem{
			testItem{title: "0", description: "desc0"},
			testItem{title: "1", description: "desc1"},
			testItem{title: "3", description: "desc3"},
		}

		selected := msl.GetSelectedItems()
		if !slices.Equal(selected, want) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

// TestModel runs a few key commands through Update and View to check that interactivity works.
func TestModel(t *testing.T) {
	var items = []list.DefaultItem{
		testItem{title: "0", description: "desc0"},
		testItem{title: "1", description: "desc1"},
		testItem{title: "2", description: "desc2"},
		testItem{title: "3", description: "desc3"},
	}
	// NOTE(rlandau): view wants can probably be replaced by teatest for cleaner interaction,
	// but I haven't had much luck with teatest.
	msl := multiselectlist.New(items, 30, 20,
		multiselectlist.Options{Preselected: map[uint]bool{
			2: false,
			3: true,
		},
		})
	t.Run("initial view", func(t *testing.T) {
		want := `   List                                                   
                                                          
  4 items                                                 
                                                          
│ [ ] 0                                                   
│ desc0                                                   
                                                          
  [ ] 1                                                   
  desc1                                                   
                                                          
  [ ] 2                                                   
  desc2                                                   
                                                          
  [✓] 3                                                   
  desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("toggle first and last items", func(t *testing.T) {
		msl, _ = msl.Update(tea.KeyMsg{Type: hotkeys.Select})
		// Reminder: lists do not natively support wrapping!
		msl.CursorDown()
		msl, _ = msl.Update(tea.KeyMsg{Type: hotkeys.CursorDown}) // should have the same result as .CursorDown()
		msl.CursorDown()
		msl.ToggleCurrentItem()
		want := `   List                                                   
                                                          
  4 items                                                 
                                                          
  [✓] 0                                                   
  desc0                                                   
                                                          
  [ ] 1                                                   
  desc1                                                   
                                                          
  [ ] 2                                                   
  desc2                                                   
                                                          
│ [ ] 3                                                   
│ desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("done", func(t *testing.T) {
		msl, _ = msl.Update(tea.KeyMsg{Type: hotkeys.Invoke})
		want := `   List                                                   
                                                          
  4 items                                                 
                                                          
  [✓] 0                                                   
  desc0                                                   
                                                          
  [ ] 1                                                   
  desc1                                                   
                                                          
  [ ] 2                                                   
  desc2                                                   
                                                          
│ [ ] 3                                                   
│ desc3                                                   
                                                          
                                                          
                                                          
                                                          
  ↑ cursor up • ↓ cursor down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Error("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		if !msl.Done() {
			t.Error("expected msl to be done after sending Enter.")
		}
	})
}
