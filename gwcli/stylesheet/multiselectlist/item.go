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
	Ttl        string
	Desc       string
	Slctd      bool
	Identifier ID_t
}

// FilterValue filters on the concat of ttl and desc.
func (i DefaultSelectableItem[ID_t]) FilterValue() string {
	return i.Ttl + i.Desc
}

func (i DefaultSelectableItem[ID_t]) Title() string {
	return i.Ttl
}

func (i DefaultSelectableItem[ID_t]) ID() ID_t {
	return i.Identifier
}

func (i DefaultSelectableItem[ID_t]) Description() string {
	return i.Desc
}

func (i *DefaultSelectableItem[ID_t]) SetSelected(selected bool) {
	i.Slctd = selected
}

func (i DefaultSelectableItem[ID_t]) Selected() bool {
	return i.Slctd
}
