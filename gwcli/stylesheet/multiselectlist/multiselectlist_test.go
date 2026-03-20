package multiselectlist_test

import (
	"slices"
	"testing"

	"github.com/charmbracelet/bubbles/list"
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

	var items = []list.DefaultItem{
		testItem{title: "0", description: "desc0"},
		testItem{title: "1", description: "desc1"},
		testItem{title: "2", description: "desc2"},
		testItem{title: "3", description: "desc3"},
	}

	t.Run("pre-select items 1 and 3", func(t *testing.T) {
		preselected := map[uint]bool{
			1: true,
			2: false,
			3: true,
		}
		msl := multiselectlist.New(items, 80, 50, preselected)
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
		msl := multiselectlist.New(items, 80, 50, nil)
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
		preselected := map[uint]bool{
			1: true,
			2: false,
			3: true,
		}
		msl := multiselectlist.New(items, 80, 50, preselected)
		if msl.Cursor() != 0 {
			t.Error("cursor is not index 0 at start! Cursor: ", msl.Cursor())
		}
		msl.SelectCurrentItem()

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
