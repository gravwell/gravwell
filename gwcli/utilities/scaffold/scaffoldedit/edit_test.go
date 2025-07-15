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
)

type val struct {
	title string
	value string
}

// Just spawns a basic edit model to ensure we can and that fields are set properly.
func TestNewEditAction(t *testing.T) {
	if err := clilog.Init(path.Join(t.TempDir(), "dev.log"), "debug"); err != nil {
		t.Fatal(err)
	}

	var allItems = map[int]val{ // set of items we want to manipulate
		0: {title: "zero", value: "z"},
		5: {title: "five", value: "f"},
	}

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
				return data.title, nil
			},
		},
	)
	em, ok := pair.Model.(*editModel[int, val])
	if !ok {
		t.Fatal("failed to type assert result to edit model")
	}
	inv, _, err := em.SetArgs(nil, []string{})
	if err != nil {
		t.Fatal(err)
	} else if inv != "" {
		t.Fatal(inv)
	}
	em.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	time.Sleep(50 * time.Millisecond)
	// enter edit mode for whichever item was listed first, don't care
	em.Update(tea.KeyMsg{Type: tea.KeyEnter})
	time.Sleep(50 * time.Millisecond)

	// check that we are actually in edit mode
	if em.mode != editing {
		t.Fatal("did not enter edit mode despite enter key")
	}
	// sanity check edit mode
	if em.editing.hovered != 0 {
		t.Error("the first line should be hovered on first entry. Found ", em.editing.hovered)
	} else if em.editing.err != "" {
		t.Error(em.editing.err)
	} else if em.editing.longestWidth < 10 { // arbitrarily small amount
		t.Errorf("longest width is too small (%v) given window width.", em.editing.longestWidth)
	}
}
