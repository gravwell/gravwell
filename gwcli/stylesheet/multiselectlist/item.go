/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package multiselectlist

//#region Item Wrappers

type SelectableItem[ID_t comparable] interface {
	// FilterValue is the value we use when filtering against this item when
	// we're filtering the list.
	FilterValue() string
	Title() string
	// unique identifier for this item
	ID() ID_t
	Description() string
	// Toggle the selection state of this item.
	SetSelected(selected bool)
	// Is this item currently selected?
	Selected() bool
}

var _ SelectableItem[any] = &DefaultSelectableItem[any]{}

// DefaultSelectableItem provides a struct for use in the SelectableItem interface for users that do not wish to customize at all.
type DefaultSelectableItem[ID_t any] struct {
	Title_       string
	Description_ string
	Selected_    bool
	ID_          ID_t
}

// FilterValue filters on the concat of ttl and desc.
func (i DefaultSelectableItem[ID_t]) FilterValue() string {
	return i.Title_ + i.Description_
}

func (i DefaultSelectableItem[ID_t]) Title() string {
	return i.Title_
}

func (i DefaultSelectableItem[ID_t]) ID() ID_t {
	return i.ID_
}

func (i DefaultSelectableItem[ID_t]) Description() string {
	return i.Description_
}

func (i *DefaultSelectableItem[ID_t]) SetSelected(selected bool) {
	i.Selected_ = selected
}

func (i DefaultSelectableItem[ID_t]) Selected() bool {
	return i.Selected_
}
