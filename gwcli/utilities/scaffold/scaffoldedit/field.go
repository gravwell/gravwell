/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
)

// Config is the full set of configuration available to and required from the implementor.
// It maps each field struct to a unique key.
type Config = map[string]*Field

// Field represents a single field that is available to edit.
// Field structs contain all data required for the field to be user-editable as well as optional parameters for
// tweaking its appearance or usability.
type Field struct {
	Required      bool   // is this field required to be populated?
	Title         string // field name displayed next to prompt and as flag name
	Usage         string // OPTIONAL. Flag usage displayed via -h
	FlagName      string // OPTIONAL. Defaults to DeriveFlagName() result.
	FlagShorthand rune   // OPTIONAL. '-x' form of FlagName.
	Order         int    // OPTIONAL. Top-Down (highest to lowest) display order of this field.

	// OPTIONAL.
	// Called once, at program start to generate a TI instead of using a generalize newTI()
	CustomTIFuncInit func() textinput.Model
}

// FieldName returns a struct suited for Name inputs.
// Order == 100.
func FieldName(singular string) Field {
	return Field{
		Required:      true,
		Title:         "name",
		Usage:         ft.Name.Usage(singular),
		FlagName:      ft.Name.Name(),
		FlagShorthand: rune(ft.Name.Shorthand()[0]),
		Order:         100,
	}
}

// FieldDescription returns a struct suited for Description inputs.
// Order == 90.
func FieldDescription(singular string) Field {
	return Field{
		Required:      false,
		Title:         "description",
		Usage:         ft.Description.Usage(singular),
		FlagName:      ft.Description.Name(),
		FlagShorthand: rune(ft.Description.Shorthand()[0]),
		Order:         90,
	}
}

// FieldLabels returns a struct suited for taking in labels as "<1>,<2>,<3>".
// Order == 70.
func FieldLabels() Field {
	return Field{
		Required: false,
		Title:    "Labels",
		Usage:    "comma-separated list of labels to apply",
		FlagName: "labels",
		Order:    70,
		CustomTIFuncInit: func() textinput.Model {
			ti := stylesheet.NewTI("", true)
			ti.Placeholder = "label1,label2,label3,..."
			return ti
		},
	}
}
