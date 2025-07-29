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

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	. "github.com/gravwell/gravwell/v4/gwcli/internal/testsupport"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
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

	inv, _, err := em.SetArgs(nil, args, 80, 50)
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

func TestNonInteractive(t *testing.T) {
	pair, items, _, sbErr := generateTestPair()

	pair.Action.SetArgs([]string{"--" + ft.NoInteractive.Name(), "--id=eb8b5cb2-7cb6-4586-a2d6-665e662ad976", "--note=\"baby girl\""})
	if err := pair.Action.Execute(); err != nil {
		t.Fatal(err)
	}
	// check outputs
	//out := strings.TrimSpace(sbOut.String())
	outErr := strings.TrimSpace(sbErr.String())
	if outErr != "" {
		t.Fatal(outErr)
	}
	// check that the map was actually updated
	if items[uuid.MustParse("eb8b5cb2-7cb6-4586-a2d6-665e662ad976")].note == "" {
		t.Fatal("expected a note to be set on Bee")
	}
	if items[uuid.MustParse("65d7e5a3-9be4-43e0-9fce-887052753661")].color != "" {
		t.Fatal("did not expect a color to be set on Mozzie")
	}
}

type cat struct {
	name      string
	color     string
	furLength string
	note      string
}

// generateTestPair builds an edit action pair, performing all required setup.
// This includes generating the data for it to operate on and redirecting stdout/stderr.
// Returns the pair, the data, and both pipes.
func generateTestPair() (pair action.Pair, data map[uuid.UUID]*cat, sbOut, sbErr strings.Builder) {
	// just some random UUIDs paired to garbage values
	var items = map[uuid.UUID]*cat{
		uuid.MustParse("eb8b5cb2-7cb6-4586-a2d6-665e662ad976"): {name: "Bee", color: "tortie"},
		uuid.MustParse("eb8b5cb2-7cb6-4586-a2d6-8f3308fafb52"): {name: "Coco", note: "adventure buddy"},
		uuid.MustParse("65d7e5a3-9be4-43e0-9fce-887052753661"): {name: "Mozzie", note: "little grey girl"},
	}

	pair = NewEditAction("cat", "cats", Config{
		"fur color": &Field{
			Required: true,
			Title:    "Fur Color",
			Usage:    "set the fur color of your feline",
			Order:    80,
			CustomTIFuncInit: func() textinput.Model {
				m := textinput.New()
				m.EchoCharacter = '^'
				m.Width = 50
				return m
			},
		},
		"fur length": &Field{
			Required: false,
			Title:    "Fur Length",
			Usage:    "set the fur length of your feline (hairless, short, medium, long)",
			Order:    100,
		},
		"note": &Field{
			Required: false,
			Title:    "note",
			Usage:    "add a note to the kitty description",
			Order:    20,
		},
	}, SubroutineSet[uuid.UUID, *cat]{
		SelectSub: func(id uuid.UUID) (item *cat, err error) {
			itm, found := items[id]
			if !found {
				return &cat{}, ErrUnknownID(id)
			}
			return itm, nil
		},
		FetchSub: func() (items []*cat, err error) {
			return items, nil
		},
		GetFieldSub: func(i *cat, fieldKey string) (value string, err error) {
			fieldKey = strings.ToLower(fieldKey)
			switch fieldKey {
			case "title":
				return i.name, nil
			case "fur color":
				return i.color, nil
			case "fur length":
				return i.furLength, nil
			case "note":
				return i.note, nil
			}
			return "", ErrUnknownField(fieldKey)
		},
		SetFieldSub: func(i **cat, fieldKey, val string) (invalid string, err error) {
			if val == "" {
				return "cannot set an empty value", nil
			}
			fieldKey = strings.ToLower(fieldKey)
			switch fieldKey {
			case "title":
				(*i).name = val
			case "fur color":
				(*i).color = val
			case "fur length":
				(*i).furLength = val
			case "note":
				(*i).note = val
			default:
				return "", ErrUnknownField(fieldKey)
			}
			return "", nil
		},
		GetTitleSub: func(i *cat) string {
			return i.name
		},
		GetDescriptionSub: func(i *cat) string {
			return "some description"
		},
		UpdateSub: func(data **cat) (identifier string, err error) {
			// nothing to be done in testing
			return (*data).name, nil
		},
	})
	// bolt on script flag
	pair.Action.Flags().Bool(ft.NoInteractive.Name(), false, "???")
	// capture output
	pair.Action.SetOut(&sbOut)
	pair.Action.SetErr(&sbErr)
	return pair, items, sbOut, sbErr
}
