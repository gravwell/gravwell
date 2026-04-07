/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package scaffoldcreate

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/uniques"

	"github.com/spf13/pflag"
)

// FlagConfig defines settings for customizing how the flag for this field is displayed and handled.
// All flag configuration is optional.
type FlagConfig struct {
	Name      string // Longform flag (ex: --flagname).
	Usage     string // Description displayed with -h.
	Shorthand rune   // Shortform flag (ex: -f). Omitted if unset.
}

// A Field defines a single data point that will be passed to the create function.
type Field struct {
	// user-facing identifier of this field.
	Title        string
	Required     bool       // this field must be populated prior to calling createFunc
	Flag         FlagConfig // OPTIONAL. Control how this field's flag is handled.
	DefaultValue string     // OPTIONAL. Default flag and TI value
	Order        int        // OPTIONAL. Top-Down (highest to lowest) display order of this field.

	Provider FieldProvider
}

// NewField composes a Field from the required parameters.
func NewField(title string, required bool, provider FieldProvider) Field {
	return Field{Title: title, Required: required, Provider: provider}
}

// Returns a FlagSet built from the given fields.
//
// If Flag.Name is empty, the entry will be skipped.
//
// All flags are read as strings (subject to change).
func installFlagsFromFields(fields Config) pflag.FlagSet {
	var flags pflag.FlagSet
	for key, f := range fields {
		if f.Flag.Name == "" {
			f.Flag.Name = ft.DeriveFlagName(f.Title) // sanitize
		} else {
			f.Flag.Name = ft.DeriveFlagName(f.Flag.Name) // sanitize
		}

		fields[key] = f

		// install flag
		if f.Flag.Shorthand != 0 {
			flags.StringP(f.Flag.Name, string(f.Flag.Shorthand), f.DefaultValue, f.Flag.Usage)
		} else {
			flags.String(
				f.Flag.Name,
				f.DefaultValue, // default flag value
				f.Flag.Usage)
		}
	}

	return flags
}

// Attempts to set flag values into their respective fields.
// Returns a list of required fields that did not recieve values and the first Set error that occurred (if one did).
func setValuesFromFlags(fs *pflag.FlagSet, fields Config) (missingRequireds []string, err error) {
	if !fs.Parsed() {
		clilog.Writer.Errorf("attempted to set values from unparsed flagset")
		return nil, uniques.ErrGeneric
	}
	for key := range fields {
		flagName := fields[key].Flag.Name
		// if this value is required, but unset, add it to the list and move on.
		// NOTE(rlandau): this uses fs.Changed(), which will fail default values.
		// I am assuming that if you need a value, a default is irrelevant.
		if fields[key].Required && !fs.Changed(flagName) {
			missingRequireds = append(missingRequireds, fields[key].Flag.Name)
			continue
		}

		v, err := fs.GetString(flagName)
		if err != nil {
			return nil, err
		}
		if invalid := fields[key].Provider.Set(v); invalid != "" {
			return nil, fmt.Errorf("%s is not a valid input to --%s: %s", v, fields[key].Flag.Name, invalid)
		}
	}
	return missingRequireds, nil
}

// FieldName returns a struct suited for Name inputs.
// Order == 100.
func FieldName(singular string) Field {
	return Field{
		Title:    ft.Name.Name(),
		Required: true,
		Flag: FlagConfig{
			Name:      ft.Name.Name(),
			Usage:     ft.Name.Usage(singular),
			Shorthand: rune(ft.Name.Shorthand()[0]),
		},
		Order:    100,
		Provider: &TextProvider{},
	}
}

// FieldDescription returns a struct suited for Description inputs.
// Order == 90.
func FieldDescription(singular string) Field {
	return Field{
		Title:    ft.Description.Name(),
		Required: false,
		Flag: FlagConfig{
			Name:      ft.Description.Name(),
			Usage:     ft.Description.Usage(singular),
			Shorthand: rune(ft.Description.Shorthand()[0]),
		},
		Order:    90,
		Provider: &TextProvider{},
	}
}

// FieldPath returns a struct suited for file path specification inputs.
// Order == 80.
func FieldPath(singular string) Field {
	return Field{
		Title:    ft.Path.Name(),
		Required: true,
		Flag: FlagConfig{
			Name:      ft.Path.Name(),
			Usage:     ft.Path.Usage(singular),
			Shorthand: rune(ft.Path.Shorthand()[0]),
		},
		Order:    80,
		Provider: &TextProvider{},
	}
}

// FieldLabels returns a struct suited for taking in labels as "<1>,<2>,<3>".
// Order == 70.
func FieldLabels() Field {
	return Field{
		Title:    "Labels",
		Required: false,
		Flag: FlagConfig{
			Name:  "labels",
			Usage: "comma-separated list of labels to apply",
		},
		Order: 70,
		Provider: &TextProvider{
			CustomInit: func() textinput.Model {
				ti := stylesheet.NewTI("", true)
				ti.Placeholder = "label1,label2,label3,..."
				return ti
			},
		},
	}
}

// FieldFrequency returns a struct suitable for taking in the frequency of something occurring as a cron string.
// Attaches uniques.CronRuneValidator and shorthand -c.
// Order == 50.
func FieldFrequency() Field {
	return Field{
		Title:    "Frequency",
		Required: true,
		Flag: FlagConfig{
			Name:      ft.Frequency.Name(),
			Usage:     ft.Frequency.Usage(),
			Shorthand: rune(ft.Frequency.Shorthand()[0]),
		},
		Order: 50,
		Provider: &TextProvider{
			CustomInit: func() textinput.Model {
				ti := stylesheet.NewTI("", false)
				ti.Placeholder = "* * * * *"
				ti.Validate = uniques.CronRuneValidator
				return ti
			},
		},
	}
}
