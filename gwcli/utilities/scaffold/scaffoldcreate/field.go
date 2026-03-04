/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"

	"github.com/spf13/pflag"
)

// FieldType (though currently not utilized) is intended as an expandable way to add new data inputs,
// such as checkboxes or radio buttons. It alters the draw in .View and how data is parsed from the
// Field's flag.
type FieldType = string

const (
	Text FieldType = "text" // string inputs, consumed via flag.String & textinput.Model
	File FieldType = "file" // takes a path as a string. In interactive mode, spins up a filepicker.
	// TODO add boolean, add label
)

// A Field defines a single data point that will be passed to the create function.
type Field struct {
	Required      bool      // this field must be populated prior to calling createFunc
	Title         string    // field name displayed next to prompt and as flag name
	Usage         string    // OPTIONAL. Flag usage displayed via -h
	Type          FieldType // type of field, dictating how it is presented to the user
	FlagName      string    // OPTIONAL. Defaults to DeriveFlagName() result.
	FlagShorthand rune      // OPTIONAL. '-x' form of FlagName.
	DefaultValue  string    // OPTIONAL. Default flag and TI value
	Order         int       // OPTIONAL. Top-Down (highest to lowest) display order of this field.

	// OPTIONAL. USED ONLY FOR TEXT TYPE.
	// Called once, at program start to generate a TI instead of using a generalize newTI().
	// Can be used to add a ValidateFunc to the TI.
	CustomTIFuncInit func() textinput.Model
	// OPTIONAL. USED ONLY FOR TEXT TYPE.
	// Called every SetArg() (prior to passing control to the child create action), if not nil.
	// The associated TI will be replaced by the returned Model.
	CustomTIFuncSetArg func(*textinput.Model) textinput.Model
}

// Valid returns why the field is currently invalid (or nil), generally due to missing required fields.
func (f *Field) Valid() error {
	switch {
	case f.Title == "":
		return errors.New("title is required")
	case f.Type == "":
		return errors.New("type is required")
	}

	return nil
}

// Returns a FlagSet built from the given flagmap
func installFlagsFromFields(fields Config) pflag.FlagSet {
	var flags pflag.FlagSet
	for key, f := range fields {
		if f.FlagName == "" {
			f.FlagName = ft.DeriveFlagName(f.Title)
		}

		// map fields to their flags
		switch f.Type {
		case Text, File:
			if f.FlagShorthand != 0 {
				flags.StringP(f.FlagName, string(f.FlagShorthand), f.DefaultValue, f.Usage)
			} else {
				flags.String(
					f.FlagName,
					f.DefaultValue, // default flag value
					f.Usage)
			}
		default:
			clilog.Writer.Error("failed to install flag for field: unknown field type",
				rfc5424.SDParam{Name: "field_key", Value: key},
				rfc5424.SDParam{Name: "unknown type", Value: f.Type},
				scaffold.IdentifyCaller(),
			)
		}
	}

	return flags
}
