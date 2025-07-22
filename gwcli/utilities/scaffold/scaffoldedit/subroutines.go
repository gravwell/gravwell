/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit

import "github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

// SelectSubroutine defines the subrotuine used to pull a specific (selected), edit-able struct when skipping list/selecting mode.
type SelectSubroutine[I scaffold.Id_t, S any] func(id I) (
	item S, err error,
)

// FetchAllSubroutine defines a subroutine that returns all edit-able data (in the form of their structs). Not used in script mode.
type FetchAllSubroutine[S any] func() (
	items []S, err error,
)

// GetFieldSubroutine defines the function to retrieve the struct value associated to the field key without reflection.
// This is probably a switch statement that maps (key -> item.X).
//
// Sister to setFieldFunction.
type GetFieldSubroutine[S any] func(item S, fieldKey string) (
	value string, err error,
)

// GetTitleSubroutine defines the subroutine to fetch a title to be displayed for this item in the list.
// This will be called in a loop when building the list.
type GetTitleSubroutine[S any] func(item S) string

// GetDescriptionSubroutine defines the subroutine to fetch a description to be displayed under this item in the list.
// This will be called in a loop when building the list.
type GetDescriptionSubroutine[S any] func(item S) string

// SetFieldSubroutine defines the function to set the struct value associated to the field key without reflection.
// This is probably a switch statement that maps (key -> item.X).
// Returns invalid if the value is invalid for the keyed field and err on an unrecoverable error.
//
// Sister to getFieldFunction.
type SetFieldSubroutine[S any] func(item *S, fieldKey, val string) (
	invalid string, err error,
)

// UpdateStructSubroutine defines the function that performs the actual update of the data on the GW instance.
// The error returned by this subroutine does not kill the action, instead displaying to the user.
// In that way, it is closer to an invalid, even though validation errors are assumed to be caught by the SetField sub.
type UpdateStructSubroutine[S any] func(data *S) (
	identifier string, err error,
)

// SubroutineSet defines the set of all subroutines required by an implementation of an edit action.
//
// ! AddEditAction will panic if any subroutine is nil
type SubroutineSet[I scaffold.Id_t, S any] struct {
	SelectSub SelectSubroutine[I, S] // fetch a specific editable struct
	// used in interactive mode to fetch all editable structs
	FetchSub    FetchAllSubroutine[S]
	GetFieldSub GetFieldSubroutine[S] // get a value within the struct
	SetFieldSub SetFieldSubroutine[S] // set a value within the struct
	// special get function to retrieve a title for the list entry
	GetTitleSub GetTitleSubroutine[S]
	// special function to retrieve a description for the list entry
	GetDescriptionSub GetDescriptionSubroutine[S]
	UpdateSub         UpdateStructSubroutine[S] // submit the struct as updated
}

// Validates that all functions were set.
// Panics if any are missing.
func (funcs *SubroutineSet[I, S]) guarantee() {
	if funcs.SelectSub == nil {
		panic("select function is required")
	}
	if funcs.FetchSub == nil {
		panic("fetch all function is required")
	}
	if funcs.GetFieldSub == nil {
		panic("get field function is required")
	}
	if funcs.SetFieldSub == nil {
		panic("set field function is required")
	}
	if funcs.GetTitleSub == nil {
		panic("get title function is required")
	}
	if funcs.GetDescriptionSub == nil {
		panic("get description function is required")
	}
	if funcs.UpdateSub == nil {
		panic("update struct function is required")
	}
}
