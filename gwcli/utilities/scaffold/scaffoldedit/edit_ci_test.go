/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
)

type val struct {
	title string
	value string
}

// E2E testing for a dummy edit action.
// Runs the dummy action twice to ensure Reset and SetArgs work back to back.
//
// Does not utilize teatest as editModel is not a full tea.Model.
// Thus, input and output are handled manually and some features (update's return, and the entirely of .View()) are ignored.
func Test_Full(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}

	var allItems = map[int]val{ // set of items we want to manipulate
		0: {title: "zero", value: "z"},
		5: {title: "five", value: "f"},
	}

	var updateCalled bool
	pair := NewEditAction("bauble", "baubles",
		Config{
			"value": &Field{
				Required: false,
				Title:    "Value",
				Usage:    "sets the value of the item",
				Order:    10,
			},
			// title is not directly editable
		},
		SubroutineSet[int, val]{
			SelectSub: func(id int) (item val, err error) {
				return allItems[id], nil
			},
			FetchSub: func() (items []val, err error) {
				return slices.Collect(maps.Values(allItems)), nil
			},
			GetFieldSub: func(item val, fieldKey string) (value string, err error) {
				fk := strings.ToLower(fieldKey)
				switch fk {
				case "title":
					return item.title, nil
				case "value":
					return item.value, nil
				}
				return "", fmt.Errorf("unknown field %v", fieldKey)
			},
			SetFieldSub: func(item *val, fieldKey, val string) (invalid string, err error) {
				fk := strings.ToLower(fieldKey)

				if val == "" {
					return "value cannot be empty", nil
				}

				switch fk {
				case "title":
					item.title = val
				case "value":
					item.value = val
				default:
					return "", fmt.Errorf("unknown field %v", fieldKey)
				}
				return "", nil
			},
			GetTitleSub: func(item val) string {
				// this is why the title is typically embedded in the struct
				for k, v := range allItems {
					if v == item {
						return strconv.Itoa(k)
					}
				}
				return "unknown"
			},
			GetDescriptionSub: func(item val) string {
				return fmt.Sprintf("%v -> %v", item.title, item.value)
			},
			UpdateSub: func(data *val) (identifier string, err error) {
				// we don't actually have updates we need to make
				updateCalled = true
				return data.title, nil
			},
		},
	)
	em, ok := pair.Model.(*editModel[int, val])
	if !ok {
		t.Fatal("failed to type assert result to edit model")
	}

	fauxMother(t, em, &updateCalled, -1)
	fauxMother(t, em, &updateCalled, 5)
	fauxMother(t, em, &updateCalled, -1)
}

// helper function to allow the action to be run back-by-back.
//
// if id is NOT set to -1, it will be passed as an argument and tested that we jumped directly to edit mode (if valid).
func fauxMother(t *testing.T, em *editModel[int, val], updateCalled *bool, id int) {
	var args []string
	if id != -1 {
		args = append(args, fmt.Sprintf("--id=%d", id))
	}

	inv, _, err := em.SetArgs(nil, args)
	if err != nil {
		t.Fatal(err)
	} else if inv != "" {
		t.Fatal(inv)
	}
	em.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	time.Sleep(50 * time.Millisecond)

	// if id was specified, we should have jumped directly to edit mode
	if id == -1 {
		// enter edit mode for whichever item was listed first, don't care
		em.Update(tea.KeyMsg{Type: tea.KeyEnter})
		time.Sleep(50 * time.Millisecond)
	}

	// check that we are actually in edit mode
	if em.mode != editing {
		t.Fatal("incorrect mode", ExpectedActual(editing, em.mode))
	}
	// sanity check edit mode
	if em.editing.hovered != 0 {
		t.Error("the first line should be hovered on first entry. Found ", em.editing.hovered)
	} else if em.editing.err != "" {
		t.Error(em.editing.err)
	} else if em.editing.longestWidth < 10 { // arbitrarily small amount
		t.Errorf("longest width is too small (%v) given window width.", em.editing.longestWidth)
	}

	// check the value of the TI

	// make sure we can nav up to cycle to the submit button
	em.Update(tea.KeyMsg{Type: tea.KeyUp})
	time.Sleep(50 * time.Millisecond)

	if !em.editing.submitHovered() {
		t.Fatal("keyUp on first field did not hover submit.",
			ExpectedActual(uint(em.editing.tiCount), em.editing.hovered))
	}
	// return to top
	em.Update(tea.KeyMsg{Type: tea.KeyDown})
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < len(em.cfg); i++ { // we should one TI for each field
		// nav through each to the submit
		em.Update(tea.KeyMsg{Type: tea.KeyDown})
		time.Sleep(50 * time.Millisecond)
	}

	if !em.editing.submitHovered() {
		t.Fatal("traversing down the list of TIs did not hover submit.",
			ExpectedActual(uint(em.editing.tiCount), em.editing.hovered))
	}

	// test the update procedure
	em.Update(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(50 * time.Millisecond)

	if !(*updateCalled) {
		t.Fatal("the update subroutine was not triggered")
	} else if !em.Done() {
		t.Fatal("triggering the update function did not mark the action as done")
	}
	if err := em.Reset(); err != nil {
		t.Fatal(err)
	}
}

// TODO test runNonInteractive
