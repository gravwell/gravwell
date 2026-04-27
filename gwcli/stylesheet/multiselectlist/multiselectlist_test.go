package multiselectlist_test

import (
	"slices"
	"testing"

	"github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
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
	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "0", Desc: "desc0", Identifier: 0},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "1", Desc: "desc1", Slctd: true, Identifier: 1},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "2", Desc: "desc2", Identifier: 2},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "3", Desc: "desc3", Slctd: true, Identifier: 3},
		}

		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{})
		want := []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "1", Desc: "desc1", Slctd: true, Identifier: 1},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "3", Desc: "desc3", Slctd: true, Identifier: 3},
		}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
	t.Run("no pre-selections", func(t *testing.T) {
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "0", Desc: "desc0", Identifier: 0},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "1", Desc: "desc1", Identifier: 1},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "2", Desc: "desc2", Identifier: 2},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "3", Desc: "desc3", Identifier: 3},
		}

		msl := multiselectlist.New(items, 80, 50, multiselectlist.Options{})
		want := []multiselectlist.SelectableItem[int]{}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

func TestSelectCurrentItem(t *testing.T) {
	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		// pre-select items 1 and 3, then toggle item zero to selected manually
		var items = []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "0", Desc: "desc0", Identifier: 0},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "1", Desc: "desc1", Slctd: true, Identifier: 1},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "2", Desc: "desc2", Identifier: 2},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "3", Desc: "desc3", Slctd: true, Identifier: 3},
		}
		msl := multiselectlist.New(items, 80, 50,
			multiselectlist.Options{})
		if msl.Cursor() != 0 {
			t.Error("cursor is not index 0 at start! Cursor: ", msl.Cursor())
		}
		msl.ToggleCurrentItem()

		want := []multiselectlist.SelectableItem[int]{
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "0", Desc: "desc0", Identifier: 0},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "1", Desc: "desc1", Slctd: true, Identifier: 1},
			&multiselectlist.DefaultSelectableItem[int]{Ttl: "3", Desc: "desc3", Slctd: true, Identifier: 3},
		}

		selected := msl.GetSelectedItems()
		if !slices.EqualFunc(selected, want, func(a, b multiselectlist.SelectableItem[int]) bool {
			return a.ID() == b.ID()
		}) {
			t.Fatal("incorrect selected items", testsupport.ExpectedActual(want, selected))
		}
	})
}

// TestModel runs a few key commands through Update and View to check that interactivity works.
/*func TestModel(t *testing.T) {
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




  ↑/k up • ↓/j down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
	})
	t.Run("toggle first and last items", func(t *testing.T) {
		msl, _ = msl.Update(tea.KeyMsg{Type: tea.KeySpace})
		// Reminder: lists do not natively support wrapping!
		msl.CursorDown()
		msl, _ = msl.Update(tea.KeyMsg{Type: tea.KeyDown}) // should have the same result as .CursorDown()
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




  ↑/k up • ↓/j down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Fatal("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		if numSel := len(msl.GetSelectedItems()); numSel != 1 {
			t.Error("incorrect number of items selected.", testsupport.ExpectedActual(1, numSel))
		}
	})
	t.Run("done", func(t *testing.T) {
		msl, _ = msl.Update(tea.KeyMsg{Type: tea.KeyEnter})
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




  ↑/k up • ↓/j down • / filter • q quit • ? more
  space select • ↲ continue`
		if v := msl.View(); v != want {
			t.Error("incorrect view", testsupport.ExpectedActual(testsupport.Uncloak(want), testsupport.Uncloak(v)))
		}
		if !msl.Done() {
			t.Error("expected msl to be done after sending Enter.")
		}
	})
}
*/
