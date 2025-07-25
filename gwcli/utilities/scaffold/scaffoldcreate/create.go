/*************************************************************************
 * Copyright 2024 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

/*
Package scaffoldcreate provides a template for building actions that create new data or configuration.

A create action creates a shallow list of inputs for the user to fill via flags or interactive
TIs before being passed back to the progenitor to transform into usable data for their create
function.

The available fields are fairly configurable, the progenitor provides their own map of Field
structs, and easily extensible, the struct can have more options or formats bolted on without too
much trouble.

This scaffold is a bit easier to extend than Delete and List, given it did not require generics.

Look to the scheduled query creation action (external to the one built into DataScope) or macro
creation action as two examples of implementation styles.

! Once a Config is given by the caller, it should be considered ReadOnly.

NOTE: More complex creation with nested options and mutli-stage flows should be built
independently. This scaffold is intended for simple, handful-of-field creations.

Example implementation:

	func NewCreateAction() action.Pair {
		n := scaffoldcreate.NewField(true, "name", 100)
		d := scaffoldcreate.NewField(true, "value", 90)
		fields := scaffoldcreate.Config{
			"name":  n,
			"value": d,
			"field3": scaffoldcreate.Field{
				Required:      true,
				Title:         "field3",
				Usage:         "field 3 usage",
				Type:          scaffoldcreate.Text,
				FlagName:      "flagn",
				FlagShorthand: 'f',
				DefaultValue:  "",
				TI: struct {
					Order       int
					Placeholder string
					Validator   func(s string) error
				}{
					Order: 80,
				},
			},
		}

		return scaffoldcreate.NewCreateAction("", fields, create)
	}

	func create(_ scaffoldcreate.Config, vals scaffoldcreate.Values) (any, string, error) {
		id, err := connection.Client.X()
		return id, "", err
	}
*/
package scaffoldcreate

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/scaffold"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/treeutils"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	errMissingRequiredFlags string = "missing required flags %v"
	createdSuccessfully     string = "Successfully created %v (ID: %v)."
	minFieldWidth           uint   = 25
)

// A Config maps keys -> Field; used as (ReadOnly) configuration for this creation instance
type Config = map[string]Field

type Values = map[string]string

// CreateFunc defines the format of the subroutine that must be passed for creating data.
// The function's return values must be:
//
// the id of the newly created value (likely as returned by the Gravwell backend)
//
// a reason the create attempt was invalid (or the empty string)
//
// and an error that occurred (or nil). This is different than an invalid reason and is likely a bubbling up of an error from the client library.
type CreateFunc func(cfg Config, values Values, fs *pflag.FlagSet) (id any, invalid string, err error)

func NewCreateAction(singular string,
	fields Config,
	create CreateFunc,
	addtlFlags func() pflag.FlagSet) action.Pair {
	// nil check singular
	if singular == "" {
		panic("")
	}

	// pull flags from provided fields
	var flags = installFlagsFromFields(fields)
	if addtlFlags != nil {
		afs := addtlFlags()
		flags.AddFlagSet(&afs)
	}

	cmd := treeutils.GenerateAction(
		"create",                 // use
		"create a "+singular,     // short
		"create a new "+singular, // long
		[]string{},               // aliases
		func(c *cobra.Command, s []string) {
			// get standard flags
			script, err := c.Flags().GetBool("script")
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			}
			// get field flags
			var values map[string]string
			if vals, mr, err := getValuesFromFlags(c.Flags(), fields); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if mr != nil {
				if !script {
					if err := mother.Spawn(c.Root(), c, s); err != nil {
						clilog.Writer.Critical(err.Error())
					}
					return
				} else {
					fmt.Fprintf(c.OutOrStdout(), errMissingRequiredFlags+"\n", mr)
				}
				return
			} else {
				values = vals
			}

			// attempt to create the new X
			if id, inv, err := create(fields, values, c.Flags()); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if inv != "" { // some of the flags were invalid
				fmt.Fprintln(c.OutOrStdout(), inv)
				return
			} else {
				fmt.Fprintf(c.OutOrStdout(), "Successfully created %v (ID: %v).", singular, id)
			}
		})

	// attach mined flags to cmd
	cmd.Flags().AddFlagSet(&flags)

	return action.NewPair(cmd, newCreateModel(fields, singular, create, addtlFlags))
}

// Given a parsed flagset and the field configuration, builds a corollary map of field values.
//
// Returns the values for each flag (default if unset), a list of required fields (as their flag
// names) that were not set, and an error (if one occurred).
func getValuesFromFlags(fs *pflag.FlagSet, fields Config) (
	values Values, missingRequireds []string, err error,
) {
	values = make(Values)
	for k, f := range fields {
		switch f.Type {
		case Text:

			flagVal, err := fs.GetString(f.FlagName)
			if err != nil {
				return nil, nil, err
			}
			// if this value is required, but unset, add it to the list
			if f.Required && !fs.Changed(f.FlagName) {
				missingRequireds = append(missingRequireds, f.FlagName)
			}

			values[k] = flagVal
		default:
			panic("developer error: unknown field type: " + f.Type)
		}
	}
	return values, missingRequireds, nil
}

//#region interactive mode (model) implementation

const defaultWidth = 80 // default wrap width, used before initial WinMsgSz arrives

type mode uint // state of the interactive application

const (
	inputting mode = iota // user entering data
	quitting              // done
)

// interactive model that builds out inputs based on the read-only Config supplied on creation.
type createModel struct {
	mode mode

	width int // tty width

	singular string // "macro", "search", etc

	fields Config // RO configuration provided by the caller

	orderedTIs         []scaffold.KeyedTI // Ordered array of map keys, based on Config.TI.Order
	selected           uint               // currently focused ti (in key order index)
	longestFieldLength int                // the longest field name of the TIs

	inputErr  string // the reason inputs are invalid
	createErr string // the reason the last create failed (not for invalid parameters)

	// function to provide additional flags for this specific create instance
	addtlFlagFunc func() pflag.FlagSet
	// current state of the flagset, Reset to addtlFlagFunc + installFlags
	fs pflag.FlagSet
	cf CreateFunc // function to create the new entity
}

func (c *createModel) SubmitSelected() bool {
	return c.selected == uint(len(c.orderedTIs))
}

// Creates and returns a create Model, ready for interactive usage via Mother.
func newCreateModel(fields Config, singular string, cf CreateFunc, addtlFlagFunc func() pflag.FlagSet) *createModel {
	c := &createModel{
		mode:          inputting,
		width:         defaultWidth,
		singular:      singular,
		fields:        fields,
		orderedTIs:    make([]scaffold.KeyedTI, 0),
		addtlFlagFunc: addtlFlagFunc,
		cf:            cf,
	}

	// set flags by mining flags and, if applicable, tacking on additional flags
	c.fs = installFlagsFromFields(fields)
	if c.addtlFlagFunc != nil {
		addtlFlags := c.addtlFlagFunc()
		c.fs.AddFlagSet(&addtlFlags)
	}

	for k, f := range fields {
		// generate the TI
		kti := scaffold.KeyedTI{
			Key:        k,
			FieldTitle: f.Title,
			Required:   f.Required,
		}
		// if a custom func was not given, use the default generation
		if f.CustomTIFuncInit == nil {
			kti.TI = stylesheet.NewTI(f.DefaultValue, !f.Required)
		} else {
			kti.TI = f.CustomTIFuncInit()
		}

		c.orderedTIs = append(c.orderedTIs, kti)

		// note the longest Title for later formatting
		if w := lipgloss.Width(f.Title); c.longestFieldLength < w {
			c.longestFieldLength = w
		}
	}
	// buffer the field length
	if c.longestFieldLength < int(minFieldWidth) {
		c.longestFieldLength = int(minFieldWidth)
	}
	// sort keys from highest order to lowest order
	slices.SortFunc(c.orderedTIs, func(a, b scaffold.KeyedTI) int {
		return fields[b.Key].Order - fields[a.Key].Order
	})

	if len(c.orderedTIs) > 0 {
		c.orderedTIs[0].TI.Focus()
	}

	return c
}

// Init is unused. It just exists so we can feed createModel into teatest.
func (c *createModel) Init() tea.Cmd {
	return nil
}

func (c *createModel) Update(msg tea.Msg) tea.Cmd {
	if c.mode == quitting {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		c.inputErr = ""  // clear last input error
		c.createErr = "" // clear error from last create attempt
		switch keyMsg.Type {
		case tea.KeyUp, tea.KeyShiftTab:
			c.focusPrevious()
			return textinput.Blink
		case tea.KeyDown:
			c.focusNext()
			return textinput.Blink
		case tea.KeyEnter:
			if c.SubmitSelected() {
				// extract values from TIs
				values, mr := c.extractValuesFromTIs()
				if mr != nil {
					if len(mr) == 1 {
						c.inputErr = fmt.Sprintf("%v is required", mr[0])
					} else {
						c.inputErr = fmt.Sprintf("%v are required", mr)
					}
					return nil
				}
				id, invalid, err := c.cf(c.fields, values, &c.fs)
				if err != nil {
					c.createErr = err.Error()
					return nil
				} else if invalid != "" {
					c.inputErr = invalid
					return nil
				}
				// done, die
				c.mode = quitting
				return tea.Println(fmt.Sprintf(createdSuccessfully, c.singular, id))
			} else {
				c.focusNext()
			}
		}
	} else if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		c.width = sizeMsg.Width
		return nil
	}
	if !c.SubmitSelected() {
		// pass message to currently focused ti
		var cmd tea.Cmd
		c.orderedTIs[c.selected].TI, cmd = c.orderedTIs[c.selected].TI.Update(msg)
		return cmd
	}
	return nil
}

// Blurs the current ti, selects and focuses the next (indexically) one.
func (c *createModel) focusNext() {
	if !c.SubmitSelected() {
		c.orderedTIs[c.selected].TI.Blur()
	}
	c.selected += 1
	if c.selected > uint(len(c.orderedTIs)) { // jump to start
		c.selected = 0
	}
	if !c.SubmitSelected() {
		c.orderedTIs[c.selected].TI.Focus()
	}
}

// Blurs the current ti, selects and focuses the previous (indexically) one.
func (c *createModel) focusPrevious() {
	// if we are not on the submit button, then blur
	if !c.SubmitSelected() {
		c.orderedTIs[c.selected].TI.Blur()
	}
	if c.selected == 0 { // wrap to submit button
		c.selected = uint(len(c.orderedTIs))
	} else {
		c.selected -= 1
	}
	// if we are not on the submit button, then focus
	if !c.SubmitSelected() {
		c.orderedTIs[c.selected].TI.Focus()
	}
}

// Generates the corrollary value map from the TIs.
//
// Returns the values for each TI (mapped to their Config key), a list of required fields (as their
// field.Title names) that were not set, and an error (if one occurred).
func (c *createModel) extractValuesFromTIs() (
	values Values, missingRequireds []string,
) {
	values = make(Values)
	for _, kti := range c.orderedTIs {
		val := strings.TrimSpace(kti.TI.Value())
		field := c.fields[kti.Key]
		if val == "" && field.Required {
			missingRequireds = append(missingRequireds, field.Title)
		}

		values[kti.Key] = val
	}

	return values, missingRequireds
}

// Iterates through the keymap, drawing each ti and title in key key order
func (c *createModel) View() string {

	inputs := scaffold.ViewKTIs(uint(c.longestFieldLength), c.orderedTIs, c.selected)

	var wrapSty = lipgloss.NewStyle().Width(c.longestFieldLength)

	var inE, cE string
	if c.inputErr != "" {
		inE = wrapSty.Render(c.inputErr)
	}
	if c.createErr != "" {
		cE = wrapSty.Render(c.createErr)
	}

	return inputs +
		"\n" +
		lipgloss.NewStyle().Width(lipgloss.Width(inputs)).AlignHorizontal(lipgloss.Center).Render(
			stylesheet.ViewSubmitButton(c.SubmitSelected(), inE, cE),
		)
}

func (c *createModel) Done() bool {
	return c.mode == quitting
}

func (c *createModel) Reset() error {
	c.mode = inputting

	var wg sync.WaitGroup
	wg.Add(2)
	// reset TIs
	go func() {
		for i := range c.orderedTIs {
			c.orderedTIs[i].TI.Reset()
			c.orderedTIs[i].TI.Blur()
		}
		wg.Done()
	}()
	// refresh flags to their original, unparsed and unvalued state
	go func() {
		c.fs = installFlagsFromFields(c.fields)
		if c.addtlFlagFunc != nil {
			addtlFlags := c.addtlFlagFunc()
			c.fs.AddFlagSet(&addtlFlags)
		}
		wg.Done()
	}()

	wg.Wait()

	c.createErr = ""
	c.inputErr = ""
	c.selected = 0
	if len(c.orderedTIs) > 0 {
		c.orderedTIs[0].TI.Focus()
	}
	return nil
}

func (c *createModel) SetArgs(_ *pflag.FlagSet, tokens []string) (
	invalid string, onStart tea.Cmd, err error,
) {
	if err := c.fs.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	// we do not need to check missing requires when run from mother
	flagVals, _, err := getValuesFromFlags(&c.fs, c.fields)
	if err != nil {
		return "", nil, err
	}

	for i, kti := range c.orderedTIs {
		// set flag values as the starter values in their corresponding TI
		c.orderedTIs[i].TI.SetValue(flagVals[kti.Key])
		// if a TI has a CustomSetArg, call it now
		if c.fields[kti.Key].CustomTIFuncSetArg != nil {
			c.orderedTIs[i].TI = c.fields[kti.Key].CustomTIFuncSetArg(&kti.TI)
		}
	}

	return "", nil, nil
}

//#endregion
