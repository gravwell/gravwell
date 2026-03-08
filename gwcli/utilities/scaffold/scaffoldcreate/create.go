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
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/gwcli/action"
	"github.com/gravwell/gravwell/v4/gwcli/clilog"
	"github.com/gravwell/gravwell/v4/gwcli/mother"
	"github.com/gravwell/gravwell/v4/gwcli/stylesheet"
	ft "github.com/gravwell/gravwell/v4/gwcli/stylesheet/flagtext"
	"github.com/gravwell/gravwell/v4/gwcli/utilities/pathtextinput"
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
)

// A Config maps keys -> Field; used as (ReadOnly) configuration for this creation instance
type Config = map[string]Field

// CreateFuncT defines the format of the subroutine that must be passed for creating data.
// The function's return values must be:
//
// the id of the newly created value (likely as returned by the Gravwell backend)
//
// a reason the create attempt was invalid (or the empty string)
//
// and an error that occurred (or nil). This is different than an invalid reason and is likely a bubbling up of an error from the client library.
type CreateFuncT func(cfg Config, fieldValues map[string]string, fs *pflag.FlagSet) (id any, invalid string, err error)

// NewCreateAction returns an action pair (covering interactive and non-interactive use) capable of creating new data based on user input.
// You must tell the create action what kind of data it accepts (in the form of fields) and
// what function to pass the populated fields to in order to actually *create* the thing (in the form of a CreateFunc).
//
// Singular is the singular version of the noun you are creating. Ex: "macro", "resource", "query".
func NewCreateAction(singular string, fields Config, createFunc CreateFuncT, extraFlagsFunc func() pflag.FlagSet) action.Pair {
	// nil check singular
	if singular == "" {
		clilog.Writer.Error("singular noun cannot be empty. Defaulting to \"UNKNOWN\"", scaffold.IdentifyCaller())
		singular = "UNKNOWN"
	}

	// standardize field titles
	for fn, f := range fields {
		f.Title = strings.ToTitle(fields[fn].Title)
		fields[fn] = f
	}

	// pull flags from provided fields
	var flags = installFlagsFromFields(fields)
	if extraFlagsFunc != nil {
		afs := extraFlagsFunc()
		flags.AddFlagSet(&afs)
	}

	// pull required flags from cfg to set usage
	requiredFlags := make([]string, 0)
	for _, v := range fields {
		if v.Required && v.FlagName != "" {
			txt := "--" + v.FlagName + "=" + ft.Mandatory("string")
			requiredFlags = append(requiredFlags, txt)
		}
	}

	cmd := treeutils.GenerateAction(
		"create",                 // use
		"create a "+singular,     // short
		"create a new "+singular, // long
		[]string{},               // aliases
		func(c *cobra.Command, s []string) {
			// check non-interactive
			noInteractive, err := c.Flags().GetBool(ft.NoInteractive.Name())
			if err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			}
			// get field flags; spool up mother to prompt for missing required flags if !non-interactive
			var values map[string]string
			if vals, mr, err := getFieldValuesFromFlags(c.Flags(), fields); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if mr != nil {
				if !noInteractive {
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
			if id, inv, err := createFunc(fields, values, c.Flags()); err != nil {
				clilog.Tee(clilog.ERROR, c.ErrOrStderr(), err.Error()+"\n")
				return
			} else if inv != "" { // some of the flags were invalid
				fmt.Fprintln(c.OutOrStdout(), inv)
				return
			} else {
				fmt.Fprintf(c.OutOrStdout(), "Successfully created %v (ID: %v).", singular, id)
			}
		}, treeutils.GenerateActionOptions{Usage: strings.Join(requiredFlags, " ")})

	// attach mined flags to cmd
	cmd.Flags().AddFlagSet(&flags)

	return action.NewPair(cmd, newCreateModel(fields, singular, createFunc, extraFlagsFunc))
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

	inputs struct {
		selected uint       // currently focused item (index correlates to "ordered"+1 (submit))
		err      string     // a reason inputs are invalid. Currently only holds the most-recently set error. Disables submit if set.
		ordered  []struct { // ordered at create-time by Config.Field.Order
			Key  string    // key to acquire the actual field
			Type FieldType // selects the map to fetch from
		}
		TIs  map[string]*textinput.Model     // Type: Text | key -> TI
		PTIs map[string]*pathtextinput.Model // Type: File | key -> PTI
	}
	longestFieldLength int // set at create time
	longestTILength    int // set at create time

	createErr string // the reason the last create failed (not for invalid parameters)

	// function to provide additional flags for this specific create instance
	addtlFlagFunc func() pflag.FlagSet
	// current state of the flagset, Reset to addtlFlagFunc + installFlags
	fs pflag.FlagSet
	cf CreateFuncT // function to create the new entity
}

// SubmitSelect returns if the select button is currently selected by the user.
func (c *createModel) SubmitSelected() bool {
	return c.inputs.selected == uint(len(c.inputs.ordered))
}

// Creates and returns a create Model, ready for interactive usage via Mother.
func newCreateModel(fields Config, singular string, createFunc CreateFuncT, addtlFlagFunc func() pflag.FlagSet) *createModel {
	c := &createModel{
		mode:     inputting,
		width:    defaultWidth,
		singular: singular,
		fields:   fields,
		inputs: struct {
			selected uint
			err      string
			ordered  []struct {
				Key  string
				Type FieldType
			}
			TIs  map[string]*textinput.Model
			PTIs map[string]*pathtextinput.Model
		}{
			ordered: make([]struct {
				Key  string
				Type FieldType
			}, len(fields)),
			TIs:  map[string]*textinput.Model{},
			PTIs: map[string]*pathtextinput.Model{},
		},
		addtlFlagFunc: addtlFlagFunc,
		cf:            createFunc,
	}

	// set flags by mining fields and, if applicable, tacking on additional flags
	c.fs = installFlagsFromFields(fields)
	if c.addtlFlagFunc != nil {
		addtlFlags := c.addtlFlagFunc()
		c.fs.AddFlagSet(&addtlFlags)
	}
	// pre-sort fields so they can be added to inputs.ordered easily
	var keys []string = slices.Collect(maps.Keys(fields))
	slices.SortStableFunc(keys, func(aKey, bKey string) int {
		// sort on order, then alpha on title
		switch {
		case fields[aKey].Order < fields[bKey].Order:
			return 1
		case fields[aKey].Order > fields[bKey].Order:
			return -1
		}
		return strings.Compare(fields[aKey].Title, fields[bKey].Title)
	})
	for i, key := range keys { // construct interactive model from fields
		f := fields[key]
		// assign each field's input to its corresponding table and add it to
		switch f.Type {
		case File:
			pti := pathtextinput.New(pathtextinput.Options{CustomTI: func() textinput.Model { return stylesheet.NewTI("", false) }})
			c.inputs.PTIs[key] = &pti
		case Text:
			var ti textinput.Model
			// if a custom func was not given, use the default generation
			if f.CustomTIFuncInit == nil {
				ti = stylesheet.NewTI(f.DefaultValue, !f.Required)
			} else {
				ti = f.CustomTIFuncInit()
			}
			c.inputs.TIs[key] = &ti

			// TODO correlate titles across types
			// note the longest Title for later formatting
			if w := lipgloss.Width(f.Title); c.longestFieldLength < w {
				c.longestFieldLength = w
			}
			// note the longest TI for later formatting
			if ti.Width > c.longestTILength {
				c.longestTILength = ti.Width
			}
		}
		c.inputs.ordered[i] = struct {
			Key  string
			Type FieldType
		}{
			key, f.Type,
		}
	}

	// focus the first input
	if len(c.inputs.ordered) > 0 {
		switch c.inputs.ordered[0].Type {
		case File:
			c.inputs.PTIs[c.inputs.ordered[0].Key].Focus()
		case Text:
			c.inputs.TIs[c.inputs.ordered[0].Key].Focus()
		default:
			clilog.Writer.Error("failed to focus ordered[0] field on startup: unknown field type", attachLogInfo(c.inputs.ordered[0].Key, c.inputs.ordered[0].Type)...)
		}
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
						c.inputs.err = fmt.Sprintf("%v is required", mr[0])
					} else {
						c.inputs.err = fmt.Sprintf("%v are required", mr)
					}
					return nil
				}
				id, invalid, err := c.cf(c.fields, values, &c.fs)
				if err != nil {
					c.createErr = err.Error()
					return nil
				} else if invalid != "" {
					c.inputs.err = invalid
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
	var cmd tea.Cmd
	if !c.SubmitSelected() {
		var iErr error // input error from this cycle, if applicable
		// pass message to currently focused input
		switch c.curInputType() {
		case File:
			pti := c.inputs.PTIs[c.curInputKey()]
			var npti pathtextinput.Model
			npti, cmd = pti.Update(msg)
			iErr = npti.Err
			c.inputs.PTIs[c.curInputKey()] = &npti // replace pti
		case Text:
			ti := c.inputs.TIs[c.curInputKey()]
			var nti textinput.Model
			nti, cmd = ti.Update(msg)
			iErr = nti.Err
			c.inputs.TIs[c.curInputKey()] = &nti // replace pti
		}

		if iErr != nil {
			c.inputs.err = iErr.Error()
		} else {
			c.inputs.err = ""
		}

	}
	return cmd
}

// Blurs the current input, selects and focuses the next one c.inputs.ordered.
func (c *createModel) focusNext() {
	c.focusInput(false)
	c.inputs.selected += 1
	if c.inputs.selected > uint(len(c.inputs.ordered)) { // jump to start
		c.inputs.selected = 0
	}
	c.focusInput(true)
}

// Blurs the current input, selects and focuses the previous one in c.inputs.ordered.
func (c *createModel) focusPrevious() {
	c.focusInput(false)

	if c.inputs.selected == 0 { // wrap to submit button
		c.inputs.selected = uint(len(c.inputs.ordered))
	} else {
		c.inputs.selected -= 1
	}
	c.focusInput(true)
}

// focusInput toggles the focus on the currently selected input (doing nothing if submit is selected).
// If !focus, blurs the input.
func (c *createModel) focusInput(focus bool) {
	if c.SubmitSelected() {
		return
	}
	switch c.curInputType() {
	case File:
		if focus {
			c.inputs.PTIs[c.curInputKey()].Focus()
		} else {
			c.inputs.PTIs[c.curInputKey()].Blur()
		}
	case Text:
		if focus {
			c.inputs.TIs[c.curInputKey()].Focus()
		} else {
			c.inputs.TIs[c.curInputKey()].Blur()
		}
	default:
		s := "focus"
		if !focus {
			s = "blur"
		}
		clilog.Writer.Error("failed to "+s+" next input: unknown field type",
			attachLogInfo(c.inputs.ordered[c.inputs.selected].Key, c.inputs.ordered[c.inputs.selected].Type)...)
	}
}

// Generates the corollary value map from the inputs.
//
// Returns:
//
// - key -> input value
//
// - a list of required fields (as their keys) with empty values
//
// - an error (if applicable)
// TODO rename to extractInputValues
func (c *createModel) extractValuesFromTIs() (fieldValues map[string]string, missingRequiredFields []string) {
	fieldValues = make(map[string]string, len(c.inputs.ordered))
	for _, o := range c.inputs.ordered {
		// fetch respective input's value
		var val string
		switch o.Type {
		case File:
			val = c.inputs.PTIs[o.Key].Value()
		case Text:
			val = c.inputs.TIs[o.Key].Value()
		default:
			clilog.Writer.Error("failed to fetch next input: unknown field type",
				attachLogInfo(o.Key, o.Type)...)
			continue
		}

		val = strings.TrimSpace(val)
		field, ok := c.fields[o.Key]
		if !ok {
			clilog.Writer.Error("failed to extract input values: failed to find field associated to key " + o.Key)
			continue
		}
		if field.Required && val == "" { // check for missing required
			missingRequiredFields = append(missingRequiredFields, o.Key)
		}

		fieldValues[o.Key] = val
	}

	return fieldValues, missingRequiredFields
}

var rightAlignSty = lipgloss.NewStyle().AlignHorizontal(lipgloss.Right)

// Iterates through the inputs in order, composing as "titles:input".
func (c *createModel) View() string {

	var titles, inputViews []string // stylized left and right items, paired on index
	var sb strings.Builder          // to build titles; reused each cycle
	for i, o := range c.inputs.ordered {
		sb.Reset()
		field, ok := c.fields[o.Key]
		if !ok {
			clilog.Writer.Error("failed to generate field view: failed to find field associated to key " + o.Key)
			continue
		}
		// left-pad so all titles are all the same width
		sb.WriteString(strings.Repeat(" ", int(max(c.longestFieldLength, c.longestTILength))-len(field.Title)))
		sb.WriteString(stylesheet.Pip(c.inputs.selected, uint(i)))
		// coloruize and attach titles
		if field.Required {
			sb.WriteString(stylesheet.Cur.PrimaryText.Render(field.Title + ":"))
		} else {
			sb.WriteString(stylesheet.Cur.SecondaryText.Render(field.Title + ":"))
		}
		// render the input and right-align it // TODO do we need to right align our titles?
		titles = append(titles, rightAlignSty.Render(sb.String()))
		sb.Reset()

		// attach input view
		switch o.Type {
		case File:
			inputViews = append(inputViews, c.inputs.PTIs[o.Key].View())
		case Text:
			inputViews = append(inputViews, c.inputs.TIs[o.Key].View())
		}
	}
	// compose the titles and inputs
	mainView := lipgloss.JoinHorizontal(lipgloss.Center, lipgloss.JoinVertical(lipgloss.Right, titles...))

	// generate submit button and align it with the center
	var sbtn = stylesheet.ViewSubmitButton(c.SubmitSelected(), c.width, c.inputs.err, c.createErr)
	// align the submit to roughly the end of the field titles
	return lipgloss.NewStyle().Width(c.width).
		AlignHorizontal(lipgloss.Center).Render(mainView) + "\n" + sbtn

}

func (c *createModel) Done() bool {
	return c.mode == quitting
}

func (c *createModel) Reset() error {
	c.mode = inputting

	var wg sync.WaitGroup
	// reset TIs
	wg.Go(func() {
		for _, pti := range c.inputs.PTIs {
			pti.Reset()
			pti.Blur()
		}
	})
	wg.Go(func() {
		for _, ti := range c.inputs.TIs {
			ti.Reset()
			ti.Blur()
		}
	})
	wg.Go(func() { // refresh flags to their original, unparsed and unvalued state
		c.fs = installFlagsFromFields(c.fields)
		if c.addtlFlagFunc != nil {
			addtlFlags := c.addtlFlagFunc()
			c.fs.AddFlagSet(&addtlFlags)
		}
	})

	wg.Wait()

	c.createErr = ""
	c.inputs.err = ""
	c.inputs.selected = 0
	c.focusInput(true)
	return nil
}

func (c *createModel) SetArgs(fs *pflag.FlagSet, tokens []string, width, height int) (
	invalid string, onStart tea.Cmd, err error) {
	if err := c.fs.Parse(tokens); err != nil {
		return err.Error(), nil, nil
	}

	// we do not need to check missing requires when run from mother
	flagVals, _, err := getFieldValuesFromFlags(&c.fs, c.fields)
	if err != nil {
		return "", nil, err
	}

	// iterate fields to check for customTIconf and values
	for key, field := range c.fields {
		// check for and set flag values
		if v, found := flagVals[key]; found {
			switch field.Type {
			case File:
				c.inputs.PTIs[key].SetValue(v)
			case Text:
				c.inputs.TIs[key].SetValue(v)
			}
		}
		// check for customTIs to call
		if field.CustomTIFuncSetArg != nil {
			ti := c.inputs.TIs[key]
			nti := field.CustomTIFuncSetArg(ti)
			c.inputs.TIs[key] = &nti
		}

	}

	for key, fval := range flagVals {
		// figure out type by searching fields
		field := c.fields[key]
		switch field.Type {
		case File:
			c.inputs.PTIs[key].SetValue(fval)
		}
	}
	c.width = width

	return "", nil, nil
}

//#region getter/setter helper functions

// Returns the key of the current input.
//
// Returns "" if submit is selected.
func (c *createModel) curInputKey() string {
	if c.SubmitSelected() {
		return ""
	}
	return c.inputs.ordered[c.inputs.selected].Key
}

// Returns the key of the current input.
//
// Returns "" if submit is selected.
func (c *createModel) curInputType() FieldType {
	if c.SubmitSelected() {
		return ""
	}
	return c.inputs.ordered[c.inputs.selected].Type
}

// getInputValue is a helper function for fetching the .Value of an input.
// If type is given, only the associated map will be checked (providing a meager time-cost reduction).
//
// Returns empty and logs and error if the key is not found.
func (c *createModel) getInputValue(key string, typ FieldType) string {
	switch typ {
	case File:
		pti, ok := c.inputs.PTIs[key]
		if ok {
			return pti.Value()
		}
	case Text:
		ti, ok := c.inputs.TIs[key]
		if ok {
			return ti.Value()
		}
	default:
		// check both: file, then text
		pti, ok := c.inputs.PTIs[key]
		if ok {
			return pti.Value()
		}
		ti, ok := c.inputs.TIs[key]
		if ok {
			return ti.Value()
		}

	}

	// if we made it this far, the key was not found. Log an error and return.
	clilog.Writer.Warn("failed to find input value associated to key", attachLogInfo(key, typ)...)

	return ""
}

//#endregion

// attachLogInfo returns 3 SDParams that are useful to attach to most/every log: key, type, and caller identity.
func attachLogInfo(key string, typ FieldType) []rfc5424.SDParam {
	return []rfc5424.SDParam{
		rfc5424.SDParam{Name: "field_key", Value: key},
		rfc5424.SDParam{Name: "type", Value: typ},
		scaffold.IdentifyCaller(),
	}
}
