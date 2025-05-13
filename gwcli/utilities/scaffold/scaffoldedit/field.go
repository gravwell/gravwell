/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldedit

import "github.com/charmbracelet/bubbles/textinput"

// The full set of configuration available to and required from the implementor
type Config = map[string]*Field

// Represents a single field within the struct that is available to edit.
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
